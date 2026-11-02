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
