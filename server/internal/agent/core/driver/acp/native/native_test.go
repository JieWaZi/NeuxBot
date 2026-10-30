package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

func TestUnknownRuntime(t *testing.T) {
	_, err := New(context.Background(), Config{RuntimeID: "missing"})
	var f *contract.Fault
	if !errors.As(err, &f) || f.Code != contract.FaultUnsupported {
		t.Fatalf("error: %v", err)
	}
}
func TestConstructAgents(t *testing.T) {
	for _, descriptor := range Descriptors() {
		id := descriptor.ID
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
	for _, descriptor := range Descriptors() {
		id := descriptor.ID
		t.Run(id, func(t *testing.T) {
			a, err := New(context.Background(), Config{RuntimeID: id, Executable: filepath.Join(t.TempDir(), "missing"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err == nil || a != nil {
				t.Fatalf("agent=%v error=%v", a, err)
			}
		})
	}
}

// TestQoderConfigCategories 验证公开工厂将 Qoder 与 Qoder CN 的错误配置类别收回对应适配器。
// 子进程只运行本测试二进制，不启动用户安装的 CLI。
func TestQoderConfigCategories(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"qoder", "qoder-cn"} {
		t.Run(id, func(t *testing.T) {
			a, err := New(context.Background(), Config{RuntimeID: id, Executable: binary, PrefixArgs: []string{"-test.run=^TestQoderConfigProcess$", "--"}, Environment: []string{"NEUX_QODER_CONFIG_PROCESS=1"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.(interface{ Close(context.Context) error }).Close(context.Background()) })
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := a.Initialize(ctx, sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber}); err != nil {
				t.Fatal(err)
			}
			session, err := a.NewSession(ctx, sdk.NewSessionRequest{Cwd: t.TempDir(), McpServers: []sdk.McpServer{}})
			if err != nil {
				t.Fatal(err)
			}
			check := func(options []sdk.SessionConfigOption, reasoning string) {
				t.Helper()
				if len(options) != 1 || options[0].Select == nil || options[0].Select.Category == nil || *options[0].Select.Category != sdk.SessionConfigOptionCategoryThoughtLevel || options[0].Select.Id != "reasoning_effort" || string(options[0].Select.CurrentValue) != reasoning {
					t.Fatalf("Qoder reasoning was exposed as model or lost its native identity: %+v", options)
				}
			}
			check(session.ConfigOptions, "low")
			response, err := a.SetSessionConfigOption(ctx, sdk.SetSessionConfigOptionRequest{ValueId: &sdk.SetSessionConfigOptionValueId{SessionId: session.SessionId, ConfigId: "reasoning_effort", Value: "high"}})
			if err != nil {
				t.Fatal(err)
			}
			check(response.ConfigOptions, "high")
		})
	}
}

// TestQoderConfigProcess 在受控子进程中模拟 Qoder 原生的错误类别与原生 setter。
func TestQoderConfigProcess(t *testing.T) {
	if os.Getenv("NEUX_QODER_CONFIG_PROCESS") != "1" {
		return
	}
	if strings.Contains(strings.Join(os.Args, " "), "--version") {
		fmt.Fprintln(os.Stdout, "qoder 0.1.0")
		os.Exit(0)
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var request map[string]json.RawMessage
		if err := decoder.Decode(&request); err != nil {
			os.Exit(0)
		}
		var method string
		_ = json.Unmarshal(request["method"], &method)
		var result any
		switch method {
		case "initialize":
			result = sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber}
		case "session/new", "session/set_config_option":
			value := "low"
			if method == "session/set_config_option" {
				var config sdk.SetSessionConfigOptionRequest
				if err := json.Unmarshal(request["params"], &config); err != nil || config.ValueId == nil || config.ValueId.ConfigId != "reasoning_effort" {
					os.Exit(2)
				}
				value = string(config.ValueId.Value)
			}
			result = map[string]any{"sessionId": "native", "configOptions": []any{map[string]any{"id": "reasoning_effort", "name": "Reasoning", "type": "select", "category": "model", "currentValue": value, "options": []any{map[string]any{"value": "low", "name": "Low"}, map[string]any{"value": "high", "name": "High"}}}}}
		default:
			os.Exit(2)
		}
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": result}); err != nil {
			os.Exit(2)
		}
	}
}
