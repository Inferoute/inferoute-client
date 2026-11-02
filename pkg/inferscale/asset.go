package inferscale

import (
	"fmt"
	"os"
	"runtime"
)

// Release is the only published tag. It is overwritten on every push to main.
// shortcut: set INFERSCALE_VERSION when a non-moving tag exists.
const Release = "edge"

func Version() string {
	if v := os.Getenv("INFERSCALE_VERSION"); v != "" {
		return v
	}
	return Release
}

// Asset names the release files for this OS and CPU. Windows is one zip that
// also contains wintun.dll. Linux and macOS are two binaries.
func Asset(goos, goarch string) (cli, daemon string, err error) {
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", "", fmt.Errorf("unsupported arch %s", goarch)
	}
	switch goos {
	case "linux", "darwin":
		return "inferscale-" + goos + "-" + goarch, "inferscaled-" + goos + "-" + goarch, nil
	case "windows":
		return "inferscale-windows-" + goarch + ".zip", "", nil
	default:
		return "", "", fmt.Errorf("unsupported OS %s", goos)
	}
}

func downloadURL(name string) string {
	return "https://github.com/Inferoute/inferoute-tailscale/releases/download/" + Version() + "/" + name
}

func hostAsset() (cli, daemon string, err error) {
	return Asset(runtime.GOOS, runtime.GOARCH)
}
