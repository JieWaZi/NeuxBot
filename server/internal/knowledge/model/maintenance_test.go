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
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
)

// maintenanceRuntimeFunc 替换外部 ACP 进程，保留真实授权表与受管文件准备。
type maintenanceRuntimeFunc func(context.Context, runtimes.RunRequest, runtimes.EventSink) runtimes.Result

func (run maintenanceRuntimeFunc) Run(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink) runtimes.Result {
	return run(ctx, request, sink)
}

func maintenanceFixture(t *testing.T, runtime maintenanceRuntimeFunc, released *bool) (*MaintenanceRunner, *agentcapability.Registry) {
	t.Helper()
	registry := agentcapability.New(nil)
	t.Cleanup(registry.Close)
	data := t.TempDir()
	binary := filepath.Join(data, "allycli")
	if err := os.WriteFile(binary, []byte("test binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	return NewMaintenanceRunner(MaintenanceConfig{
		Runtime: runtime, Registry: registry, Binary: binary,
		Endpoint: "http://127.0.0.1:8432" + constants.AgentCLIPath,
		Model:    "model-a", Reasoning: "high",
		Prepare: func(context.Context) (runtimes.RuntimeTarget, runtimes.DetectionResult, func(), error) {
			return runtimes.RuntimeTarget{RuntimeID: "codex", AccountID: "account"}, runtimes.DetectionResult{}, func() { *released = true }, nil
		},
	}), registry
}

func maintenanceCapabilities(registry *agentcapability.Registry, connection agentcapability.Connection) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, constants.AgentCLIPath+"/capabilities", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	response := httptest.NewRecorder()
	registry.ServeHTTP(response, request)
	return response
}

func maintenanceAudit(registry *agentcapability.Registry, connection agentcapability.Connection) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, constants.AgentCLIPath+"/commands", strings.NewReader(`{"operation":"knowledge.audit.get","params":{"libraryId":"library"}}`))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	response := httptest.NewRecorder()
	registry.ServeHTTP(response, request)
	return response
}

func maintenanceInput() MaintenanceRequest {
	return MaintenanceRequest{JobID: "wiki-job", Instructions: "upstream fixer workflow", Message: "maintain existing wiki", Validate: func(context.Context) error { return nil }, Bindings: []agentcapability.Binding{{Operation: "knowledge.audit.get", Call: func(context.Context, agentcapability.Request) (agentcapability.Result, error) {
		return agentcapability.Result{}, nil
	}}}}
}

