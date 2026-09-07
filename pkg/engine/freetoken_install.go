package engine

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const freeTokenCLITimeout = 30 * time.Minute

type freeTokenAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type freeTokenManifest struct {
	Runtime     freeTokenAsset `json:"runtime"`
	KernelCache freeTokenAsset `json:"kernel_cache"`
}

func installFreeToken(ctx context.Context, w io.Writer) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("FreeToken auto-install is only supported on Windows")
	}
	fmt.Fprintln(w, "Installing FreeToken CLI (PyTorch + engine wheels; this can take several minutes)...")
	if err := installFreeTokenCLI(ctx, w); err != nil {
		return err
	}
	bin := freeTokenCLIBin()
	if !fileExists(bin) {
		return fmt.Errorf("installed FreeToken CLI but %s is missing", bin)
	}
	return nil
}

func installFreeTokenCLI(ctx context.Context, w io.Writer) error {
	venv, err := freeTokenVenvDir()
	if err != nil {
		return err
	}
	man, err := fetchFreeTokenManifest(ctx)
	if err != nil {
		return err
	}
	wheels, err := downloadFreeTokenWheels(ctx, man)
	if err != nil {
		return err
	}

	pipCtx, cancel := context.WithTimeout(ctx, freeTokenCLITimeout)
	defer cancel()

	uv, uvErr := ensureUV(ctx, w)
	if uvErr == nil {
		return installFreeTokenWithUV(pipCtx, w, uv, venv, wheels)
	}

	py, prefix, pyErr := findPython312()
	if pyErr != nil {
		return fmt.Errorf("need uv or Python 3.12 for the FreeToken CLI: %v; %w", uvErr, pyErr)
	}

	venvArgs := append(append([]string{}, prefix...), "-m", "venv", venv)
	fmt.Fprintf(w, "Creating venv at %s...\n", venv)
	if err := runCapture(pipCtx, py, venvArgs...); err != nil {
		return fmt.Errorf("python venv: %w", err)
	}
	pip := venvPip(venv)
	pipArgs := append([]string{"install"}, wheels...)
	if err := runCapture(pipCtx, pip, pipArgs...); err != nil {
		return fmt.Errorf("pip install freetoken: %w", err)
	}
	return nil
}

func installFreeTokenWithUV(ctx context.Context, w io.Writer, uv, venv string, wheels []string) error {
	fmt.Fprintf(w, "Creating venv with uv at %s...\n", venv)
	if err := runCapture(ctx, uv, "venv", "--python", "3.12", venv); err != nil {
		return fmt.Errorf("uv venv: %w", err)
	}
	py := venvPython(venv)
	args := append([]string{"pip", "install", "--python", py}, wheels...)
	if err := runCapture(ctx, uv, args...); err != nil {
		return fmt.Errorf("uv pip install freetoken: %w", err)
	}
	return nil
}

func ensureUV(ctx context.Context, w io.Writer) (string, error) {
	if p := lookPath("uv"); p != "" {
		return p, nil
	}
	binDir, err := inferouteBinDir()
	if err != nil {
		return "", err
	}
	dest := filepath.Join(binDir, "uv.exe")
	if fileExists(dest) {
		return dest, nil
	}
	fmt.Fprintln(w, "Downloading uv (provides Python 3.12)...")
	zipPath := filepath.Join(os.TempDir(), "uv-x86_64-pc-windows-msvc.zip")
	if err := download(ctx, uvWindowsURL, zipPath); err != nil {
		return "", fmt.Errorf("download uv: %w", err)
	}
	if err := extractNamedFromZip(zipPath, dest, "uv.exe"); err != nil {
		return "", fmt.Errorf("extract uv: %w", err)
	}
	return dest, nil
}

func inferouteBinDir() (string, error) {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		local = filepath.Join(home, "AppData", "Local")
	}
	dir := filepath.Join(local, "inferoute", "bin")
	return dir, os.MkdirAll(dir, 0o755)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func extractNamedFromZip(zipPath, dest, name string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() || filepath.Base(f.Name) != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		_ = rc.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return fmt.Errorf("%s not found in %s", name, zipPath)
}

func freeTokenVenvDir() (string, error) {
	dir := freeTokenVenvDirPath()
	if dir == "" {
		return "", fmt.Errorf("cannot resolve FreeToken venv directory")
	}
	return dir, os.MkdirAll(filepath.Dir(dir), 0o755)
}

func freeTokenVenvDirPath() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		local = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(local, "inferoute", "venv-freetoken")
}

func freeTokenCLIBin() string {
	dir := freeTokenVenvDirPath()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "Scripts", "ft.exe")
}

func venvPython(venv string) string {
	return filepath.Join(venv, "Scripts", "python.exe")
}

func venvPip(venv string) string {
	return filepath.Join(venv, "Scripts", "pip.exe")
}

func fetchFreeTokenManifest(ctx context.Context) (freeTokenManifest, error) {
	tmp := filepath.Join(os.TempDir(), "engine-win_amd64.json")
	if err := download(ctx, freeTokenEngineManifestURL, tmp); err != nil {
		return freeTokenManifest{}, fmt.Errorf("download FreeToken engine manifest: %w", err)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return freeTokenManifest{}, err
	}
	return parseFreeTokenManifest(data)
}

func parseFreeTokenManifest(data []byte) (freeTokenManifest, error) {
	var man freeTokenManifest
	if err := json.Unmarshal(data, &man); err != nil {
		return freeTokenManifest{}, fmt.Errorf("parse FreeToken engine manifest: %w", err)
	}
	if man.Runtime.URL == "" || man.KernelCache.URL == "" {
		return freeTokenManifest{}, fmt.Errorf("FreeToken engine manifest missing wheel URLs")
	}
	return man, nil
}

func downloadFreeTokenWheels(ctx context.Context, man freeTokenManifest) ([]string, error) {
	assets := []freeTokenAsset{man.Runtime, man.KernelCache}
	paths := make([]string, 0, len(assets))
	for _, a := range assets {
		name := a.Name
		if name == "" {
			name = filepath.Base(a.URL)
		}
		dest := filepath.Join(os.TempDir(), name)
		if err := download(ctx, a.URL, dest); err != nil {
			return nil, fmt.Errorf("download %s: %w", name, err)
		}
		paths = append(paths, dest)
	}
	return paths, nil
}

func findPython312() (bin string, prefix []string, err error) {
	local := os.Getenv("LOCALAPPDATA")
	pf := os.Getenv("ProgramFiles")
	type cand struct {
		bin    string
		prefix []string
	}
	cands := []cand{
		{lookPath("py"), []string{"-3.12"}},
		{lookPath("python3.12"), nil},
		{filepath.Join(local, "Programs", "Python", "Python312", "python.exe"), nil},
		{filepath.Join(pf, "Python312", "python.exe"), nil},
		{lookPath("python"), nil},
	}
	for _, c := range cands {
		if c.bin == "" {
			continue
		}
		if pythonIs312(c.bin, c.prefix) {
			return c.bin, c.prefix, nil
		}
	}
	return "", nil, fmt.Errorf("python 3.12 not found")
}

func pythonIs312(bin string, prefix []string) bool {
	args := append(append([]string{}, prefix...), "--version")
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "3.12")
}

func runCapture(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s: %w", name, err)
		}
		return fmt.Errorf("%s: %w: %s", name, err, msg)
	}
	return nil
}
