//go:build windows

package commandbridge

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// configureProcessGroup 避免桥接加入宿主控制台进程组。
func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
}

// terminateProcessGroup 在Windows先以EOF请求桥接自行清理，兜底统一终止进程树。
func terminateProcessGroup(command *exec.Cmd) {}

func killProcessGroup(command *exec.Cmd) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run()
	_ = command.Process.Kill()
}
