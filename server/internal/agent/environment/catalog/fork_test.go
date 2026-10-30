package catalog

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"

	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	acp "github.com/JieWaZi/acp-go/sdk"
	"os"
	"reflect"
	"testing"
	"time"
)

// forkManagementAgent 记录原生分叉与管理连接的收尾，不提供模型回合。
type forkManagementAgent struct {
	// fakeCodexACPAgent 提供配置目录与资源回收的标准协议替身。
	*fakeCodexACPAgent
	// mode 是本次初始化确认的分叉精度。
	mode string
	// requests 保存原样穿过 ACP 边界的分叉请求。
	requests []acp.UnstableForkSessionRequest
	// forkError 模拟原生分叉已进入协议后返回的错误。
	forkError error
}

// Initialize 仅公开已确认的分叉精度。
func (agent *forkManagementAgent) Initialize(ctx context.Context, request acp.InitializeRequest) (acp.InitializeResponse, error) {
	result, err := agent.fakeCodexACPAgent.Initialize(ctx, request)
	result.Meta = acpmeta.ForkMetadata(agent.mode)
	result.AgentCapabilities.SessionCapabilities.Fork = acpmeta.ForkCapability(agent.mode)
	return result, err
}

// UnstableForkSession 保留来源、目标与对账元数据。
func (agent *forkManagementAgent) UnstableForkSession(_ context.Context, request acp.UnstableForkSessionRequest) (acp.UnstableForkSessionResponse, error) {
	agent.requests = append(agent.requests, request)
	return acp.UnstableForkSessionResponse{SessionId: "fork-child"}, agent.forkError
}

// TestForkSessionOwnsIsolatedManagementConnection 验证分叉不依赖旧执行缓存且连接逐次释放。
func TestForkSessionOwnsIsolatedManagementConnection(t *testing.T) {
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, target, extra := t.TempDir(), t.TempDir(), t.TempDir()
	request := ForkRequest{ReceiptDirectory: t.TempDir(), SourceSessionID: "source", SourceWorkspace: source, Position: "opaque-turn",
		Target:    runtimeconfig.RuntimeTarget{Isolated: true, RuntimeID: runtimeconfig.RuntimeIDCodex, NativePluginDirectories: []string{"native-plugin"}, CodexPluginConfiguration: map[string]string{"plugins.fixture": "enabled"}},
		Detection: readyDetectionForRun(t, sdk, runtimeconfig.RuntimeIDCodex, binary), Workspace: c.WorkspaceScope{Root: target, Directories: []string{extra}}, ManagedWorkspace: true,
		PermissionLevel: runtimeconfig.PermissionLevelAuto, MCPServers: []acp.McpServer{{Http: &acp.McpServerHttpInline{Name: "ally_fixture", Url: "http://127.0.0.1:1/mcp", Headers: []acp.HttpHeader{{Name: "Authorization", Value: "fixture"}, {Name: "Z-Header", Value: "z"}}}}},
	}

	var agents []*forkManagementAgent
	var configs []acpAdapterConfig
	replaceACPAgentFactory(sdk.acp.(*acpBackend), runtimeconfig.RuntimeIDCodex, func(_ context.Context, config acpAdapterConfig) (acp.Agent, error) {
		agent := &forkManagementAgent{fakeCodexACPAgent: testCodexACPAgentWithCatalog(), mode: acpmeta.ForkTurn}
		agents = append(agents, agent)
		configs = append(configs, config)
		return agent, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, reconcile := range []bool{false, true} {
		request.ReconcileOnly = reconcile
		result, err := sdk.ForkSession(ctx, request)
		if err != nil || result.SessionID != "fork-child" || result.Mode != acpmeta.ForkTurn {
			t.Fatalf("fork = %+v, %v", result, err)
		}
		agent := agents[len(agents)-1]
		if !agent.closed || agent.promptCalls != 0 || agent.newCalls != 0 || agent.closeSessionCalls != 1 || len(agent.requests) != 1 {
			t.Fatalf("management lifecycle: %+v", agent.fakeCodexACPAgent)
		}
		got := agent.requests[0]
		if got.SessionId != "source" || got.Cwd != target || !reflect.DeepEqual(got.AdditionalDirectories, []string{extra}) || got.Meta["forkPosition"] != "opaque-turn" || got.Meta["forkReceiptDirectory"] != request.ReceiptDirectory || got.Meta["sourceCwd"] != source || got.Meta["reconcileOnly"] != reconcile {
			t.Fatalf("fork input: %+v", got)
		}
		servers, err := acpmeta.ForkMCPServers(got.McpServers)
		if err != nil || len(servers) != 1 || servers[0].Http == nil || servers[0].Http.Name != "ally_fixture" || len(servers[0].Http.Headers) != 2 || servers[0].Http.Headers[0].Name != "Authorization" {
			t.Fatalf("MCP scope/order: %+v, %v", servers, err)
		}
	}
	if len(agents) != 2 || configs[0].WorkingDirectory != source || !reflect.DeepEqual(configs[0].NativePluginDirectories, []string{"native-plugin"}) || configs[0].CodexPluginConfiguration["plugins.fixture"] != "enabled" {
		t.Fatalf("management launch facts: %+v", configs)
	}
	if err := sdk.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := sdk.ForkSession(ctx, request); !errors.Is(err, status_error.RuntimeUnavailable) || len(agents) != 2 {
		t.Fatalf("closed SDK accepted fork: %v agents=%d", err, len(agents))
	}
}

// TestForkManagementClosesOnUnsupportedOrIndeterminateResult 验证拒绝和不确定结果同样释放独占连接。
func TestForkManagementClosesOnUnsupportedOrIndeterminateResult(t *testing.T) {
	for _, test := range []struct {
		// name 是原生分叉失败边界。
		name string
		// mode 是适配器确认的精度。
		mode string
		// failure 是协议错误或取消原因。
		failure error
		// expected 是公开业务错误。
		expected error
		// calls 是应交付到适配器的分叉次数。
		calls int
	}{
		{name: "unsupported", mode: acpmeta.ForkUnsupported, expected: status_error.RuntimePolicyUnsupported, calls: 0},
		{name: "position requires turn", mode: acpmeta.ForkLatest, expected: status_error.RuntimePolicyUnsupported, calls: 0},
		{name: "cancelled after dispatch", mode: acpmeta.ForkTurn, failure: context.Canceled, expected: status_error.ChatForkIndeterminate, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			sdk, err := New(Config{})
			if err != nil {
				t.Fatal(err)
			}
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			agent := &forkManagementAgent{fakeCodexACPAgent: testCodexACPAgentWithCatalog(), mode: test.mode, forkError: test.failure}
			replaceACPAgentFactory(sdk.acp.(*acpBackend), runtimeconfig.RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err = sdk.ForkSession(ctx, ForkRequest{SourceSessionID: "source", SourceWorkspace: t.TempDir(), Position: "opaque-turn", Target: runtimeconfig.RuntimeTarget{Isolated: true, RuntimeID: runtimeconfig.RuntimeIDCodex}, Detection: readyDetectionForRun(t, sdk, runtimeconfig.RuntimeIDCodex, binary), Workspace: c.WorkspaceScope{Root: t.TempDir()}, PermissionLevel: runtimeconfig.PermissionLevelDefault})
			if !errors.Is(err, test.expected) || !agent.closed || len(agent.requests) != test.calls || agent.promptCalls != 0 {
				t.Fatalf("error=%v closed=%t fork calls=%d prompts=%d", err, agent.closed, len(agent.requests), agent.promptCalls)
			}
		})
	}
}
