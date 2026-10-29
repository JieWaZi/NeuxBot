package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go/sdk"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func TestResolveExecutableRejectsUnregisteredRuntimeWithStableError(t *testing.T) {
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sdk.ResolveExecutable(runtimecontract.DetectRequest{RuntimeID: "unregistered-runtime"})
	var stable statuserror.CommonError
	if !errors.As(err, &stable) || stable.Code() != status_error.RuntimeNotRegistered.Code() {
		t.Fatalf("unregistered runtime error = %v, want RuntimeNotRegistered", err)
	}
}

func TestDetectUsesACPWithoutExecutingRuntimeProbeCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}
	marker := filepath.Join(t.TempDir(), "executed")
	executable := filepath.Join(t.TempDir(), "runtime-fixture")
	script := "#!/bin/sh\n: > \"" + marker + "\"\nexit 99\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatalf("write Runtime fixture: %v", err)
	}
	sdk, err := New(Config{Profiles: []runtimecontract.ProfileSpec{{
		ID:               "codex-detection-fixture",
		BaseRuntimeID:    runtimecontract.RuntimeIDCodex,
		BinaryCandidates: []string{executable},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	installFakeCodexACPAgent(t, sdk, testCodexACPAgentWithCatalog())
	resolved, err := sdk.ResolveExecutable(runtimecontract.DetectRequest{
		RuntimeID:     "codex-detection-fixture",
		HomeDirectory: t.TempDir(),
	})
	if err != nil || resolved != executable {
		t.Fatalf("ResolveExecutable() = %q, %v", resolved, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("ResolveExecutable invoked the CLI: %v", err)
	}

	result := sdk.Detect(context.Background(), runtimecontract.DetectRequest{
		RuntimeID:     "codex-detection-fixture",
		HomeDirectory: t.TempDir(),
	})
	if result.State != runtimecontract.DetectionStateReady || result.Auth != runtimecontract.AuthStatusUnknown {
		t.Fatalf("Detection = %+v", result)
	}
	if result.Version != "0.149.1" ||
		!result.Capabilities.Has(runtimecontract.CapabilityResumeSession) ||
		!result.Capabilities.Has(runtimecontract.CapabilityMCPStreamableHTTP) ||
		!result.Capabilities.Has(runtimecontract.CapabilityDynamicModels) {
		t.Fatalf("ACP evidence = version %q, capabilities %+v", result.Version, result.Capabilities)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Runtime probe executable was invoked: %v", err)
	}
}

func TestDetectRefreshesVersionAfterCLIUpdate(t *testing.T) {
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := New(Config{Profiles: []runtimecontract.ProfileSpec{{
		ID:               "codex-refresh-fixture",
		BaseRuntimeID:    runtimecontract.RuntimeIDCodex,
		BinaryCandidates: []string{executable},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	backend := sdk.acp.(*acpBackend)
	version := "a"
	created := 0
	replaceACPAgentFactory(backend, runtimecontract.RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
		created++
		agent := testCodexACPAgentWithCatalog()
		agent.runtimeVersion = version
		return agent, nil
	})
	t.Cleanup(func() { _ = sdk.Close(context.Background()) })
	request := runtimecontract.DetectRequest{RuntimeID: "codex-refresh-fixture", HomeDirectory: t.TempDir()}
	first := sdk.Detect(context.Background(), request)
	if first.Version != "a" || first.State != runtimecontract.DetectionStateReady {
		t.Fatalf("first detection = %+v", first)
	}
	version = "b"
	second := sdk.Detect(context.Background(), request)
	if second.Version != "b" || second.State != runtimecontract.DetectionStateReady || created != 2 {
		t.Fatalf("refresh detection = %+v, agents created = %d; want version b from a fresh agent", second, created)
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
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	request := runtimecontract.DetectRequest{
		RuntimeID:       runtimecontract.RuntimeIDOpenCode,
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

func TestACPInitializeCatalogDoesNotUseAgentImplementationVersion(t *testing.T) {
	catalog := acpInitializeCatalog(acpRuntimeBinding{}, acp.InitializeResponse{
		AgentInfo: &acp.Implementation{Version: "development"},
	})

	if catalog.Version != "" {
		t.Fatalf("缺少 Runtime CLI 元数据时版本为 %q，期望为空", catalog.Version)
	}
}

func TestDetectKeepsInstalledRuntimesReadyWhenAuthenticationIsMissing(t *testing.T) {
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, runtimeID := range []string{runtimecontract.RuntimeIDCodex, runtimecontract.RuntimeIDClaude, runtimecontract.RuntimeIDKimi, runtimecontract.RuntimeIDCursor, runtimecontract.RuntimeIDPi} {
		t.Run(runtimeID, func(t *testing.T) {
			profileID := runtimeID + "-auth-fixture"
			sdk, err := New(Config{Profiles: []runtimecontract.ProfileSpec{{
				ID:               profileID,
				BaseRuntimeID:    runtimeID,
				BinaryCandidates: []string{executable},
			}}})
			if err != nil {
				t.Fatal(err)
			}
			agent := testCodexACPAgentWithCatalog()
			agent.newSessionError = acp.NewAuthRequired(nil)
			backend := sdk.acp.(*acpBackend)
			replaceACPAgentFactory(backend, runtimeID, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
				return agent, nil
			})
			t.Cleanup(func() { _ = sdk.Close(context.Background()) })

			result := sdk.Detect(context.Background(), runtimecontract.DetectRequest{
				RuntimeID:     profileID,
				HomeDirectory: t.TempDir(),
			})
			if result.State != runtimecontract.DetectionStateReady || result.Auth != runtimecontract.AuthStatusMissing || result.ResolvedBinary != executable || result.Version != "0.149.1" {
				t.Fatalf("Detection = %+v", result)
			}
		})
	}
}
