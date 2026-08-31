//go:build !windows

package exechide

import "os/exec"

// Apply is a no-op off Windows.
func Apply(*exec.Cmd) {}
