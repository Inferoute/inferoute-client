//go:build !windows

package exechide

import (
	"os/exec"
	"testing"
)

func TestApplyNoop(t *testing.T) {
	cmd := exec.Command("true")
	Apply(cmd)
	if cmd.SysProcAttr != nil {
		t.Fatalf("SysProcAttr = %#v, want nil", cmd.SysProcAttr)
	}
}

func TestApplyNilCmd(t *testing.T) {
	Apply(nil)
}
