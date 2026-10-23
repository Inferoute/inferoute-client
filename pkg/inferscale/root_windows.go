//go:build windows

package inferscale

import "fmt"

func requireRoot() error {
	return fmt.Errorf("inferscale needs a Linux TUN")
}
