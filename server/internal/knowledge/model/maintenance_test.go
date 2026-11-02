package model

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/native"
)

// maintenanceRuntimeFunc 替换共享宿主边界，保留真实授权表与资源准备。
type maintenanceRuntimeFunc func(context.Context, agentapi.TaskRequest) (agentapi.TextResult, error)

func (run maintenanceRuntimeFunc) ExecuteTask(ctx context.Context, input agentapi.TaskRequest) (agentapi.TextResult, error) {
	return run(ctx, input)
}

func maintenanceFixture(t *testing.T, runtime maintenanceRuntimeFunc, released *bool) (*MaintenanceRunner, *capability.Registry) {
	t.Helper()
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	binary := filepath.Join(t.TempDir(), "allycli")
	if err := os.WriteFile(binary, []byte("test binary"), 0700); err != nil {
		t.Fatal(err)
	}
	return NewMaintenanceRunner(MaintenanceConfig{Runtime: maintenanceRuntimeFunc(func(ctx context.Context, input agentapi.TaskRequest) (agentapi.TextResult, error) {
		workspace, err := os.MkdirTemp("", "neux-maintenance-fixture-")
		if err != nil {
			t.Fatal(err)
		}
		workspace, err = filepath.EvalSymlinks(workspace)
		if err != nil {
			t.Fatal(err)
		}
		lease, err := input.Resources.Acquire(ctx, agentapi.ResourceRequest{Workspace: native.WorkspaceScope{Root: workspace}, Contributions: input.Contributions})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			input.Resources.Revoke()
			_ = lease.Close(context.Background())
			_ = os.RemoveAll(workspace)
			*released = true
		}()
		return runtime(ctx, input)
	}), Registry: registry, Binary: binary, Endpoint: "http://127.0.0.1:8432" + constants.AgentCLIPath, RuntimeID: "codex", AccountID: "account", Model: "model-a", Reasoning: "high"}), registry
}

func maintenanceCapabilities(registry *capability.Registry, connection capability.Connection) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, constants.AgentCLIPath+"/capabilities", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	response := httptest.NewRecorder()
	registry.ServeHTTP(response, request)
	return response
}

func maintenanceAudit(registry *capability.Registry, connection capability.Connection) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, constants.AgentCLIPath+"/commands", strings.NewReader(`{"operation":"knowledge.audit.get","params":{"libraryId":"library"}}`))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	response := httptest.NewRecorder()
	registry.ServeHTTP(response, request)
	return response
}

func maintenanceInput() MaintenanceRequest {
	return MaintenanceRequest{JobID: "wiki-job", Instructions: "upstream fixer workflow", Message: "maintain existing wiki", Validate: func(context.Context) error { return nil }, Bindings: []capability.Binding{{Operation: "knowledge.audit.get", Call: func(context.Context, capability.Request) (capability.Result, error) {
		return capability.Result{}, nil
	}}}}
}

