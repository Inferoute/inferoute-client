package cloudflare

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func cloudflaredLogPath() string {
	return filepath.Join(os.TempDir(), "cloudflared-debug.log")
}

func findCloudflared() (string, error) {
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("cloudflared.exe"); err == nil {
			return p, nil
		}
	}

	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "cloudflared"),
			filepath.Join(dir, "cloudflared.exe"),
		)
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidates = append(candidates, filepath.Join(local, "inferoute", "bin", "cloudflared.exe"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "bin", "cloudflared"),
			filepath.Join(home, "bin", "cloudflared.exe"),
		)
	}
	for _, c := range candidates {
		st, err := os.Stat(c)
		if err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("cloudflared executable not found in PATH or standard install locations")
}
