package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/agentclient"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// TestNewNativeRuntimeRegistry verifies runtime discovery and real instruction and skill directories without fabricating model catalogs.
func TestNewNativeRuntimeRegistry(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"opencode", "gemini", "grok"} {
		definition, ok := definitions[id]
		if !ok {
			t.Errorf("runtime missing: %s", id)
			continue
		}
		instruction := "AGENTS.md"
		if id == "gemini" {
			instruction = "GEMINI.md"
		}
		if definition.instructionFileName != instruction || definition.skillDirectoryName != "."+id || len(definition.models) != 0 {
			t.Errorf("invented native files/models: %+v", definition)
		}
	}
}

// TestQoderCNIsUnsupported rejects the removed runtime instead of substituting Qoder.
func TestQoderCNIsUnsupported(t *testing.T) {
	catalog, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(BaseRuntimeIDs(), "qoder-cn") {
		t.Fatal("removed runtime is still advertised")
	}
	_, err = catalog.ResolveExecutable(DetectRequest{RuntimeID: "qoder-cn", HomeDirectory: t.TempDir(), BaseEnvironment: []string{"PATH="}})
	var stable statuserror.CommonError
	if !errors.As(err, &stable) || stable.Code() != status_error.RuntimeNotRegistered.Code() {
		t.Fatalf("removed runtime resolution = %v", err)
	}
}

// TestQoderNativeInstallDiscovery verifies official native path resolution without PATH and explicit path overrides.
func TestQoderNativeInstallDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	for _, id := range []string{RuntimeIDQoder} {
		t.Run(id, func(t *testing.T) {
			home := t.TempDir()
			sdk, err := NewCatalog(CatalogConfig{})
			if err != nil {
				t.Fatal(err)
			}
			definition := sdk.definitions[id]
			binary := filepath.Join(home, definition.userBinaryCandidates[0])
			if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '1.2.3\\n'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			request := DetectRequest{RuntimeID: id, HomeDirectory: home, BaseEnvironment: []string{"PATH="}}
			resolved, err := sdk.ResolveExecutable(request)
			if err != nil || resolved != binary {
				t.Fatalf("native executable = %s, %v", resolved, err)
			}
			installations := sdk.DiscoverInstallations(context.Background(), request, nil)
			found := false
			for _, installation := range installations {
				if installation.Path == binary && installation.Available && installation.Version == "1.2.3" {
					found = true
				}
			}
			if !found {
				t.Fatalf("native installation omitted: %+v", installations)
			}
			request.LaunchOverride.Binary = filepath.Join(home, "missing-explicit-binary")
			if _, err := sdk.ResolveExecutable(request); err == nil {
				t.Fatal("explicit binary silently fell back to native installation")
			}
		})
	}
}

// TestQoderDesktopDispatcherIsNotNativeInstall prevents desktop entry dispatch scripts from being reported as native CLI installations.
func TestQoderDesktopDispatcherIsNotNativeInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	for _, id := range []string{RuntimeIDQoder} {
		home := t.TempDir()
		sdk, err := NewCatalog(CatalogConfig{})
		if err != nil {
			t.Fatal(err)
		}
		definition := sdk.definitions[id]
		entry := filepath.Join(home, "."+id, "entry", definition.binaryCandidates[0])
		if err := os.MkdirAll(filepath.Dir(entry), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(entry, []byte("#!/bin/sh\nexit 127\n"), 0700); err != nil {
			t.Fatal(err)
		}
		request := DetectRequest{RuntimeID: id, HomeDirectory: home, BaseEnvironment: []string{"PATH="}}
		if _, err := sdk.ResolveExecutable(request); err == nil {
			t.Fatal("desktop dispatcher treated as installed CLI")
		}
	}
}

// TestQoderRegistryPreservesNativeInstallation verifies native directories, transports, and launch argument constraints.
func TestQoderRegistryPreservesNativeInstallation(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions[RuntimeIDQoder]
	if definition.name != "Qoder" || definition.instructionFileName != "AGENTS.md" || definition.skillDirectoryName != ".qoder" || definition.configHomeEnvironmentVariable != "QODER_CONFIG_DIR" || !reflect.DeepEqual(definition.binaryCandidates, []string{"qoder", "qodercli"}) || len(definition.models) != 0 {
		t.Fatalf("native definition: %+v", definition)
	}
	for _, transport := range []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE} {
		if supported, _ := ConnectorCompatibility(RuntimeIDQoder, transport); !supported {
			t.Fatalf("missing transport: %s", transport)
		}
	}
	for flag := range protectedPrefixArguments[RuntimeIDQoder] {
		if LaunchOverrideArgumentsAllowed(RuntimeIDQoder, []string{flag}) || LaunchOverrideArgumentsAllowed(RuntimeIDQoder, []string{flag + "=override"}) {
			t.Fatalf("unsafe override: %s", flag)
		}
	}
}

