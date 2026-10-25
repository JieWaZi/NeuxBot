//go:build !windows

package commandbridge

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReviewBridgeChildProcess(t *testing.T) {
	if os.Getenv("REVIEW_BRIDGE_CHILD") != "1" {
		return
	}
	child := exec.Command("/bin/sleep", "20")
	child.Stdout = os.Stdout
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	_ = os.WriteFile(os.Getenv("REVIEW_BRIDGE_PID"), []byte(fmt.Sprint(child.Process.Pid)), 0600)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
	}
	os.Exit(0)
}
func TestReviewCancellationReapsBridgeChildren(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	binary, _ := os.Executable()
	driver := New(Config{Command: binary, Args: []string{"-test.run=^TestReviewBridgeChildProcess$"}, Directory: t.TempDir(), DataDirectory: t.TempDir(), PluginID: "p", Environment: append(os.Environ(), "REVIEW_BRIDGE_CHILD=1", "REVIEW_BRIDGE_PID="+pidFile)})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := driver.Status(ctx, true); done <- err }()
	var pid int
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			fmt.Sscan(string(data), &pid)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		cancel()
		t.Fatal("helper child not started")
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	cancel()
	select {
	case err := <-done:
		if syscall.Kill(pid, 0) == nil {
			t.Fatalf("bridge returned %v but child still alive", err)
		}
	case <-time.After(4 * time.Second):
		alive := syscall.Kill(pid, 0) == nil
		_ = syscall.Kill(pid, syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatalf("cancellation remains blocked after 4s; child still alive=%t", alive)
	}
}
