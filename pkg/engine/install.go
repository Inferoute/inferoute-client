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
)

const (
	ollamaInstallURL           = "https://ollama.com/install.sh"
	vllmMetalInstallURL        = "https://raw.githubusercontent.com/vllm-project/vllm-metal/main/install.sh"
	vllmMetalInstallURLGitHub  = "https://github.com/vllm-project/vllm-metal/raw/main/install.sh"
	vllmMetalInstallURLAPI     = "https://api.github.com/repos/vllm-project/vllm-metal/contents/install.sh?ref=main"
	freeTokenEngineManifestURL = "https://github.com/FlashML-org/FreeToken-Web/releases/download/beta/engine-win_amd64.json"
	uvWindowsURL               = "https://github.com/astral-sh/uv/releases/latest/download/uv-x86_64-pc-windows-msvc.zip"
	vllmDocsURL                = "https://docs.vllm.ai/en/stable/getting_started/installation/gpu/index.html"
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
	try := []struct {
		url  string
		curl string
	}{
		{vllmMetalInstallURL, vllmMetalInstallURL},
		{vllmMetalInstallURLGitHub, vllmMetalInstallURLGitHub},
		{vllmMetalInstallURLAPI, `-H "Accept: application/vnd.github.raw" -H "User-Agent: inferoute-client" ` + vllmMetalInstallURLAPI},
	}
	var last error
	for i, t := range try {
		if i > 0 {
			fmt.Fprintf(w, "Retrying from %s...\n", t.url)
		}
		last = runScript(ctx, w, t.curl)
		if last == nil {
			return nil
		}
		fmt.Fprintf(w, "%s failed: %v\n", t.url, last)
	}
	return last
}

func run(ctx context.Context, w io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

func runScript(ctx context.Context, w io.Writer, curlArgs string) error {
	cmd := exec.CommandContext(ctx, "bash", "-c", "set -o pipefail; curl -fsSL "+curlArgs+" | bash")
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
