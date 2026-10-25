//go:build !windows

package commandbridge

import (
	"os/exec"
	"syscall"
	"time"
)

// configureProcessGroup 将桥接及其继承子进程纳入独立回收边界。
func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessGroup(command *exec.Cmd) {
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
}

func killProcessGroup(command *exec.Cmd) {
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	// 等待内核完成子进程退出与收养，避免宿主先于进程组清理返回。
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) && syscall.Kill(-command.Process.Pid, 0) == nil {
		time.Sleep(10 * time.Millisecond)
	}
}
