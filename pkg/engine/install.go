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
	"strings"
)

const (
	vllmMetalInstallURL = "https://raw.githubusercontent.com/vllm-project/vllm-metal/main/install.sh"
	vllmDocsURL         = "https://docs.vllm.ai/en/stable/getting_started/installation/gpu/index.html"
	uvInstallURL        = "https://astral.sh/uv/install.sh"
	// vllmPython is the interpreter every node uses. uv downloads this CPython
	// when the host does not have it, so the distro's Python does not matter.
	vllmPython = "3.12"
)

// Install runs the engine's official install path. version pins a vLLM
// release (for example "0.31.0"). Empty installs the current latest.
func Install(ctx context.Context, kind Kind, version string, w io.Writer) error {
	if w == nil {
		w = io.Discard
	}
	switch kind {
	case KindVLLM:
		return installVLLM(ctx, version, w)
	case KindVLLMMetal:
		return installVLLMMetal(ctx, w)
	case KindLlamaCpp:
		return installLlamaCpp(ctx, w)
	default:
		return fmt.Errorf("unknown engine %s", kind)
	}
}

func vllmRequirement(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "vllm"
	}
	return "vllm==" + version
}

func installVLLM(ctx context.Context, version string, w io.Writer) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("vLLM is not supported on Windows (see %s)", vllmDocsURL)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	venv := filepath.Join(home, ".venv-vllm")
	if strings.TrimSpace(version) != "" {
		if err := replaceVenvDir(w, venv); err != nil {
			return err
		}
	}
	uv, err := ensureUV(ctx, w, home)
	if err != nil {
		return err
	}
	req := vllmRequirement(version)
	fmt.Fprintf(w, "Creating vLLM venv at %s (Python %s)...\n", venv, vllmPython)
	if err := run(ctx, w, uv, "venv", "--python", vllmPython, venv); err != nil {
		return err
	}
	fmt.Fprintln(w, "Installing vLLM (this can take several minutes)...")
	py := venvExe(venv, "python")
	if err := run(ctx, w, uv, "pip", "install", "--python", py, req); err != nil {
		return fmt.Errorf("vLLM pip install failed: %w (see %s)", err, vllmDocsURL)
	}
	if err := downloadKernels(ctx, w, venv); err != nil {
		return err
	}
	return checkTorchCUDA(ctx, w, venv)
}

// findUV returns uv on PATH, or ~/.local/bin/uv when that file exists.
// pathHit is the result of lookPath("uv"); tests pass it directly.
func findUV(home, pathHit string) string {
	if pathHit != "" {
		return pathHit
	}
	local := filepath.Join(home, ".local", "bin", "uv")
	if runtime.GOOS == "windows" {
		local += ".exe"
	}
	if fileExists(local) {
		return local
	}
	return ""
}

func ensureUV(ctx context.Context, w io.Writer, home string) (string, error) {
	if uv := findUV(home, lookPath("uv")); uv != "" {
		return uv, nil
	}
	fmt.Fprintln(w, "Installing uv...")
	if err := runScript(ctx, w, uvInstallURL); err != nil {
		return "", fmt.Errorf("install uv: %w", err)
	}
	uv := findUV(home, "")
	if uv == "" {
		return "", fmt.Errorf("uv install did not create %s", filepath.Join(home, ".local", "bin", "uv"))
	}
	return uv, nil
}

// checkTorchCUDA fails setup when the host driver cannot load the CUDA
// runtime bundled in the torch wheel. Otherwise that shows up later, on
// `vllm serve`, after the cluster has started waiting.
func checkTorchCUDA(ctx context.Context, w io.Writer, venv string) error {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		fmt.Fprintln(w, "Skipping PyTorch CUDA check (nvidia-smi not found).")
		return nil
	}
	fmt.Fprintln(w, "Checking the NVIDIA driver can load PyTorch...")
	if err := run(ctx, w, venvExe(venv, "python"), "-c", "import torch; torch.cuda.init()"); err != nil {
		return fmt.Errorf("PyTorch cannot use this NVIDIA driver: %w (see %s)", err, vllmDocsURL)
	}
	return nil
}

// downloadKernels fetches prebuilt FlashInfer kernels. Without them vLLM
// compiles missing kernels on first start, which needs nvcc. Machines with a
// driver and no CUDA toolkit die during warmup.
func downloadKernels(ctx context.Context, w io.Writer, venv string) error {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		fmt.Fprintln(w, "Skipping vLLM kernel download (nvidia-smi not found).")
		return nil
	}
	fmt.Fprintln(w, "Downloading FlashInfer kernels...")
	vllm := venvExe(venv, "vllm")
	help, _ := exec.CommandContext(ctx, vllm, "-h").CombinedOutput()
	// vLLM 0.33 has no download-kernels subcommand. Later releases wrap this CLI.
	if cliHasSubcommand(string(help), "download-kernels") {
		if err := run(ctx, w, vllm, "download-kernels"); err != nil {
			return fmt.Errorf("vLLM kernel download failed: %w", err)
		}
		return nil
	}
	py := venvExe(venv, "python")
	cmd := exec.CommandContext(ctx, py, "-m", "flashinfer", "download-kernels")
	cmd.Stdout = w
	cmd.Stderr = w
	// FlashInfer refuses to import when an older cubin is already installed.
	cmd.Env = append(os.Environ(), "FLASHINFER_DISABLE_VERSION_CHECK=1")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("vLLM kernel download failed: %w", err)
	}
	return nil
}

func venvExe(venv, name string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venv, "Scripts", name+".exe")
	}
	return filepath.Join(venv, "bin", name)
}

func cliHasSubcommand(help, name string) bool {
	for _, field := range strings.FieldsFunc(help, func(r rune) bool {
		switch r {
		case '{', '}', ',', ' ', '\n', '\t', '\r':
			return true
		default:
			return false
		}
	}) {
		if field == name {
			return true
		}
	}
	return false
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

// InstalledVersion reads `<bin> --version`. The last field is the release.
func InstalledVersion(bin string) string {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return ""
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil && text == "" {
		return ""
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
