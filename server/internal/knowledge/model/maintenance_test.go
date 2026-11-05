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
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// maintenanceRuntimeFunc 替换共享宿主边界，保留真实授权表与资源准备。
type maintenanceRuntimeFunc func(context.Context, agent.TaskRequest) (agent.TurnResult, error)

func (run maintenanceRuntimeFunc) ExecuteTask(ctx context.Context, input agent.TaskRequest) (agent.TurnResult, error) {
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
	return NewMaintenanceRunner(MaintenanceConfig{Runtime: maintenanceRuntimeFunc(func(ctx context.Context, input agent.TaskRequest) (agent.TurnResult, error) {
		lease, err := input.Ports.AcquireResources(ctx, input.Thread.Connection.Resources)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = lease.Close(context.Background())
			_ = lease.CloseFiles(context.Background())
			*released = true
		}()
		connection, err := capability.ReadConnection(lease.MCPServers[0].Arguments[3])
		if err != nil {
			t.Fatal(err)
		}
		if capabilities := maintenanceCapabilities(registry, connection); capabilities.Code != http.StatusOK {
			t.Fatalf("MCP discovery failed before submission: %d", capabilities.Code)
		}
		if audit := maintenanceAudit(registry, connection); strings.Contains(audit.Body.String(), `"ok":true`) {
			t.Fatal("MCP could run a knowledge operation before submission")
		}
		revoke, err := input.Ports.Authorize(ctx, agent.TurnAdmission{}, agent.CommitReceipt{})
		if err != nil {
			t.Fatal(err)
		}
		defer revoke()
		// 后续断言只读取同一次冷启动物化的资源。
		input.Ports.AcquireResources = func(context.Context, agent.ResourceSelection) (agent.ResourceLease, error) { return lease, nil }
		result, err := runtime(ctx, input)
		result.BusinessAdmitted = true
		return result, err
	}), Registry: registry, Binary: binary, Endpoint: "http://127.0.0.1:8432" + constants.AgentCLIPath,
		Connection: agent.ProviderConnectionRequest{Account: agent.AccountRequest{Provider: "claude", AccountID: "account", Model: "model-a"}},
		Reasoning:  "high",
	}), registry
}

func maintenanceResult(text, stop string) agent.TurnResult {
	return agent.TurnResult{Outcome: &agent.AgentNativeOutcome{
		Turn: acpmeta.NativeTurn{Status: acpmeta.NativeTurnCompleted}, FinalMessage: &agent.AgentFinalMessage{Text: text}, StopReason: stop,
	}}
}

