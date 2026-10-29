package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
)

func TestUnknownRuntime(t *testing.T) {
	_, err := New(context.Background(), Config{RuntimeID: "missing"})
	var f *contract.Fault
	if !errors.As(err, &f) || f.Code != contract.FaultUnsupported {
		t.Fatalf("error: %v", err)
	}
}
func TestConstructAgents(t *testing.T) {
	for _, id := range []string{"codex", "claude", "kimi", "cursor", "pi", "opencode", "gemini", "grok", "workbuddy", "qoder", "qoder-cn"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "cli")
			if err := os.WriteFile(executable, []byte("#!/bin/sh\ncase \"$*\" in *--version*) echo \"codex-cli 0.99.0\";; esac\n"), 0755); err != nil {
				t.Fatal(err)
			}
			module := filepath.Join(dir, "module")
			if err := os.WriteFile(module, []byte(""), 0600); err != nil {
				t.Fatal(err)
			}
			a, err := New(context.Background(), Config{RuntimeID: id, Executable: executable, PrefixArgs: []string{"--test"}, Environment: []string{"PI_MCP_MODULE_PATH=" + module}, WorkingDirectory: t.TempDir(), StateDirectory: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil || a == nil {
				t.Fatalf("agent=%v error=%v", a, err)
			}
			t.Cleanup(func() {
				if closer, ok := a.(interface{ Close(context.Context) error }); ok {
					_ = closer.Close(context.Background())
				}
			})
		})
	}
}
func TestEnvironmentValue(t *testing.T) {
	if got := environmentValue([]string{"PI_MCP_MODULE_PATH=old", "OTHER=x", "PI_MCP_MODULE_PATH=a=b"}, "PI_MCP_MODULE_PATH"); got != "a=b" {
		t.Fatalf("value=%q", got)
	}
}

// TestQoderProcessConfig 验证显式环境和启动配置实际传入进程。
func TestQoderProcessConfig(t *testing.T) {
	t.Setenv("NEUX_NATIVE_SENTINEL", "inherited")
	for _, tc := range []struct {
		// name 标识本次显式环境场景。
		name string
		// env 是允许传给受控进程的全部环境。
		env []string
		// want 是进程应观察到的两个环境变量。
		want string
	}{
		{"empty", []string{}, "absent|absent"},
		{"explicit", []string{"NEUX_NATIVE_VALUE=provided"}, "absent|provided"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "capture")
			cli := filepath.Join(dir, "cli")
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\n%%s\n%%s|%%s\n' \"$*\" \"$PWD\" \"${NEUX_NATIVE_SENTINEL-absent}\" \"${NEUX_NATIVE_VALUE-absent}\" > %q\nexec /bin/cat >/dev/null\n", out)
			if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			a, err := New(context.Background(), Config{RuntimeID: "qoder", Executable: cli, PrefixArgs: []string{"--prefix", "value"}, Environment: tc.env, WorkingDirectory: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.(interface{ Close(context.Context) error }).Close(context.Background()) })
			var data []byte
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				data, err = os.ReadFile(out)
				if err == nil && len(data) > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			physicalDir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(lines) != 3 || lines[0] != "--prefix value --acp" || lines[1] != physicalDir || lines[2] != tc.want {
				t.Fatalf("capture=%q", data)
			}
		})
	}
}

// TestConstructionFailureReturnsNil 验证构造失败不会返回带类型的空接口。
func TestConstructionFailureReturnsNil(t *testing.T) {
	for _, id := range []string{"codex", "claude", "kimi", "cursor", "pi", "opencode", "gemini", "grok", "workbuddy", "qoder", "qoder-cn"} {
		t.Run(id, func(t *testing.T) {
			a, err := New(context.Background(), Config{RuntimeID: id, Executable: filepath.Join(t.TempDir(), "missing"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err == nil || a != nil {
				t.Fatalf("agent=%v error=%v", a, err)
			}
		})
	}
}
