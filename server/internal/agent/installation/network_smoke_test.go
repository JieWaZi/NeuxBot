package installation

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	acp "github.com/JieWaZi/acp-go-sdk"
	"github.com/JieWaZi/acp-go/pkg/agentclient"
	"github.com/JieWaZi/acp-go/pkg/standardacp"
)

type smokeTransport struct {
	// test owns the opt-in run's safe host/status observations.
	test *testing.T
	// base retains the production system proxy and certificate configuration.
	base http.RoundTripper
}

func (s smokeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := s.base.RoundTrip(request)
	if err != nil {
		kind := "transport"
		var certificate *tls.CertificateVerificationError
		if errors.As(err, &certificate) {
			kind = "TLS certificate verification"
		} else if errors.Is(err, context.DeadlineExceeded) {
			kind = "deadline"
		}
		s.test.Logf("HTTP host=%s failure=%s type=%T", request.URL.Hostname(), kind, err)
	} else {
		s.test.Logf("HTTP host=%s status=%d bytes=%d", request.URL.Hostname(), response.StatusCode, response.ContentLength)
	}
	return response, err
}

type smokeRunner struct {
	// test owns fixed-command observations without exposing process environments.
	test *testing.T
}

func (s smokeRunner) Run(ctx context.Context, command Command) (string, error) {
	if os.Getenv("NEUXBOT_INSTALLATION_SMOKE_DIAGNOSTIC") == "1" && runtime.GOOS == "darwin" && filepath.Base(command.Binary) == "kimi" {
		s.test.Logf("DIAGNOSTIC private-payload=%s", filepath.Dir(command.Binary))
		for _, invocation := range [][]string{{"/usr/bin/file", command.Binary}, {"/usr/bin/otool", "-L", command.Binary}, {"/usr/bin/codesign", "--verify", "--deep", "--strict", "--verbose=2", command.Binary}} {
			probe := exec.CommandContext(ctx, invocation[0], invocation[1:]...)
			probe.Dir, probe.Env = command.Dir, command.Env
			output, err := probe.CombinedOutput()
			s.test.Logf("DIAGNOSTIC tool=%s output=%q error=%v", filepath.Base(invocation[0]), output, err)
		}
	}
	started := time.Now()
	output, err := (commandRunner{}).Run(ctx, command)
	if slices.Contains(command.Args, "--version") {
		s.test.Logf("PROBE executable=%s elapsed=%s output=%q error=%v", filepath.Base(command.Binary), time.Since(started).Round(time.Millisecond), strings.TrimSpace(output), err)
	} else {
		s.test.Logf("DEPENDENCIES executable=%s error=%v", filepath.Base(command.Binary), err)
		if err != nil {
			s.test.Logf("DEPENDENCIES diagnostic-bytes=%d", len(output))
		}
	}
	return output, err
}

