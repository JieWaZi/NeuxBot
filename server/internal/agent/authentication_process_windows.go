//go:build windows

package agent

import (
	"os/exec"
	"strconv"
	"syscall"
)

// configureAuthenticationProcess cancels the login process tree without a visible console.
func configureAuthenticationProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000200}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return exec.Command("taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run()
	}
}