func TestMaintenanceUsesSharedHostWithScopedCLIAndCleansUp(t *testing.T) {
	var released bool
	var connection capability.Connection
	var workspace, connectionPath string
	var registry *capability.Registry
	runner, actualRegistry := maintenanceFixture(t, func(ctx context.Context, request agentapi.TaskRequest) (agentapi.TextResult, error) {
		workspace = request.Resources.(*maintenanceResources).workspace
		for _, skill := range request.Resources.(*maintenanceResources).Resources() {
			relative, err := filepath.Rel(workspace, skill.Path)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				t.Fatalf("maintenance skill is outside the native trusted workspace and requires unavailable Read approval: %s", skill.Path)
			}
			if _, err := os.ReadFile(filepath.Join(skill.Path, "SKILL.md")); err != nil {
				t.Fatalf("maintenance skill is not readable: %v", err)
			}
		}
		connectionPath = request.Resources.LaunchEnvironment(nil)[constants.AgentCLIContextEnvironment]
		var err error
		connection, err = capability.ReadConnection(connectionPath)
		if err != nil {
			t.Fatal(err)
		}
		capabilities := maintenanceCapabilities(registry, connection)
		if capabilities.Code != http.StatusOK || !strings.Contains(capabilities.Body.String(), "knowledge.audit.get") || strings.Contains(capabilities.Body.String(), "im.targets.list") {
			t.Fatalf("maintenance grant leaked or was inactive: %d %s", capabilities.Code, capabilities.Body)
		}
		if audit := maintenanceAudit(registry, connection); audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), `"ok":true`) {
			t.Fatalf("maintenance domain call failed: %d %s", audit.Code, audit.Body)
		}
		if len(request.Contributions.MCP) != 0 || len(request.Contributions.Skills) != 1 || request.Contributions.Skills[0].Name != "ally" || request.Contributions.Skills[0].Optional || request.RuntimeID != "codex" || request.AccountID != "account" {
			t.Fatalf("maintenance was not an independent ACP request: %+v", request)
		}
		if request.Model != "model-a" || request.Reasoning != "high" || !request.Thinking {
			t.Fatalf("maintenance did not receive the authorized full-access policy: %+v", request.TextRequest)
		}
		if request.Messages[0].Content != "maintain existing wiki" || len(request.Contributions.Instructions) != 2 || request.Contributions.Instructions[0].Text != "upstream fixer workflow" || request.Resources.LaunchEnvironment(nil)[constants.AgentCLIBinaryEnvironment] == "" {
			t.Fatalf("fixer workflow or explicit CLI path missing: %+v", request)
		}
		cliInstructions := request.Contributions.Instructions[1].Text
		if strings.Contains(cliInstructions, "read-only") || strings.Contains(cliInstructions, "without a shell wrapper") || !strings.Contains(cliInstructions, "temporary workspace") {
			t.Fatalf("maintenance instructions conflict with the full-access policy: %s", cliInstructions)
		}
		return agentapi.TextResult{Text: "maintenance complete", StopReason: "end_turn"}, nil
	}, &released)
	registry = actualRegistry
	result, err := runner.Run(context.Background(), maintenanceInput())
	if err != nil || result != "maintenance complete" || !released {
		t.Fatalf("result=%q err=%v released=%v", result, err, released)
	}
	if response := maintenanceCapabilities(registry, connection); response.Code != http.StatusUnauthorized {
		t.Fatalf("finished maintenance retained grant: %d", response.Code)
	}
	for _, path := range []string{workspace, connectionPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("finished maintenance retained temporary files: %s %v", path, err)
		}
	}
}

func TestMaintenanceCancellationRevokesGrantAndDiscardsPartialReport(t *testing.T) {
	var released bool
	var connection capability.Connection
	var workspace string
	entered := make(chan struct{})
	runner, registry := maintenanceFixture(t, func(ctx context.Context, request agentapi.TaskRequest) (agentapi.TextResult, error) {
		workspace = request.Resources.(*maintenanceResources).workspace
		var err error
		connection, err = capability.ReadConnection(request.Resources.LaunchEnvironment(nil)[constants.AgentCLIContextEnvironment])
		if err != nil {
			t.Fatal(err)
		}

		close(entered)
		<-ctx.Done()
		return agentapi.TextResult{Text: "partial"}, status_error.OperationCanceled
	}, &released)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		result, err := runner.Run(ctx, maintenanceInput())
		if result != "" {
			done <- status_error.RuntimeProtocolIncompatible
			return
		}
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("maintenance did not enter native execution: %v", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, status_error.OperationCanceled) || !released {
		t.Fatalf("cancellation did not release account: %v %v", err, released)
	}
	if response := maintenanceCapabilities(registry, connection); response.Code != http.StatusUnauthorized {
		t.Fatalf("cancelled maintenance retained grant: %d", response.Code)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("cancelled workspace retained: %v", err)
	}
}

func TestMaintenanceRejectsInvalidTaskBeforePreparingAccount(t *testing.T) {
	var prepared bool
	runner := NewMaintenanceRunner(MaintenanceConfig{Runtime: maintenanceRuntimeFunc(func(context.Context, agentapi.TaskRequest) (agentapi.TextResult, error) {
		prepared = true
		return agentapi.TextResult{}, nil
	})})
	if _, err := runner.Run(context.Background(), MaintenanceRequest{}); !errors.Is(err, status_error.KnowledgeInvalid) || prepared {
		t.Fatalf("invalid task entered runtime preparation: %v %v", err, prepared)
	}
}

