package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Detected is the result of looking for an engine binary.
type Detected struct {
	Kind  Kind
	Bin   string
	Found bool
}

// HasNVIDIA reports whether nvidia-smi is on PATH.
func HasNVIDIA() bool {
	_, err := exec.LookPath("nvidia-smi")
	return err == nil
}

// Detect looks for the engine binary on PATH and in known install locations.
func Detect(kind Kind) Detected {
	d := Detected{Kind: kind}
	if bin := lookup(kind); bin != "" {
		d.Bin = bin
		d.Found = true
	}
	return d
}

func lookup(kind Kind) string {
	home, _ := os.UserHomeDir()
	switch kind {
	case KindOllama:
		return lookPath("ollama")
	case KindVLLM:
		if home != "" {
			if p := firstExisting(
				filepath.Join(home, ".venv-vllm", "bin", "vllm"),
				filepath.Join(home, ".venv-vllm", "Scripts", "vllm.exe"),
			); p != "" {
				return p
			}
		}
		return lookPath("vllm")
	case KindVLLMMetal:
		if home != "" {
			if p := firstExisting(filepath.Join(home, ".venv-vllm-metal", "bin", "vllm")); p != "" {
				return p
			}
		}
		return lookPath("vllm")
	case KindFreeToken:
		return lookupFreeToken(home)
	}
	return ""
}

func lookPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func findFreeTokenWindows() string {
	return lookupFreeToken("")
}

func lookupFreeToken(home string) string {
	if p := freeTokenCLIBin(); p != "" && fileExists(p) {
		return p
	}
	if p := lookPath("ft"); p != "" && !isFreeTokenDesktopBin(p) {
		return p
	}
	if runtime.GOOS == "windows" || home == "" {
		return ""
	}
	return firstExisting(
		filepath.Join(home, ".local", "bin", "ft"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "inferoute", "venv-freetoken", "bin", "ft"),
	)
}

// ResolveBin picks the engine binary. Configured paths that are missing or
// FreeToken Desktop's bundled ft.exe are ignored.
func ResolveBin(kind Kind, configured string) string {
	if usableEngineBin(kind, configured) {
		return configured
	}
	return Detect(kind).Bin
}

func usableEngineBin(kind Kind, path string) bool {
	if path == "" || !fileExists(path) {
		return false
	}
	return kind != KindFreeToken || !isFreeTokenDesktopBin(path)
}

func isFreeTokenDesktopBin(path string) bool {
	p := strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(p, "freetoken desktop") || strings.Contains(p, "freetoken-desktop")
}
