//go:build !windows

package agent

import (
	"os/exec"
	"syscall"
)

// configureAuthenticationProcess cancels the selected login process and its children together.
func configureAuthenticationProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}
