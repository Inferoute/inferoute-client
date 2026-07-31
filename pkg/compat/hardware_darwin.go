//go:build darwin

package compat

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func systemRAMBytes() (int64, error) {
	n, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0, fmt.Errorf("sysctl hw.memsize: %w", err)
	}
	return int64(n), nil
}