// TestWorkBuddyRegistryPreservesNativeInstallation verifies only declarations still used by detection and installation.
func TestWorkBuddyRegistryPreservesNativeInstallation(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions[RuntimeIDWorkBuddy]
	if definition.name != "WorkBuddy" || definition.instructionFileName != "CODEBUDDY.md" || definition.skillDirectoryName != ".codebuddy" || definition.configHomeEnvironmentVariable != "CODEBUDDY_CONFIG_DIR" || !reflect.DeepEqual(definition.binaryCandidates, []string{"codebuddy", "cbc"}) || len(definition.models) != 0 {
		t.Fatalf("native definition: %+v", definition)
	}
	for _, transport := range []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE} {
		if ok, _ := ConnectorCompatibility(RuntimeIDWorkBuddy, transport); !ok {
			t.Fatalf("missing transport: %s", transport)
		}
	}
	for _, flag := range []string{"--acp-transport=http", "--permission-mode=fullAccess", "--resume=old", "--model=other", "--mcp-config=other", "--system-prompt=other", "--multitask"} {
		if LaunchOverrideArgumentsAllowed(RuntimeIDWorkBuddy, []string{flag}) {
			t.Fatalf("managed argument override: %s", flag)
		}
	}
}

// TestConnectorCompatibilityMatchesUpstreamTransports locks each runtime's upstream MCP boundaries.
func TestConnectorCompatibilityMatchesUpstreamTransports(t *testing.T) {
	t.Parallel()
	tests := []struct {
		// name identifies the capability combination.
		name string
		// runtimeID is the base runtime under test.
		runtimeID string
		// transport is the target MCP transport.
		transport ConnectorTransport
		// expected reports whether the transport should be supported.
		expected bool
	}{
		{name: "codex stdio", runtimeID: RuntimeIDCodex, transport: ConnectorTransportStdio, expected: true},
		{name: "codex http", runtimeID: RuntimeIDCodex, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "codex sse", runtimeID: RuntimeIDCodex, transport: ConnectorTransportSSE, expected: false},
		{name: "claude stdio", runtimeID: RuntimeIDClaude, transport: ConnectorTransportStdio, expected: true},
		{name: "claude http", runtimeID: RuntimeIDClaude, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "claude sse", runtimeID: RuntimeIDClaude, transport: ConnectorTransportSSE, expected: true},
		{name: "kimi stdio", runtimeID: RuntimeIDKimi, transport: ConnectorTransportStdio, expected: true},
		{name: "kimi http", runtimeID: RuntimeIDKimi, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "kimi sse", runtimeID: RuntimeIDKimi, transport: ConnectorTransportSSE, expected: false},
		{name: "cursor stdio", runtimeID: RuntimeIDCursor, transport: ConnectorTransportStdio, expected: true},
		{name: "cursor http", runtimeID: RuntimeIDCursor, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "cursor sse", runtimeID: RuntimeIDCursor, transport: ConnectorTransportSSE, expected: false},
		{name: "pi stdio", runtimeID: RuntimeIDPi, transport: ConnectorTransportStdio, expected: true},
		{name: "pi http", runtimeID: RuntimeIDPi, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "pi sse", runtimeID: RuntimeIDPi, transport: ConnectorTransportSSE, expected: true},
		{name: "opencode stdio", runtimeID: RuntimeIDOpenCode, transport: ConnectorTransportStdio, expected: true},
		{name: "opencode http", runtimeID: RuntimeIDOpenCode, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "opencode sse", runtimeID: RuntimeIDOpenCode, transport: ConnectorTransportSSE, expected: true},
		{name: "gemini stdio", runtimeID: RuntimeIDGemini, transport: ConnectorTransportStdio, expected: true},
		{name: "gemini http", runtimeID: RuntimeIDGemini, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "gemini sse", runtimeID: RuntimeIDGemini, transport: ConnectorTransportSSE, expected: true},
		{name: "grok stdio", runtimeID: RuntimeIDGrok, transport: ConnectorTransportStdio, expected: true},
		{name: "grok http", runtimeID: RuntimeIDGrok, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "grok sse", runtimeID: RuntimeIDGrok, transport: ConnectorTransportSSE, expected: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual, reason := ConnectorCompatibility(test.runtimeID, test.transport)
			if actual != test.expected || (actual && reason != "") || (!actual && reason == "") {
				t.Fatalf("compatibility = %v/%q, want %v", actual, reason, test.expected)
			}
		})
	}
}

