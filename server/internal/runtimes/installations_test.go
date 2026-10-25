package runtimes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/JieWaZi/acp-go/pkg/nativeacp"
)

func TestDiscoverInstallationsKeepsMultipleVersionsAndMissingHistory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	directories := []string{t.TempDir(), t.TempDir()}
	for index, directory := range directories {
		version := "1.0"
		if index == 1 {
			version = "2.0"
		}
		if err := os.WriteFile(filepath.Join(directory, "codex"), []byte("#!/bin/sh\nprintf '"+version+"\\n'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	request := DetectRequest{
		RuntimeID:       RuntimeIDCodex,
		HomeDirectory:   t.TempDir(),
		BaseEnvironment: []string{"PATH=" + directories[0] + string(filepath.ListSeparator) + directories[1]},
	}
	missing := filepath.Join(t.TempDir(), "codex")
	installations := sdk.DiscoverInstallations(context.Background(), request, []CLIInstallation{{
		Path: missing, Version: "0.9", Available: true,
	}})
	versions := make(map[string]CLIInstallation, len(installations))
	for _, installation := range installations {
		versions[installation.Path] = installation
	}
	for index, directory := range directories {
		path := filepath.Join(directory, "codex")
		want := "1.0"
		if index == 1 {
			want = "2.0"
		}
		if !versions[path].Available || versions[path].Version != want {
			t.Fatalf("installation %s = %+v, want %s", path, versions[path], want)
		}
	}
	if versions[missing].Available || versions[missing].Version != "0.9" {
		t.Fatalf("missing installation = %+v", versions[missing])
	}
}

// installationProbeFixture 复用同一测试二进制，屏障不再启动 shell、touch 或 sleep 子进程。
func installationProbeFixture(t *testing.T) (string, []string, []string, []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses executable aliases through POSIX symlinks")
	}
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, "first"), filepath.Join(root, "second")}
	for _, path := range paths {
		if err := os.Symlink(executable, path); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"-test.run=^TestInstallationVersionProbeProcess$", "--"}
	environment := []string{"PATH=", "ALLY_INSTALLATION_PROBE_ROOT=" + root}
	return root, paths, args, environment
}

// TestInstallationVersionProbeProcess 在两个真实版本进程中各自登记启动，等待对端进入后才输出版本。
func TestInstallationVersionProbeProcess(t *testing.T) {
	root := os.Getenv("ALLY_INSTALLATION_PROBE_ROOT")
	if root == "" {
		return
	}
	index := 0
	switch filepath.Base(os.Args[0]) {
	case "first":
	case "second":
		index = 1
	default:
		os.Exit(2)
	}
	marker := filepath.Join(root, fmt.Sprintf("started-%d", index))
	if err := os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339Nano)), 0600); err != nil {
		os.Exit(2)
	}
	peer := filepath.Join(root, fmt.Sprintf("started-%d", 1-index))
	for {
		if _, err := os.Stat(peer); err == nil {
			break
		} else if !os.IsNotExist(err) {
			os.Exit(2)
		}
		// 进程内等待避免外部 sleep 的启动成本，又不使用消耗 CPU 的忙循环。
		time.Sleep(time.Millisecond)
	}
	fmt.Printf("1.%d.0\n", index)
	os.Exit(0)
}

// TestDiscoverInstallationsProbesPathsConcurrently 验证两个真实版本进程必须同时启动才能通过屏障。
func TestDiscoverInstallationsProbesPathsConcurrently(t *testing.T) {
	root, paths, args, environment := installationProbeFixture(t)
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	definition := sdk.definitions[RuntimeIDCodex]
	definition.binaryCandidates = paths
	definition.prefixArgs = args
	sdk.definitions[RuntimeIDCodex] = definition
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := sdk.DiscoverInstallations(ctx, DetectRequest{RuntimeID: RuntimeIDCodex, HomeDirectory: root, BaseEnvironment: environment}, []CLIInstallation{{Path: paths[0]}, {Path: paths[1]}})
	for index := range paths {
		marker, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("started-%d", index)))
		if err != nil {
			t.Fatalf("probe %d did not enter its barrier: %v", index, err)
		}
		t.Logf("probe %d entered barrier at %s", index, marker)
	}
	if len(got) != 2 {
		t.Fatalf("installations=%+v", got)
	}
	for index, installation := range got {
		if !installation.Available || installation.Version != fmt.Sprintf("1.%d.0", index) {
			t.Fatalf("version probes failed to run concurrently: %+v", got)
		}
	}
}

// TestInstallationVersionProbeBarrierRejectsSequentialStart 证明顺序执行不能获得两个有效版本，防止屏障退化为空验证。
func TestInstallationVersionProbeBarrierRejectsSequentialStart(t *testing.T) {
	root, paths, args, environment := installationProbeFixture(t)
	args = append(args, "--version")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first := nativeacp.RuntimeVersion(ctx, paths[0], args, environment, "")
	if first != "" || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("first probe crossed barrier without peer: version=%q context=%v", first, ctx.Err())
	}
	if _, err := os.Stat(filepath.Join(root, "started-0")); err != nil {
		t.Fatalf("first probe did not enter barrier: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "started-1")); !os.IsNotExist(err) {
		t.Fatalf("second probe started before first completed: %v", err)
	}
	secondContext, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if second := nativeacp.RuntimeVersion(secondContext, paths[1], args, environment, ""); second != "1.1.0" {
		t.Fatalf("second probe could not observe the completed first probe: %q", second)
	}
}

// TestDiscoverInstallationsKeepsVersionAfterUnknownOutput 验证未知输出不覆盖已知安装版本。
func TestDiscoverInstallationsKeepsVersionAfterUnknownOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	root := t.TempDir()
	path := filepath.Join(root, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'unknown\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	definition := sdk.definitions[RuntimeIDCodex]
	definition.binaryCandidates = []string{path}
	sdk.definitions[RuntimeIDCodex] = definition
	got := sdk.DiscoverInstallations(context.Background(), DetectRequest{RuntimeID: RuntimeIDCodex, HomeDirectory: root, BaseEnvironment: []string{"PATH="}}, []CLIInstallation{{Path: path, Version: "1.2.3"}})
	if len(got) != 1 || got[0].Version != "1.2.3" {
		t.Fatalf("known installation version overwritten: %+v", got)
	}
}

// TestDiscoverInstallationsStillFindsPATHAfterExplicitSelection 验证保存明确路径后仍能枚举 PATH 中的新版本。
func TestDiscoverInstallationsStillFindsPATHAfterExplicitSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	selected := filepath.Join(t.TempDir(), "chosen-codex")
	otherDirectory := t.TempDir()
	other := filepath.Join(otherDirectory, "codex")
	for _, path := range []string{selected, other} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '1.2.3\\n'\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	request := DetectRequest{RuntimeID: RuntimeIDCodex, HomeDirectory: t.TempDir(), BaseEnvironment: []string{"PATH=" + otherDirectory}, LaunchOverride: LaunchOverride{Binary: selected}}
	installations := sdk.DiscoverInstallations(context.Background(), request, nil)
	found := map[string]bool{}
	for _, installation := range installations {
		found[installation.Path] = installation.Available
	}
	if !found[selected] || !found[other] {
		t.Fatalf("explicit selection hid PATH install: %+v", installations)
	}
}
