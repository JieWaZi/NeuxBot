//go:build !windows

package commandbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		var request struct {
			// ID 是宿主请求标识。
			ID int `json:"id"`
			// Method 是初始化或授权操作。
			Method string `json:"method"`
			// Params 是宿主固定的初始化上下文。
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		if request.Method == constants.PluginAuthInitializeMethod {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"protocol": constants.PluginAuthProtocol, "context": request.Params}})
		}
	}
	os.Exit(0)
}
func TestReviewCancellationReapsBridgeChildren(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	binary, _ := os.Executable()
	driver := New(Config{Command: binary, Args: []string{"-test.run=^TestReviewBridgeChildProcess$"}, Directory: t.TempDir(), DataDirectory: t.TempDir(), PluginID: "p", VersionID: "version", Digest: strings.Repeat("a", 64), Environment: append(os.Environ(), "REVIEW_BRIDGE_CHILD=1", "REVIEW_BRIDGE_PID="+pidFile)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := driver.Status(ctx, true); done <- err }()
	var pid int
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if count, parseErr := fmt.Sscan(string(data), &pid); parseErr == nil && count == 1 && pid > 0 {
				break
			}
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
