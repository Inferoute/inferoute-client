package engine

import (
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
	fmt.Fprintln(w, "Downloading FreeToken Windows installer...")
	tmp := filepath.Join(os.TempDir(), "FreeToken-Setup-win-x64.exe")
	if err := download(ctx, freeTokenWindowsURL, tmp); err != nil {
		return err
	}
	fmt.Fprintln(w, "Starting silent installer (Windows may ask for permission)...")
	startFreeTokenSetup(tmp)

	if Detect(KindFreeToken).Found {
		return nil
	}

	fmt.Fprintln(w, "Installing FreeToken CLI (PyTorch + engine wheels; this can take several minutes)...")
	cliErr := installFreeTokenCLI(ctx, w)
	if Detect(KindFreeToken).Found {
		return nil
	}
	if waitForFreeToken(ctx, 45*time.Second) {
		return nil
	}
	if cliErr != nil {
		return cliErr
	}
	return fmt.Errorf("installed FreeToken Desktop but could not find ft.exe; install Python 3.12 and re-run setup")
}

func startFreeTokenSetup(installer string) {
	cmd := exec.Command(installer, "/S")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return
	}
	go func() { _ = cmd.Wait() }()
}

func waitForFreeToken(ctx context.Context, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if Detect(KindFreeToken).Found {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
	return Detect(KindFreeToken).Found
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

	if uv := lookPath("uv"); uv != "" {
		fmt.Fprintf(w, "Creating venv with uv at %s...\n", venv)
		if err := runCapture(pipCtx, uv, "venv", "--python", "3.12", venv); err != nil {
			return fmt.Errorf("uv venv: %w", err)
		}
		py := venvPython(venv)
		args := append([]string{"pip", "install", "--python", py}, wheels...)
		if err := runCapture(pipCtx, uv, args...); err != nil {
			return fmt.Errorf("uv pip install freetoken: %w", err)
		}
		return nil
	}

	py, prefix, err := findPython312()
	if err != nil {
		fmt.Fprintln(w, "Python 3.12 not found; installing with winget...")
		if werr := runCapture(pipCtx, "winget", "install", "--id", "Python.Python.3.12", "-e", "--accept-package-agreements", "--accept-source-agreements"); werr != nil {
			return fmt.Errorf("need Python 3.12 for the FreeToken CLI (Desktop does not ship ft.exe): %w", err)
		}
		py, prefix, err = findPython312()
		if err != nil {
			return fmt.Errorf("installed Python 3.12 but it is not on PATH yet; open a new terminal and re-run setup: %w", err)
		}
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

func freeTokenVenvDir() (string, error) {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		local = filepath.Join(home, "AppData", "Local")
	}
	dir := filepath.Join(local, "inferoute", "venv-freetoken")
	return dir, os.MkdirAll(filepath.Dir(dir), 0o755)
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
