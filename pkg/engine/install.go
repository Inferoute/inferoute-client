package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const (
	ollamaInstallURL    = "https://ollama.com/install.sh"
	vllmMetalInstallURL = "https://raw.githubusercontent.com/vllm-project/vllm-metal/main/install.sh"
	freeTokenWindowsURL = "https://github.com/FlashML-org/FreeToken-Web/releases/latest/download/FreeToken-Setup-win-x64.exe"
	vllmDocsURL         = "https://docs.vllm.ai/en/stable/getting_started/installation/gpu/index.html"
)

// Install runs the engine's official install path. It prints progress to w.
func Install(ctx context.Context, kind Kind, w io.Writer) error {
	if w == nil {
		w = io.Discard
	}
	switch kind {
	case KindOllama:
		return installOllama(ctx, w)
	case KindVLLM:
		return installVLLM(ctx, w)
	case KindVLLMMetal:
		return installVLLMMetal(ctx, w)
	case KindFreeToken:
		return installFreeToken(ctx, w)
	default:
		return fmt.Errorf("unknown engine %s", kind)
	}
}

func installOllama(ctx context.Context, w io.Writer) error {
	switch runtime.GOOS {
	case "windows":
		fmt.Fprintln(w, "Installing Ollama via winget...")
		return run(ctx, w, "winget", "install", "--id", "Ollama.Ollama", "-e", "--accept-package-agreements", "--accept-source-agreements")
	case "darwin":
		if lookPath("brew") != "" {
			fmt.Fprintln(w, "Installing Ollama via Homebrew...")
			return run(ctx, w, "brew", "install", "ollama")
		}
		fmt.Fprintln(w, "Installing Ollama via official install script...")
		return runScript(ctx, w, ollamaInstallURL)
	default:
		fmt.Fprintln(w, "Installing Ollama via official install script...")
		return runScript(ctx, w, ollamaInstallURL)
	}
}

func installVLLM(ctx context.Context, w io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	venv := filepath.Join(home, ".venv-vllm")
	fmt.Fprintf(w, "Creating vLLM venv at %s...\n", venv)
	if lookPath("uv") != "" {
		if err := run(ctx, w, "uv", "venv", venv); err != nil {
			return err
		}
		pip := filepath.Join(venv, "bin", "uv")
		if runtime.GOOS == "windows" {
			pip = filepath.Join(venv, "Scripts", "uv.exe")
		}
		fmt.Fprintln(w, "Installing vLLM (this can take several minutes)...")
		if err := run(ctx, w, pip, "pip", "install", "vllm"); err != nil {
			return fmt.Errorf("vLLM pip install failed: %w (see %s)", err, vllmDocsURL)
		}
		return nil
	}
	py := "python3"
	if lookPath(py) == "" {
		py = "python"
	}
	if err := run(ctx, w, py, "-m", "venv", venv); err != nil {
		return err
	}
	pip := filepath.Join(venv, "bin", "pip")
	if runtime.GOOS == "windows" {
		pip = filepath.Join(venv, "Scripts", "pip.exe")
	}
	fmt.Fprintln(w, "Installing vLLM (this can take several minutes)...")
	if err := run(ctx, w, pip, "install", "vllm"); err != nil {
		return fmt.Errorf("vLLM pip install failed: %w (see %s)", err, vllmDocsURL)
	}
	return nil
}

func installVLLMMetal(ctx context.Context, w io.Writer) error {
	fmt.Fprintln(w, "Installing vLLM Metal via official install script...")
	return runScript(ctx, w, vllmMetalInstallURL)
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
	fmt.Fprintln(w, "Running silent installer (Windows may ask for permission)...")
	if err := run(ctx, w, tmp, "/S"); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if Detect(KindFreeToken).Found {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if Detect(KindFreeToken).Found {
		return nil
	}
	return fmt.Errorf("installed FreeToken but could not find ft.exe; re-run setup after adding it to PATH")
}

func run(ctx context.Context, w io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

func runScript(ctx context.Context, w io.Writer, url string) error {
	cmd := exec.CommandContext(ctx, "bash", "-c", "curl -fsSL "+url+" | bash")
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

func download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
