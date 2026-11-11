//go:build windows

package installation

import (
	"os/exec"
	"strconv"
	"syscall"
)

// configureProcess terminates the fixed installer's whole process tree on cancellation.
func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return exec.Command("taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run()
	}
}