func TestResolveExecutableRejectsUnregisteredRuntimeWithStableError(t *testing.T) {
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sdk.ResolveExecutable(DetectRequest{RuntimeID: "unregistered-runtime"})
	var stable statuserror.CommonError
	if !errors.As(err, &stable) || stable.Code() != status_error.RuntimeNotRegistered.Code() {
		t.Fatalf("unregistered runtime error = %v, want RuntimeNotRegistered", err)
	}
}

func TestOpenCodeDiscoversBothInstallationsAndHonorsExplicitPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executables")
	}
	home := t.TempDir()
	userBinary := filepath.Join(home, ".opencode", "bin", "opencode")
	oldBinary := filepath.Join(t.TempDir(), "opencode")
	for _, binary := range []string{userBinary, oldBinary} {
		if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	request := DetectRequest{
		RuntimeID:       RuntimeIDOpenCode,
		HomeDirectory:   home,
		BaseEnvironment: []string{"PATH=" + filepath.Dir(oldBinary)},
	}
	resolved, err := sdk.ResolveExecutable(request)
	if err != nil || resolved != oldBinary {
		t.Fatalf("default OpenCode binary = %q, %v; want %q", resolved, err, oldBinary)
	}
	installations := sdk.DiscoverInstallations(context.Background(), request, nil)
	paths := make(map[string]bool, len(installations))
	for _, installation := range installations {
		paths[installation.Path] = installation.Available
	}
	if !paths[oldBinary] || !paths[userBinary] {
		t.Fatalf("OpenCode installations = %+v; want both paths", installations)
	}
	request.LaunchOverride.Binary = oldBinary
	resolved, err = sdk.ResolveExecutable(request)
	if err != nil || resolved != oldBinary {
		t.Fatalf("explicit OpenCode binary = %q, %v; want %q", resolved, err, oldBinary)
	}
}

// TestCommonExecutableDirectoriesCoversDesktopToolchains verifies cross-platform common installation directories added for desktop processes.
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
		// name is the test platform name.
		name string
		// goos is the Go platform identifier to project.
		goos string
		// expected lists common directories required on the platform.
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

// TestPreparedEnvironmentResolvesCLIFromCommonShim verifies common shim directories participate in executable resolution.
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
	sdk, err := NewCatalog(CatalogConfig{})
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

// installationProbeFixture reuses the test binary so barriers require no shell, touch, or sleep subprocesses.
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

// TestInstallationVersionProbeProcess registers each real version process and waits for its peer before printing a version.
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
		// Waiting within the process avoids external sleep startup costs without busy-spinning.
		time.Sleep(time.Millisecond)
	}
	fmt.Printf("1.%d.0\n", index)
	os.Exit(0)
}

