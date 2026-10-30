package native

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestDescriptorSnapshotsCannotChangeLaunchPolicy 验证调用方修改目录副本不会污染下一次启动的权限候选或安装入口。
func TestDescriptorSnapshotsCannotChangeLaunchPolicy(t *testing.T) {
	descriptor, ok := Lookup("codex")
	if !ok {
		t.Fatal("Codex descriptor missing")
	}
	descriptor.Modes.Unrestricted[0] = "read-only"
	descriptor.BinaryCandidates[0] = "other"
	for _, item := range Descriptors() {
		if item.ID == "codex" {
			item.Modes.Unrestricted[0] = "other"
		}
	}
	again, _ := Lookup("codex")
	if !reflect.DeepEqual(again.Modes.Unrestricted, []string{"agent-full-access"}) || !reflect.DeepEqual(again.BinaryCandidates, []string{"codex"}) {
		t.Fatalf("descriptor mutation escaped snapshot: %+v", again)
	}
}

// TestNativeConfigurationReachesCLI 验证冻结的插件和设置事实由统一工厂转为真实进程参数。
func TestNativeConfigurationReachesCLI(t *testing.T) {
	for _, tc := range []struct {
		// id 是需要消费原生配置覆盖的适配器。
		id string
		// want 是受控版本进程应收到的完整参数。
		want string
	}{
		{"codex", "--prefix\n-c\na=1\n-c\nz=2\n--version\n"},
		{"claude", "--prefix\n--settings\n/settings.json\n--plugin-dir\n/plugin-a\n--plugin-dir\n/plugin-b\n--version\n"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "capture")
			executable := filepath.Join(dir, "cli")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\necho 'codex-cli 0.99.0'\n"
			if err := os.WriteFile(executable, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			agent, err := New(context.Background(), Config{RuntimeID: tc.id, Executable: executable, PrefixArgs: []string{"--prefix"}, Environment: []string{"CAPTURE=" + capture}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), CodexConfiguration: map[string]string{"z": "2", "a": "1"}, ClaudeSettingsPath: "/settings.json", NativePluginDirectories: []string{"/plugin-a", "/plugin-b"}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.(interface{ Close(context.Context) error }).Close(context.Background()) })
			got, err := os.ReadFile(capture)
			if err != nil || string(got) != tc.want {
				t.Fatalf("process arguments=%q error=%v, want %q", got, err, tc.want)
			}
		})
	}
}