// TestOfficialInstallationSmoke is deliberately opt-in: it downloads and executes
// official --version commands only, with private temporary homes and no login.
func TestOfficialInstallationSmoke(t *testing.T) {
	if os.Getenv("NEUXBOT_INSTALLATION_SMOKE") != "1" {
		t.Skip("set NEUXBOT_INSTALLATION_SMOKE=1 to exercise official downloads and private version probes")
	}
	t.Logf("platform=%s/%s", runtime.GOOS, runtime.GOARCH)
	selected := strings.Split(os.Getenv("NEUXBOT_INSTALLATION_SMOKE_RUNTIME"), ",")
	for _, id := range []string{constants.RuntimeIDCodex, constants.RuntimeIDClaude, constants.RuntimeIDCursor, constants.RuntimeIDKimi,
		constants.RuntimeIDOpenCode, constants.RuntimeIDPi, constants.RuntimeIDGrok, constants.RuntimeIDGemini,
		constants.RuntimeIDQoder, constants.RuntimeIDWorkBuddy} {
		if selected[0] != "" && !slices.Contains(selected, id) {
			continue
		}
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			installer := NewWithOptions(root, Options{
				Client: &http.Client{Timeout: 10 * time.Minute, Transport: smokeTransport{test: t, base: http.DefaultTransport}},
				Runner: smokeRunner{test: t},
			})
			if !installer.Supported(id) {
				t.Skip("official managed source is unsupported on this platform")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
			defer cancel()
			lastPhase := ""
			result, err := installer.Install(ctx, id, func(progress Progress) {
				if lastPhase != progress.Phase {
					lastPhase = progress.Phase
					t.Logf("PHASE %s", progress.Phase)
				}
			})
			if err != nil {
				if _, published := installer.Installed(id); !errors.Is(published, os.ErrNotExist) {
					t.Errorf("failed install left a published executable: %v", published)
				}
				if entries, readErr := os.ReadDir(filepath.Join(root, id)); readErr == nil {
					for _, entry := range entries {
						if strings.HasPrefix(entry.Name(), ".staging-") {
							t.Error("failed install did not reclaim staging")
						}
					}
				}
				operation, reason, status := Diagnosis(err)
				t.Fatalf("official install failed at phase=%s operation=%s reason=%s http_status=%d: %v", lastPhase, operation, reason, status, err)
			}
			if result.Version == "" || !strings.HasPrefix(result.Binary, root+string(filepath.Separator)) && len(result.PrefixArgs) == 0 {
				t.Fatalf("installation escaped managed root or omitted version: binary=%s version=%s", result.Binary, result.Version)
			}
			installed, err := installer.Installed(id)
			if err != nil || installed.Binary != result.Binary || installed.Version != result.Version {
				t.Fatalf("published install could not be read: %v", err)
			}
			if id == constants.RuntimeIDKimi {
				workspace := t.TempDir()
				for _, name := range []string{"home", "cache", "tmp"} {
					if err := os.Mkdir(filepath.Join(workspace, name), 0o700); err != nil {
						t.Fatal(err)
					}
				}
				native, err := standardacp.NewClient(ctx, standardacp.Options{Provider: standardacp.Kimi, Executable: result.Binary,
					PrefixArgs: result.PrefixArgs, WorkingDirectory: workspace, Environment: privateEnvironment(workspace)}, agentclient.HostCallbacks{})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					closeCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
					defer done()
					if err := native.Close(closeCtx); err != nil {
						t.Errorf("Kimi native owner cleanup failed: %v", err)
					}
				})
				initialization, err := native.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: 1})
				if err != nil {
					t.Fatalf("installed Kimi did not initialize through the production native adapter: %v", err)
				}
				t.Logf("ACP identity=%s version=%s auth-methods=%d", initialization.AgentInfo.Name, initialization.AgentInfo.Version, len(initialization.AuthMethods))
				if os.Getenv("NEUXBOT_INSTALLATION_SMOKE_HELP") == "1" {
					for _, args := range [][]string{{"--help"}, {"login", "--help"}} {
						readCtx, done := context.WithTimeout(ctx, 30*time.Second)
						output, err := (commandRunner{}).Run(readCtx, Command{Binary: result.Binary, Args: args, Dir: workspace, Env: privateEnvironment(workspace)})
						done()
						t.Logf("READONLY HELP args=%v output=%q error=%v", args, output, err)
						if err != nil {
							t.Fatal("Kimi read-only help failed")
						}
					}
				}
			}
			t.Logf("VERIFIED version=%s source=%s", result.Version, result.SourceURL)
		})
	}
}

// TestLegacyKimiExtractionComparison preserves the deprecated Python artifact's
// tar-layout diagnosis; it is never a managed installation source.
func TestLegacyKimiExtractionComparison(t *testing.T) {
	if os.Getenv("NEUXBOT_INSTALLATION_SMOKE_DIAGNOSTIC") != "1" || runtime.GOOS != "darwin" {
		t.Skip("opt-in macOS diagnosis only; uses official files and private temporary homes")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	root := t.TempDir()
	installer := NewWithOptions(root, Options{Client: &http.Client{Timeout: 5 * time.Minute, Transport: smokeTransport{test: t, base: http.DefaultTransport}}})
	triple := map[string]string{"amd64": "x86_64-apple-darwin", "arm64": "aarch64-apple-darwin"}[runtime.GOARCH]
	if triple == "" {
		t.Skip("legacy diagnosis has no official artifact for this architecture")
	}
	artifact, err := installer.github(ctx, "MoonshotAI/kimi-cli", "", func(version string) string {
		return "kimi-" + version + "-" + triple + "-onedir.tar.gz"
	}, "kimi/kimi")
	if err != nil {
		t.Fatal(err)
	}
	downloaded := filepath.Join(root, "artifact.tgz")
	if err := installer.download(ctx, &artifact, downloaded, func(Progress) {}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"go-extractor", "system-tar"} {
		base := filepath.Join(root, mode)
		for _, name := range []string{"", "home", "cache", "tmp"} {
			if err := os.MkdirAll(filepath.Join(base, name), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "go-extractor" {
			if err := extract(ctx, downloaded, base, artifact.format, artifact.binary); err != nil {
				t.Fatal(err)
			}
		} else {
			command := exec.CommandContext(ctx, "/usr/bin/tar", "-xzf", downloaded, "-C", base, "--no-same-owner")
			command.Dir, command.Env = base, privateEnvironment(base)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("canonical tar failed: %v output=%s", err, output)
			}
		}
	}
	t.Logf("comparison version=%s digest=%s source=%s", artifact.version, artifact.digest, artifact.url)
	for _, mode := range []string{"go-extractor", "system-tar"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			base := filepath.Join(root, mode)
			probe, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			started := time.Now()
			output, err := (commandRunner{}).Run(probe, Command{Binary: filepath.Join(base, filepath.FromSlash(artifact.binary)), Args: []string{"--version"}, Dir: base, Env: privateEnvironment(base)})
			t.Logf("comparison elapsed=%s output=%q error=%v", time.Since(started).Round(time.Millisecond), output, err)
			if err != nil {
				t.Fatalf("native startup failed; context=%v", probe.Err())
			}
		})
	}
}