func TestMaintenanceRunsDirectACPWithScopedCLIAndCleansUp(t *testing.T) {
	var released bool
	var connection agentcapability.Connection
	var workspace, connectionPath string
	var registry *agentcapability.Registry
	runner, actualRegistry := maintenanceFixture(t, func(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink) runtimes.Result {
		workspace = request.Workspace.Path
		for _, skill := range request.Skills {
			relative, err := filepath.Rel(workspace, skill.Path)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				t.Fatalf("maintenance skill is outside the native trusted workspace and requires unavailable Read approval: %s", skill.Path)
			}
			if _, err := os.ReadFile(filepath.Join(skill.Path, "SKILL.md")); err != nil {
				t.Fatalf("maintenance skill is not readable: %v", err)
			}
		}
		connectionPath = request.Target.Environment[constants.AgentCLIContextEnvironment]
		var err error
		connection, err = agentcapability.ReadConnection(connectionPath)
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
		if len(request.History) != 0 || len(request.Connectors) != 0 || len(request.Skills) != 1 || request.Skills[0].Name != "ally" || request.Session.Mode != runtimes.SessionModeNew || !request.Workspace.Managed || !request.Target.CloseAfterRun {
			t.Fatalf("maintenance was not an independent ACP request: %+v", request)
		}
		if request.Policy.Model != "model-a" || request.Policy.Reasoning != "high" || request.Policy.Execution.PermissionLevel != runtimes.PermissionLevelFullAccess || request.Policy.Execution.WorkspaceReadOnly {
			t.Fatalf("maintenance did not receive the authorized full-access policy: %+v", request.Policy)
		}
		if request.Message.Text != "maintain existing wiki" || len(request.Instructions) < 2 || request.Instructions[0].Content != "upstream fixer workflow" || request.Target.Environment[constants.AgentCLIBinaryEnvironment] == "" {
			t.Fatalf("fixer workflow or explicit CLI path missing: %+v", request)
		}
		cliInstructions := request.Instructions[1].Content
		if strings.Contains(cliInstructions, "read-only") || strings.Contains(cliInstructions, "without a shell wrapper") || !strings.Contains(cliInstructions, "temporary workspace") {
			t.Fatalf("maintenance instructions conflict with the full-access policy: %s", cliInstructions)
		}
		if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: []byte(`{"text":"maintenance complete"}`)}); err != nil {
			t.Fatal(err)
		}
		return runtimes.Result{StopReason: "end_turn", ProtocolHealthy: true}
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
	var connection agentcapability.Connection
	var workspace string
	entered := make(chan struct{})
	runner, registry := maintenanceFixture(t, func(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink) runtimes.Result {
		workspace = request.Workspace.Path
		var err error
		connection, err = agentcapability.ReadConnection(request.Target.Environment[constants.AgentCLIContextEnvironment])
		if err != nil {
			t.Fatal(err)
		}
		_ = sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: []byte(`{"text":"partial"}`)})
		close(entered)
		<-ctx.Done()
		return runtimes.Result{Failure: &runtimes.Failure{Code: runtimes.FailureCodeContextCanceled}}
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
	<-entered
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
	runner := NewMaintenanceRunner(MaintenanceConfig{Prepare: func(context.Context) (runtimes.RuntimeTarget, runtimes.DetectionResult, func(), error) {
		prepared = true
		return runtimes.RuntimeTarget{}, runtimes.DetectionResult{}, nil, nil
	}})
	if _, err := runner.Run(context.Background(), MaintenanceRequest{}); !errors.Is(err, status_error.KnowledgeInvalid) || prepared {
		t.Fatalf("invalid task entered runtime preparation: %v %v", err, prepared)
	}
}

func TestMaintenanceDoesNotPublishTruncatedReport(t *testing.T) {
	var released bool
	runner, _ := maintenanceFixture(t, func(ctx context.Context, _ runtimes.RunRequest, sink runtimes.EventSink) runtimes.Result {
		if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: []byte(`{"text":"partial report"}`)}); err != nil {
			t.Fatal(err)
		}
		return runtimes.Result{StopReason: "max_tokens", ProtocolHealthy: true}
	}, &released)
	report, err := runner.Run(context.Background(), maintenanceInput())
	var coded interface{ Code() int64 }
	if report != "" || !errors.As(err, &coded) || coded.Code() != status_error.RuntimeExecutionFailed.Code() || !released {
		t.Fatalf("truncated maintenance was published or misclassified: report=%q err=%v released=%v", report, err, released)
	}
}

func TestMaintenanceDoesNotAcceptNarrativeWithoutSuccessfulKnowledgeCall(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "discovery-only", true: "denied-domain-call"}[denied], func(t *testing.T) {
			var released bool
			var registry *agentcapability.Registry
			runner, actualRegistry := maintenanceFixture(t, func(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink) runtimes.Result {
				connection, err := agentcapability.ReadConnection(request.Target.Environment[constants.AgentCLIContextEnvironment])
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
				if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: []byte(`{"text":"maintenance complete"}`)}); err != nil {
					t.Fatal(err)
				}
				return runtimes.Result{StopReason: "end_turn", ProtocolHealthy: true}
			}, &released)
			registry = actualRegistry
			input := maintenanceInput()
			deniedCalled := false
			if denied {
				input.Bindings[0].Call = func(context.Context, agentcapability.Request) (agentcapability.Result, error) {
					deniedCalled = true
					return agentcapability.Result{}, status_error.KnowledgeForbidden
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
