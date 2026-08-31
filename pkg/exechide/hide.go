//go:build windows

package exechide

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Apply hides the child console on Windows. Console-subsystem binaries
// (nvidia-smi, cloudflared) otherwise flash a terminal for every spawn.
func Apply(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}
