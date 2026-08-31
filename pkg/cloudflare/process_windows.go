//go:build windows

package cloudflare

import (
	"os"
	"os/exec"
	"unsafe"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/exechide"
	"golang.org/x/sys/windows"
)

const stillActive = 259

type procJob struct {
	handle windows.Handle
}

func newProcJob() (*procJob, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return &procJob{}, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(h)
		return &procJob{}, err
	}
	return &procJob{handle: h}, nil
}

func (j *procJob) assign(p *os.Process) error {
	if j == nil || j.handle == 0 || p == nil {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.AssignProcessToJobObject(j.handle, h)
}

func (j *procJob) close() {
	if j == nil || j.handle == 0 {
		return
	}
	windows.CloseHandle(j.handle)
	j.handle = 0
}

func configureCmd(cmd *exec.Cmd) {
	exechide.Apply(cmd)
}

func processAlive(p *os.Process) bool {
	if p == nil {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p.Pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

func terminateProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
