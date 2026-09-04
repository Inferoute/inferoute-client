package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		if p := lookPath("ft"); p != "" {
			return p
		}
		if runtime.GOOS == "windows" {
			return findFreeTokenWindows()
		}
		if home != "" {
			return firstExisting(
				filepath.Join(home, ".local", "bin", "ft"),
				filepath.Join(os.Getenv("LOCALAPPDATA"), "inferoute", "venv-freetoken", "bin", "ft"),
			)
		}
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
	local := os.Getenv("LOCALAPPDATA")
	pf := os.Getenv("ProgramFiles")
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(local, "FreeToken", "ft.exe"),
		filepath.Join(local, "freetoken", "ft.exe"),
		filepath.Join(local, "Programs", "FreeToken", "ft.exe"),
		filepath.Join(local, "inferoute", "venv-freetoken", "Scripts", "ft.exe"),
		filepath.Join(pf, "FreeToken", "ft.exe"),
		filepath.Join(home, ".local", "bin", "ft.exe"),
	}
	if p := firstExisting(candidates...); p != "" {
		return p
	}
	roots := []string{
		filepath.Join(local, "FreeToken"),
		filepath.Join(local, "freetoken"),
		filepath.Join(local, "Programs", "FreeToken"),
		filepath.Join(pf, "FreeToken"),
	}
	for _, root := range roots {
		if found := walkFor(root, "ft.exe", 4); found != "" {
			return found
		}
	}
	return ""
}
