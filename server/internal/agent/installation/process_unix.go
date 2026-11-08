//go:build !windows

package installation

import (
	"os/exec"
	"syscall"
)

// configureProcess makes cancellation reap npm and any inherited subprocesses together.
func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}