func fixtureConnection(t *testing.T, ctx context.Context, request agent.TaskRequest) (capability.Connection, string) {
	t.Helper()
	lease, err := request.Ports.AcquireResources(ctx, request.Thread.Connection.Resources)
	if err != nil || len(lease.MCPServers) != 1 {
		t.Fatalf("MCP resources = %+v, %v", lease, err)
	}
	bridge := lease.MCPServers[0]
	if bridge.Name != "ally_capabilities" || bridge.Transport != "stdio" || len(bridge.Arguments) != 4 ||
		bridge.Arguments[0] != "mcp" || bridge.Arguments[1] != "serve" || bridge.Arguments[2] != "--context" || len(lease.Environment) != 0 {
		t.Fatalf("unexpected MCP transport: %+v", bridge)
	}
	path := bridge.Arguments[3]
	connection, err := capability.ReadConnection(path)
	if err != nil {
		t.Fatal(err)
	}
	return connection, path
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
	runner, actualRegistry := maintenanceFixture(t, func(ctx context.Context, request agent.TaskRequest) (agent.TurnResult, error) {
		connection, connectionPath = fixtureConnection(t, ctx, request)
		workspace = filepath.Dir(connectionPath)
		capabilities := maintenanceCapabilities(registry, connection)
		if capabilities.Code != http.StatusOK || !strings.Contains(capabilities.Body.String(), "knowledge.audit.get") || strings.Contains(capabilities.Body.String(), "im.targets.list") {
			t.Fatalf("maintenance grant leaked or was inactive: %d %s", capabilities.Code, capabilities.Body)
		}
		if audit := maintenanceAudit(registry, connection); audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), `"ok":true`) {
			t.Fatalf("maintenance domain call failed: %d %s", audit.Code, audit.Body)
		}
		account, launch := request.Thread.Connection.Account, request.Thread.Connection.Launch
		if account.Provider != "claude" || account.AccountID != "account" || account.Model != "model-a" ||
			request.Thread.Selection.Reasoning != "high" || !launch.RequireVerifiedMCP || launch.Claude.Tools == nil ||
			len(launch.Claude.Tools) != 0 || request.Thread.Mode != "default" || request.Thread.ChatID != "" {
			t.Fatalf("maintenance was not a scoped detached request: %+v", request.Thread)
		}
		instructions := request.Thread.Session.DeveloperInstructions
		if request.Input.Text != "maintain existing wiki" || instructions == nil ||
			!strings.HasPrefix(*instructions, "upstream fixer workflow") || strings.Contains(*instructions, connection.Token) {
			t.Fatalf("fixer instructions or scope invalid: %+v", request.Input)
		}
		options := []agent.AgentApprovalOption{{ID: "once", Kind: "allow_once"}, {ID: "persistent", Kind: "allow_always"}}
		for name, want := range map[string]string{"mcp__ally_capabilities__knowledge_audit_get": "once", "mcp__ally_capabilities__workflow_source_save": "", "Bash": ""} {
			choice, err := request.Ports.Approve(ctx, agent.AgentScope{}, agent.AgentApproval{NativeToolName: name, Options: options})
			if err != nil || choice != want {
				t.Fatalf("native approval %s = %q, %v", name, choice, err)
			}
		}
		return maintenanceResult("maintenance complete", "end_turn"), nil
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
	runner, registry := maintenanceFixture(t, func(ctx context.Context, request agent.TaskRequest) (agent.TurnResult, error) {
		var connectionPath string
		connection, connectionPath = fixtureConnection(t, ctx, request)
		workspace = filepath.Dir(connectionPath)

		close(entered)
		<-ctx.Done()
		return maintenanceResult("partial", "cancelled"), status_error.OperationCanceled
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
	runner := NewMaintenanceRunner(MaintenanceConfig{Runtime: maintenanceRuntimeFunc(func(context.Context, agent.TaskRequest) (agent.TurnResult, error) {
		prepared = true
		return agent.TurnResult{}, nil
	})})
	if _, err := runner.Run(context.Background(), MaintenanceRequest{}); !errors.Is(err, status_error.KnowledgeInvalid) || prepared {
		t.Fatalf("invalid task entered runtime preparation: %v %v", err, prepared)
	}
}

func TestMaintenanceDoesNotPublishTruncatedReport(t *testing.T) {
	var released bool
	runner, _ := maintenanceFixture(t, func(ctx context.Context, _ agent.TaskRequest) (agent.TurnResult, error) {
		return maintenanceResult("partial report", "max_tokens"), nil
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
			runner, actualRegistry := maintenanceFixture(t, func(ctx context.Context, request agent.TaskRequest) (agent.TurnResult, error) {
				connection, _ := fixtureConnection(t, ctx, request)
				if response := maintenanceCapabilities(registry, connection); response.Code != http.StatusOK {
					t.Fatalf("discovery failed: %d", response.Code)
				}
				if denied {
					if response := maintenanceAudit(registry, connection); strings.Contains(response.Body.String(), `"ok":true`) {
						t.Fatalf("domain rejection was lost: %s", response.Body)
					}
				}
				return maintenanceResult("maintenance complete", "end_turn"), nil
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
	runner, actual := maintenanceFixture(t, func(ctx context.Context, request agent.TaskRequest) (agent.TurnResult, error) {
		connection, _ := fixtureConnection(t, ctx, request)
		if response := maintenanceAudit(registry, connection); response.Code != http.StatusOK {
			t.Fatalf("domain call failed: %s", response.Body)
		}
		return maintenanceResult(" \n", "end_turn"), nil
	}, &released)
	registry = actual
	if report, err := runner.Run(context.Background(), maintenanceInput()); report != "" || err == nil || !released {
		t.Fatalf("empty final accepted: %q %v", report, err)
	}
}

func TestCancelledMaintenanceDoesNotAcquireBridge(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runner := &MaintenanceRunner{}
	lease, _, err := runner.acquireBridge(ctx, maintenanceInput(), nil, "", "")
	if !errors.Is(err, context.Canceled) || lease.Close != nil || lease.CloseFiles != nil {
		t.Fatalf("cancelled preparation acquired resources: %+v %v", lease, err)
	}
}
