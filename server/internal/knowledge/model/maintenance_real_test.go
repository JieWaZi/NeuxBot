package model

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
)

// TestRealWikiMaintenanceUsesScopedMCP 核验真实原生会话通过专属 MCP 执行只读知识命令，不访问生产知识内容。
func TestRealWikiMaintenanceUsesScopedMCP(t *testing.T) {
	if os.Getenv("ALLY_REAL_WIKI_MAINTENANCE") != "1" {
		t.Skip("requires explicit real-model Wiki maintenance transport check")
	}
	binary := os.Getenv(constants.AgentCLIBinaryEnvironment)
	if binary == "" {
		t.Fatal("the packaged allycli path is required")
	}
	runtimeID := os.Getenv("ALLY_WIKI_RUNTIME")
	if runtimeID == "" {
		runtimeID = constants.RuntimeIDClaude
	}
	model := os.Getenv("ALLY_WIKI_MODEL")
	if !agent.SupportsDetachedPolicy(runtimeID) {
		t.Fatalf("provider cannot enforce scoped maintenance: %s", runtimeID)
	}
	reasoning := os.Getenv("ALLY_WIKI_REASONING")
	sdk, err := agent.NewCatalog(agent.CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	executable, err := sdk.ResolveExecutable(agent.DetectRequest{RuntimeID: runtimeID, BaseEnvironment: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	registry := capability.New(nil)
	defer registry.Close()
	endpoint := httptest.NewServer(registry)
	defer endpoint.Close()
	var calls atomic.Int32
	runner := NewMaintenanceRunner(realMaintenanceConfig(t, registry, endpoint.URL, binary, executable, model, reasoning))
	input := MaintenanceRequest{
		JobID:        "readonly-maintenance-transport-check",
		Instructions: "This is a read-only transport verification, with one synthetic library named transport-check. Only knowledge.audit.get is authorized. Do not read any other local files, access other services, modify files, or attempt any writes.",
		Message:      "Use the provided MCP tool knowledge_audit_get for libraryId transport-check. Report the marker returned by that call.",
		Validate:     func(context.Context) error { return nil },
		Bindings: []capability.Binding{{Operation: "knowledge.audit.get", Call: func(_ context.Context, request capability.Request) (capability.Result, error) {
			if request.Params["libraryId"] != "transport-check" {
				t.Errorf("unexpected synthetic scope: %+v", request.Params)
			}
			calls.Add(1)
			return capability.Result{Data: map[string]any{"marker": "WIKI_TRANSPORT_OK", "issues": []any{}}}, nil
		}}},
	}
	report, err := runner.Run(ctx, input)
	if err != nil || calls.Load() < 1 || !strings.Contains(report, "WIKI_TRANSPORT_OK") {
		t.Fatalf("synthetic maintenance transport failed: calls=%d err=%v report=%q", calls.Load(), err, report)
	}
	t.Logf("real %s scoped MCP transport passed, calls=%d", runtimeID, calls.Load())
}

// realMaintenanceConfig 仅在显式启用真实验收时装配正式原生运行时。
func realMaintenanceConfig(t *testing.T, registry *capability.Registry, endpoint, binary, executable, model, reasoning string) MaintenanceConfig {
	t.Helper()
	store, err := conversation.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	accountService := aiaccount.New(nil)
	providers := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{AcquireAccount: accountService.AcquireAccount})
	t.Cleanup(func() { _ = providers.Close(context.Background()) })
	threads := agent.NewThreadLifecycle(providers, store)
	t.Cleanup(func() { _ = threads.Close(context.Background()) })
	return MaintenanceConfig{Runtime: threads, Registry: registry, Binary: binary, Endpoint: endpoint + constants.AgentCLIPath,
		Connection: agent.ProviderConnectionRequest{
			Account: agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: constants.AiAccountSystemID, Executable: executable, Model: model},
			Launch:  agent.ProviderLaunch{Executable: executable},
		},
		ResolveAccount: accountService.ResolveAccount, Reasoning: reasoning,
	}
}
