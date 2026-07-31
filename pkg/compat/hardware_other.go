//go:build !darwin && !linux

package compat

import "fmt"

func systemRAMBytes() (int64, error) {
	return 0, fmt.Errorf("system RAM detection not implemented on this OS")
}
