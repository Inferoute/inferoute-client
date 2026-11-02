//go:build !windows

package inferscale

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func euid() int { return os.Geteuid() }

// ensureSudo caches a sudo credential when this process is not root.
// Later inferscale commands use sudo -n and do not prompt again.
func ensureSudo(ctx context.Context) error {
	if os.Geteuid() == 0 {
		return nil
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("inferscale needs root to create %s, and sudo is not installed", TUN)
	}
	fmt.Fprintf(os.Stderr, "inferscale needs root to create %s\n", TUN)
	cmd := exec.CommandContext(ctx, "sudo", "-v")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo: %w", err)
	}
	return nil
}
