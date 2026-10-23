//go:build !windows

package inferscale

import (
	"fmt"
	"os"
)

func requireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("inferscale needs root to create %s; re-run setup as root", TUN)
	}
	return nil
}
