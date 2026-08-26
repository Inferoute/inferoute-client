//go:build !windows

package cloudflare

import (
	"os"
	"os/exec"
	"syscall"
)

type procJob struct{}

func newProcJob() (*procJob, error) {
	return &procJob{}, nil
}

func (j *procJob) assign(*os.Process) error { return nil }

func (j *procJob) close() {}

func configureCmd(*exec.Cmd) {}

func processAlive(p *os.Process) bool {
	if p == nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func terminateProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(syscall.SIGTERM)
}