func TestMaintenanceDoesNotPublishTruncatedReport(t *testing.T) {
	var released bool
	runner, _ := maintenanceFixture(t, func(ctx context.Context, _ agentapi.TaskRequest) (agentapi.TextResult, error) {
		return agentapi.TextResult{Text: "partial report", StopReason: "max_tokens"}, nil
	}, &released)
	report, err := runner.Run(context.Background(), maintenanceInput())
	var coded interface{ Code() int64 }
	var policy interface{ Retryable() bool }
	if report != "" || !errors.As(err, &coded) || coded.Code() != status_error.RuntimeExecutionFailed.Code() || !released || !errors.As(err, &policy) || policy.Retryable() {
		t.Fatalf("truncated maintenance was published or retriable: report=%q err=%v released=%v", report, err, released)
	}
}

func TestMaintenanceDoesNotAcceptNarrativeWithoutSuccessfulKnowledgeCall(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "discovery-only", true: "denied-domain-call"}[denied], func(t *testing.T) {
			var released bool
			var registry *capability.Registry
			runner, actualRegistry := maintenanceFixture(t, func(ctx context.Context, request agentapi.TaskRequest) (agentapi.TextResult, error) {
				connection, err := capability.ReadConnection(request.Resources.LaunchEnvironment(nil)[constants.AgentCLIContextEnvironment])
				if err != nil {
					t.Fatal(err)
				}
				if response := maintenanceCapabilities(registry, connection); response.Code != http.StatusOK {
					t.Fatalf("discovery failed: %d", response.Code)
				}
				if denied {
					if response := maintenanceAudit(registry, connection); strings.Contains(response.Body.String(), `"ok":true`) {
						t.Fatalf("domain rejection was lost: %s", response.Body)
					}
				}
				return agentapi.TextResult{Text: "maintenance complete", StopReason: "end_turn"}, nil
			}, &released)
			registry = actualRegistry
			input := maintenanceInput()
			deniedCalled := false
			if denied {
				input.Bindings[0].Call = func(context.Context, capability.Request) (capability.Result, error) {
					deniedCalled = true
					return capability.Result{}, status_error.KnowledgeForbidden
				}
			}
			report, err := runner.Run(context.Background(), input)
			if denied && !deniedCalled {
				t.Fatal("denied domain operation was not reached")
			}
			var coded interface{ Code() int64 }
			if report != "" || !errors.As(err, &coded) || coded.Code() != status_error.RuntimeExecutionFailed.Code() || !released {
				t.Fatalf("narrative was accepted as maintenance: report=%q err=%v released=%v", report, err, released)
			}
		})
	}
}

func TestMaintenanceRejectsEmptyFinalEvenAfterAuthorizedCall(t *testing.T) {
	var released bool
	var registry *capability.Registry
	runner, actual := maintenanceFixture(t, func(_ context.Context, request agentapi.TaskRequest) (agentapi.TextResult, error) {
		environment := request.Resources.LaunchEnvironment(map[string]string{"PATH": "account-path", "ACCOUNT_TOKEN": "secret"})
		if environment["ACCOUNT_TOKEN"] != "secret" || !strings.HasSuffix(environment["PATH"], "account-path") {
			t.Fatalf("launch environment lost account facts: %v", environment)
		}
		connection, err := capability.ReadConnection(environment[constants.AgentCLIContextEnvironment])
		if err != nil {
			t.Fatal(err)
		}
		if response := maintenanceAudit(registry, connection); response.Code != http.StatusOK {
			t.Fatalf("domain call failed: %s", response.Body)
		}
		return agentapi.TextResult{Text: " \n", StopReason: "end_turn"}, nil
	}, &released)
	registry = actual
	if report, err := runner.Run(context.Background(), maintenanceInput()); report != "" || err == nil || !released {
		t.Fatalf("empty final accepted: %q %v", report, err)
	}
}

func TestMaintenanceResourceCancellationPreservesCleanupOwnership(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resources := &maintenanceResources{}
	lease, err := resources.Acquire(ctx, agentapi.ResourceRequest{Workspace: native.WorkspaceScope{Root: root}})
	if !errors.Is(err, status_error.OperationCanceled) || lease != resources || resources.directory != "" {
		t.Fatalf("cancelled resource preparation created files or lost partial lease: %v %q", err, resources.directory)
	}
	resources.directory = filepath.Join(root, "lease-owned")
	if err := os.Mkdir(resources.directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := resources.Close(ctx); !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("cleanup ignored cancelled context: %v", err)
	}
	if _, err := os.Stat(resources.directory); err != nil {
		t.Fatalf("cancelled cleanup removed owned files: %v", err)
	}
	if err := resources.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(resources.directory); !os.IsNotExist(err) {
		t.Fatalf("retry did not finish cleanup: %v", err)
	}
}
