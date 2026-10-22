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
	"strconv"
	"strings"
)

const (
	vllmMetalInstallURL = "https://raw.githubusercontent.com/vllm-project/vllm-metal/main/install.sh"
	vllmDocsURL         = "https://docs.vllm.ai/en/stable/getting_started/installation/gpu/index.html"
	// blackwellComputeMajor is the first CUDA compute capability whose vLLM
	// attention path needs FlashInfer cubins that are not in the pip wheel.
	// sm_100 (10.0) is datacenter Blackwell. sm_120 (12.0) is RTX 50-series
	// and RTX PRO 6000. Ampere (8.6), Ada (8.9), and Hopper (9.0) do not.
	blackwellComputeMajor = 10
)

// Install runs the engine's official install path. It prints progress to w.
func Install(ctx context.Context, kind Kind, w io.Writer) error {
	if w == nil {
		w = io.Discard
	}
	switch kind {
	case KindVLLM:
		return installVLLM(ctx, w)
	case KindVLLMMetal:
		return installVLLMMetal(ctx, w)
	case KindLlamaCpp:
		return installLlamaCpp(ctx, w)
	default:
		return fmt.Errorf("unknown engine %s", kind)
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
		return downloadKernelsIfNeeded(ctx, w, venv)
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
	return downloadKernelsIfNeeded(ctx, w, venv)
}

// downloadKernelsIfNeeded fetches FlashInfer cubins when a Blackwell GPU is
// present. Ampere and Ada ship FlashAttention inside the vLLM wheel. sm_120
// does not, and vLLM serve dies during CUDA graph capture without the cubins.
func downloadKernelsIfNeeded(ctx context.Context, w io.Writer, venv string) error {
	caps, err := nvidiaComputeCaps(ctx)
	if err != nil {
		fmt.Fprintf(w, "Skipping vLLM kernel download (could not read GPU compute capability: %v)\n", err)
		return nil
	}
	if !needsFlashInferKernels(caps) {
		return nil
	}
	vllm := filepath.Join(venv, "bin", "vllm")
	if runtime.GOOS == "windows" {
		vllm = filepath.Join(venv, "Scripts", "vllm.exe")
	}
	fmt.Fprintln(w, "Blackwell GPU detected. Downloading FlashInfer kernels...")
	if err := run(ctx, w, vllm, "download-kernels"); err != nil {
		return fmt.Errorf("vLLM kernel download failed: %w", err)
	}
	return nil
}

func nvidiaComputeCaps(ctx context.Context) ([]string, error) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=compute_cap", "--format=csv,noheader")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var caps []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			caps = append(caps, line)
		}
	}
	if len(caps) == 0 {
		return nil, fmt.Errorf("nvidia-smi returned no compute capability")
	}
	return caps, nil
}

func needsFlashInferKernels(computeCaps []string) bool {
	for _, cap := range computeCaps {
		major, _, ok := parseComputeCap(cap)
		if ok && major >= blackwellComputeMajor {
			return true
		}
	}
	return false
}

func parseComputeCap(s string) (major, minor int, ok bool) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ".")
	if parts[0] == "" {
		return 0, 0, false
	}
	var err error
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	if len(parts) > 1 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
	}
	return major, minor, true
}

func installVLLMMetal(ctx context.Context, w io.Writer) error {
	if home, err := os.UserHomeDir(); err == nil {
		if err := replaceVenvDir(w, filepath.Join(home, ".venv-vllm-metal")); err != nil {
			return err
		}
	}
	fmt.Fprintln(w, "Installing vLLM Metal via official install script...")
	return runScript(ctx, w, vllmMetalInstallURL)
}

func replaceVenvDir(w io.Writer, venv string) error {
	if _, err := os.Stat(venv); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	fmt.Fprintf(w, "Removing existing venv at %s so uv can recreate it...\n", venv)
	if err := os.RemoveAll(venv); err != nil {
		return fmt.Errorf("remove %s: %w", venv, err)
	}
	return nil
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
