//go:build windows

package exechide

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestApplySetsCREATE_NO_WINDOW(t *testing.T) {
	cmd := exec.Command("nvidia-smi")
	Apply(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow = false, want true")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Errorf("CreationFlags = %#x, want CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
}

func TestApplyNilCmd(t *testing.T) {
	Apply(nil)
}
