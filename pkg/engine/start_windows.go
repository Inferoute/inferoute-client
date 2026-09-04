//go:build windows

package engine

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const createBreakawayFromJob = 0x01000000

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP |
			windows.DETACHED_PROCESS |
			windows.CREATE_NO_WINDOW |
			createBreakawayFromJob,
	}
}
