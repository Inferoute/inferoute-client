//go:build windows

package inferscale

import "os/exec"

func detach(cmd *exec.Cmd) {}
