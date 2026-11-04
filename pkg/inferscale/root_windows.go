//go:build windows

package inferscale

import (
	"context"
	"fmt"
)

func euid() int { return 0 }

func ensureSudo(context.Context) error {
	return fmt.Errorf("inferscale needs a Linux TUN")
}

func rootOwned(string) error {
	return fmt.Errorf("inferscale needs a Linux TUN")
}

func rejectSymlink(string) error {
	return fmt.Errorf("inferscale needs a Linux TUN")
}

func lockDownDir(context.Context, string) error {
	return fmt.Errorf("inferscale needs a Linux TUN")
}
