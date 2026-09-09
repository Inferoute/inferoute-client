package engine

import (
	"bytes"
	"fmt"
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
	// Unusable is set when Bin exists but cannot be executed (broken venv shebang).
	Unusable string
}

// HasNVIDIA reports whether nvidia-smi is on PATH.
func HasNVIDIA() bool {
	_, err := exec.LookPath("nvidia-smi")
	return err == nil
}

// Detect looks for the engine binary on PATH and in known install locations.
func Detect(kind Kind) Detected {
	d := Detected{Kind: kind}
	bin := lookup(kind)
	if bin == "" {
		return d
	}
	d.Bin = bin
	if runnableBin(bin) {
		d.Found = true
		return d
	}
	d.Unusable = brokenBinReason(bin)
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
	d := Detect(kind)
	if d.Found {
		return d.Bin
	}
	return ""
}

func usableEngineBin(kind Kind, path string) bool {
	if path == "" || !runnableBin(path) {
		return false
	}
	return kind != KindFreeToken || !isFreeTokenDesktopBin(path)
}

// runnableBin reports whether path is a regular file we can actually exec.
// A console-script wrapper whose shebang interpreter is gone (typical after a
// Homebrew Python upgrade) is not runnable — os.Stat on the wrapper still succeeds.
func runnableBin(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	if st.Mode()&0o111 == 0 {
		return false
	}
	interp, envArg, err := shebangInterpreter(path)
	if err != nil {
		return false
	}
	if interp == "" {
		return true
	}
	if envArg != "" {
		return lookPath(envArg) != ""
	}
	ist, err := os.Stat(interp)
	return err == nil && !ist.IsDir()
}

func shebangInterpreter(path string) (interp, envArg string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if n < 2 {
		return "", "", err
	}
	if buf[0] != '#' || buf[1] != '!' {
		return "", "", nil
	}
	line := buf[2:n]
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(string(line))
	if len(fields) == 0 {
		return "", "", fmt.Errorf("empty shebang in %s", path)
	}
	interp = fields[0]
	if filepath.Base(interp) == "env" && len(fields) > 1 {
		arg := fields[1]
		if arg == "-S" && len(fields) > 2 {
			arg = fields[2]
		}
		return interp, arg, nil
	}
	return interp, "", nil
}

func brokenBinReason(path string) string {
	interp, envArg, err := shebangInterpreter(path)
	if err != nil {
		return err.Error()
	}
	target := interp
	if envArg != "" {
		target = envArg
	}
	if target == "" {
		return "not executable"
	}
	return fmt.Sprintf("interpreter %s is missing (broken venv)", target)
}

func isFreeTokenDesktopBin(path string) bool {
	p := strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(p, "freetoken desktop") || strings.Contains(p, "freetoken-desktop")
}
