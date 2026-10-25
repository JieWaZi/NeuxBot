//go:build windows

package terminalruntime

import (
	"os/exec"
	"strconv"
	"syscall"
)

func configureGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
}
func killGroup(command *exec.Cmd) {
	if command.Process != nil {
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run()
	}
}