// TestDiscoverInstallationsProbesPathsConcurrently requires both real version processes to start concurrently to pass the barrier.
func TestDiscoverInstallationsProbesPathsConcurrently(t *testing.T) {
	root, paths, args, environment := installationProbeFixture(t)
	sdk, err := NewCatalog(CatalogConfig{})
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

// TestInstallationVersionProbeBarrierRejectsSequentialStart proves sequential execution cannot yield two valid versions, guarding against a vacuous barrier.
func TestInstallationVersionProbeBarrierRejectsSequentialStart(t *testing.T) {
	root, paths, args, environment := installationProbeFixture(t)
	args = append(args, "--version")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first := agentclient.RuntimeVersion(ctx, paths[0], args, environment, "")
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
	if second := agentclient.RuntimeVersion(secondContext, paths[1], args, environment, ""); second != "1.1.0" {
		t.Fatalf("second probe could not observe the completed first probe: %q", second)
	}
}

// TestDiscoverInstallationsKeepsVersionAfterUnknownOutput verifies unknown output cannot overwrite a known installation version.
func TestDiscoverInstallationsKeepsVersionAfterUnknownOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	root := t.TempDir()
	path := filepath.Join(root, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'unknown\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	sdk, err := NewCatalog(CatalogConfig{})
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

// TestDiscoverInstallationsStillFindsPATHAfterExplicitSelection verifies new PATH versions remain discoverable after saving an explicit path.
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
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
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

func TestLaunchOverrideArgumentsAllowedRejectsAllyManagedArguments(t *testing.T) {
	tests := []struct {
		// name identifies the test scenario.
		name string
		// runtimeID is the built-in runtime owning the arguments.
		runtimeID string
		// arguments contains the leading arguments submitted by the user.
		arguments []string
		// allowed is the expected argument validation result.
		allowed bool
	}{
		{name: "codex safe profile", runtimeID: RuntimeIDCodex, arguments: []string{"--profile", "ally"}, allowed: true},
		{name: "codex workspace", runtimeID: RuntimeIDCodex, arguments: []string{"--cd=/tmp/project"}, allowed: false},
		{name: "codex permission", runtimeID: RuntimeIDCodex, arguments: []string{"--sandbox", "read-only"}, allowed: false},
		{name: "claude safe setting", runtimeID: RuntimeIDClaude, arguments: []string{"--setting-sources", "user"}, allowed: true},
		{name: "claude protocol", runtimeID: RuntimeIDClaude, arguments: []string{"--output-format=stream-json"}, allowed: false},
		{name: "claude permission", runtimeID: RuntimeIDClaude, arguments: []string{"--permission-mode", "plan"}, allowed: false},
		{name: "unknown runtime", runtimeID: "unknown", arguments: nil, allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := LaunchOverrideArgumentsAllowed(test.runtimeID, test.arguments); actual != test.allowed {
				t.Fatalf("LaunchOverrideArgumentsAllowed() = %v, want %v", actual, test.allowed)
			}
		})
	}
}

func TestLaunchDetectInputsEqualIgnoresNonLaunchFields(t *testing.T) {
	previous := LaunchDetectInput{
		Binary:     "codex",
		PrefixArgs: []string{"--profile", "ally"},
		ConfigHome: "/tmp/codex-home",
	}
	next := previous
	if !LaunchDetectInputsEqual(previous, next) {
		t.Fatal("identical launch inputs should compare equal")
	}
	next.PrefixArgs = []string{"--profile", "other"}
	if LaunchDetectInputsEqual(previous, next) {
		t.Fatal("prefix args change should require detect refresh")
	}
}

// TestProfileInstallationUsesItsEnvironment verifies profile executable location, launch arguments, and configuration home reach installation version probes.
func TestProfileInstallationUsesItsEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}
	home := t.TempDir()
	binary := filepath.Join(home, "qoder-profile")
	probe := "#!/bin/sh\n" +
		"[ \"$1\" = profile-prefix ] && [ \"$2\" = --version ] && [ \"$QODER_CONFIG_DIR\" = \"$PROFILE_HOME\" ] || exit 2\n" +
		"printf '2.1.0\\n'\n"
	if err := os.WriteFile(binary, []byte(probe), 0o700); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewCatalog(CatalogConfig{Profiles: []ProfileSpec{{
		ID: "qoder-profile", BaseRuntimeID: RuntimeIDQoder, BinaryCandidates: []string{binary},
		PrefixArgs: []string{"profile-prefix"}, Environment: map[string]string{"PROFILE_HOME": home, "QODER_CONFIG_DIR": home},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	request := DetectRequest{
		RuntimeID: "qoder-profile", HomeDirectory: home, BaseEnvironment: []string{"PATH="},
	}
	for _, summary := range catalog.Runtimes() {
		if summary.ID == request.RuntimeID && summary.ConfigHome != home {
			t.Fatalf("static native home was lost from the internal launch snapshot: %+v", summary)
		}
	}
	resolved, err := catalog.ResolveExecutable(request)
	if err != nil || resolved != binary {
		t.Fatalf("resolved profile executable = %q, %v", resolved, err)
	}
	installations := catalog.DiscoverInstallations(t.Context(), request, nil)
	if !slices.Contains(installations, CLIInstallation{Path: binary, Version: "2.1.0", Available: true}) {
		t.Fatalf("profile installation was not probed with its launch configuration: %+v", installations)
	}
}
