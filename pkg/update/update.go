package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/nodeapi"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/version"
)

const (
	githubRepo = "inferoute/inferoute-client"
	githubAPI  = "https://api.github.com/repos/" + githubRepo + "/releases/latest"
)

// Release is the newest client the platform (or GitHub) is advertising.
type Release struct {
	Latest      string
	Min         string
	DownloadURL string
}

// FetchRelease asks the node, then GitHub, which client release is current.
func FetchRelease(ctx context.Context, platformURL string) (Release, error) {
	if platformURL != "" {
		rel, err := fetchFromNode(ctx, platformURL)
		if err == nil && rel.Latest != "" {
			return rel, nil
		}
	}
	return fetchFromGitHub(ctx)
}

// Notice is the line printed at startup. required means the node will reject this build.
func Notice(rel Release) (message string, required bool) {
	cur := version.Version
	if nodeapi.Newer(rel.Min, cur) {
		return fmt.Sprintf("Update required: %s -> %s. Run 'inferoute-client update' then restart.", cur, rel.Latest), true
	}
	if nodeapi.Newer(rel.Latest, cur) {
		return fmt.Sprintf("Update available: %s -> %s. Run 'inferoute-client update' then restart.", cur, rel.Latest), false
	}
	return "", false
}

// Apply downloads the newest build, checks checksums.txt, and replaces this binary.
// The caller must not be the running daemon.
func Apply(ctx context.Context, platformURL string, checkOnly bool) error {
	if os.Getenv("INFEROUTE_IN_DOCKER") == "1" {
		fmt.Println("This client is running in Docker. Pull the latest image and restart the container.")
		return nil
	}
	rel, err := FetchRelease(ctx, platformURL)
	if err != nil {
		return err
	}
	if !nodeapi.Newer(rel.Latest, version.Version) && !nodeapi.Newer(rel.Min, version.Version) {
		fmt.Printf("inferoute-client %s is up to date.\n", version.Version)
		return nil
	}
	msg, _ := Notice(rel)
	fmt.Println(msg)
	if checkOnly {
		return nil
	}
	if running, err := daemonRunning(ctx); err != nil {
		return err
	} else if running {
		return fmt.Errorf("inferoute-client is running. Stop it, then run 'inferoute-client update' again")
	}
	asset := assetName()
	zipURL := assetURL(rel, asset)
	dir, err := os.MkdirTemp("", "inferoute-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	zipPath := filepath.Join(dir, asset)
	if err := download(ctx, zipURL, zipPath); err != nil {
		return err
	}
	if err := verifyChecksum(ctx, rel, asset, zipPath); err != nil {
		return err
	}
	binPath, err := unzipBinary(zipPath, dir)
	if err != nil {
		return err
	}
	if err := replaceExecutable(binPath); err != nil {
		return err
	}
	fmt.Printf("Updated to %s. Restart inferoute-client.\n", rel.Latest)
	return nil
}

// RemoveStaleBackup deletes the previous Windows executable left beside the new one.
func RemoveStaleBackup() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
}

func fetchFromNode(ctx context.Context, platformURL string) (Release, error) {
	url := strings.TrimRight(platformURL, "/") + "/api/client/version"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	resp, err := nodeapi.NewHTTPClient(10 * time.Second).Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GET /api/client/version returned %d", resp.StatusCode)
	}
	var body struct {
		Latest      string `json:"latest"`
		Min         string `json:"min_supported"`
		DownloadURL string `json:"download_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, err
	}
	return Release{Latest: body.Latest, Min: body.Min, DownloadURL: body.DownloadURL}, nil
}

func fetchFromGitHub(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPI, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github releases returned %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, err
	}
	latest := strings.TrimPrefix(body.TagName, "v")
	return Release{
		Latest:      latest,
		DownloadURL: "https://github.com/" + githubRepo + "/releases/latest",
	}, nil
}

func assetName() string {
	return fmt.Sprintf("inferoute-client-%s-%s.zip", runtime.GOOS, runtime.GOARCH)
}

func assetURL(rel Release, asset string) string {
	base := strings.TrimRight(rel.DownloadURL, "/")
	tag := rel.Latest
	if tag != "" && !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	if base == "" || strings.HasSuffix(base, "/releases/latest") {
		if tag == "" || tag == "v" {
			return "https://github.com/" + githubRepo + "/releases/latest/download/" + asset
		}
		return "https://github.com/" + githubRepo + "/releases/download/" + tag + "/" + asset
	}
	return base + "/" + asset
}

func checksumURL(rel Release) string {
	asset := assetURL(rel, "checksums.txt")
	return asset[:strings.LastIndex(asset, "/")+1] + "checksums.txt"
}

func download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned %d", url, resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func verifyChecksum(ctx context.Context, rel Release, asset, zipPath string) error {
	sumsPath := zipPath + ".sums"
	if err := download(ctx, checksumURL(rel), sumsPath); err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	raw, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	var expected string
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == asset {
			expected = strings.ToLower(fields[0])
		}
	}
	if expected == "" {
		return fmt.Errorf("checksums.txt has no entry for %s", asset)
	}
	f, err := os.Open(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s", asset)
	}
	return nil
}

func unzipBinary(zipPath, dir string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		src, err := f.Open()
		if err != nil {
			return "", err
		}
		dest := filepath.Join(dir, "inferoute-client.new")
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			src.Close()
			return "", err
		}
		_, copyErr := io.Copy(out, src)
		src.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return dest, nil
	}
	return "", fmt.Errorf("zip did not contain a binary")
}

func replaceExecutable(newPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if err := os.Chmod(newPath, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return fmt.Errorf("rename current binary: %w", err)
		}
		if err := os.Rename(newPath, exe); err != nil {
			_ = os.Rename(old, exe)
			return fmt.Errorf("install new binary: %w", err)
		}
		return nil
	}
	if err := os.Rename(newPath, exe); err != nil {
		return fmt.Errorf("install new binary: %w (is inferoute-client on a different filesystem, or not writable?)", err)
	}
	return nil
}

func daemonRunning(ctx context.Context) (bool, error) {
	path, err := configPath()
	if err != nil || path == "" {
		return false, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return false, nil
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/status", cfg.Server.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "inferoute", "config.yaml"), nil
}
