package catalog

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// TestCommonExecutableDirectoriesCoversDesktopToolchains 验证桌面进程补齐跨平台通用安装目录。
func TestCommonExecutableDirectoriesCoversDesktopToolchains(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "tester")
	environment, err := buildEnvironment([]string{
		"MISE_DATA_DIR=" + filepath.Join(home, "mise-data"),
		"ASDF_DATA_DIR=" + filepath.Join(home, "asdf-data"),
		"PNPM_HOME=" + filepath.Join(home, "pnpm-home"),
		"GOBIN=" + filepath.Join(home, "go-bin"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		// name 是测试平台名称。
		name string
		// goos 是待投影的 Go 平台标识。
		goos string
		// expected 是该平台必须包含的通用目录。
		expected []string
	}{
		{
			name: "macOS",
			goos: "darwin",
			expected: []string{
				filepath.Join(home, "mise-data", "shims"),
				filepath.Join(home, "asdf-data", "shims"),
				filepath.Join(home, "pnpm-home"),
				filepath.Join(home, "go-bin"),
				filepath.Join(home, ".bun", "bin"),
				filepath.Join(home, ".cargo", "bin"),
				"/opt/homebrew/bin",
				"/usr/local/bin",
			},
		},
		{
			name: "Linux",
			goos: "linux",
			expected: []string{
				filepath.Join(home, ".local", "bin"),
				filepath.Join(home, ".volta", "bin"),
				"/home/linuxbrew/.linuxbrew/bin",
				"/snap/bin",
			},
		},
		{
			name: "Windows",
			goos: "windows",
			expected: []string{
				filepath.Join(home, "AppData", "Roaming", "npm"),
				filepath.Join(home, "scoop", "shims"),
				filepath.Join(home, ".bun", "bin"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directories := commonExecutableDirectories(test.goos, home, environment)
			for _, expected := range test.expected {
				if !slices.Contains(directories, expected) {
					t.Errorf("directories missing %q: %v", expected, directories)
				}
			}
			for _, directory := range directories {
				if directory == filepath.Join(home, ".kimi-code", "bin") {
					t.Fatal("common discovery must not contain a Kimi-specific directory")
				}
			}
		})
	}
}

// TestPreparedEnvironmentResolvesCLIFromCommonShim 验证通用 shim 目录真实参与可执行文件解析。
func TestPreparedEnvironmentResolvesCLIFromCommonShim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows executable suffix is covered by platform-specific tests")
	}
	home := t.TempDir()
	shimDirectory := filepath.Join(home, ".local", "share", "mise", "shims")
	if err := os.MkdirAll(shimDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(shimDirectory, "sample-cli")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	environment, err := buildPreparedEnvironment([]string{"PATH="}, nil, home)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveExecutable([]string{"sample-cli"}, environment)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != executable {
		t.Fatalf("resolved executable = %q, want %q", resolved, executable)
	}
}
