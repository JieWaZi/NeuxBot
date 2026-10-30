package catalog

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/environment/launch"
	acp "github.com/JieWaZi/acp-go/sdk"
	"sync"
	"testing"
)

// acpAgentFactory 仅供管理协议替身消费冻结的启动事实。
type acpAgentFactory func(context.Context, acpAdapterConfig) (acp.Agent, error)

// fakeCodexACPAgent 提供无模型调用的管理协议替身，供探测与分叉测试复用。
type fakeCodexACPAgent struct {
	// mutex 保护请求计数与关闭事实。
	mutex sync.Mutex
	// runtimeVersion 是真实 CLI 版本元数据替身。
	runtimeVersion string
	// newCalls 记录临时会话创建次数。
	newCalls int
	// promptCalls 检查管理操作没有发起模型回合。
	promptCalls int
	// closeSessionCalls 记录会话资源释放次数。
	closeSessionCalls int
	// configCalls 记录目录模型切换次数。
	configCalls int
	// selectedConfigs 保存已请求的配置值。
	selectedConfigs map[acp.SessionConfigId]acp.SessionConfigValueId
	// configOptions 是临时会话初始配置目录。
	configOptions []acp.SessionConfigOption
	// configOptionsByModel 保存每个模型的实际目录回执。
	configOptionsByModel map[acp.SessionConfigValueId][]acp.SessionConfigOption
	// sessionID 覆盖临时会话身份。
	sessionID acp.SessionId
	// newSessionError 模拟认证或创建故障。
	newSessionError error
	// closed 记录原生管理适配器已释放。
	closed bool
}

// installFakeCodexACPAgent 替换 SDK 的 Codex Agent 工厂并在测试结束时释放资源。
func installFakeCodexACPAgent(
	t *testing.T,
	sdk *Catalog,
	agent *fakeCodexACPAgent,
) {
	t.Helper()
	backend, ok := sdk.acp.(*acpBackend)
	if !ok {
		t.Fatalf("ACP backend = %T", sdk.acp)
	}
	replaceACPAgentFactory(backend, runtimeconfig.RuntimeIDCodex, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return agent, nil
	})
	t.Cleanup(func() {
		if err := sdk.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

// replaceACPAgentFactory 只替换目标测试 Backend 的一个 Runtime Agent 工厂。
func replaceACPAgentFactory(
	backend *acpBackend,
	baseRuntimeID string,
	factory acpAgentFactory,
) {
	previous := backend.openAgent
	backend.openAgent = func(ctx context.Context, prepared *launch.PreparedAgent, config acpAdapterConfig) (acp.Agent, error) {
		if config.BaseRuntimeID == baseRuntimeID {
			return factory(ctx, config)
		}
		return previous(ctx, prepared, config)
	}
}

// testCodexACPConfigOptions 创建 model 与当前模型 reasoning_effort 配置。
func testCodexACPConfigOptions(
	currentModel string,
	models []acp.SessionConfigSelectOption,
	reasoning []acp.SessionConfigSelectOption,
) []acp.SessionConfigOption {
	modelValues := acp.SessionConfigSelectOptionsUngrouped(models)
	options := []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("model"),
		Name:         "Model",
		CurrentValue: acp.SessionConfigValueId(currentModel),
		Options: acp.SessionConfigSelectOptions{
			Ungrouped: &modelValues,
		},
		Type: "select",
	}}}
	if reasoning == nil {
		return options
	}
	reasoningValues := acp.SessionConfigSelectOptionsUngrouped(reasoning)
	options = append(options, acp.SessionConfigOption{Select: &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("reasoning_effort"),
		Name:         "Reasoning effort",
		CurrentValue: reasoningValues[0].Value,
		Options: acp.SessionConfigSelectOptions{
			Ungrouped: &reasoningValues,
		},
		Type: "select",
	}})
	return options
}

// testCodexACPAgentWithCatalog 创建声明两个模型及不同推理强度的 fake Agent。
func testCodexACPAgentWithCatalog() *fakeCodexACPAgent {
	models := []acp.SessionConfigSelectOption{
		{Value: "gpt-fixture", Name: "GPT Fixture"},
		{Value: "gpt-fast", Name: "GPT Fast"},
	}
	fixtureOptions := testCodexACPConfigOptions(
		"gpt-fixture",
		models,
		[]acp.SessionConfigSelectOption{
			{Value: "medium", Name: "Medium"},
			{Value: "high", Name: "High"},
		},
	)
	fastOptions := testCodexACPConfigOptions(
		"gpt-fast",
		models,
		[]acp.SessionConfigSelectOption{{Value: "low", Name: "Low"}},
	)
	return &fakeCodexACPAgent{
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		configOptions:   fixtureOptions,
		configOptionsByModel: map[acp.SessionConfigValueId][]acp.SessionConfigOption{
			"gpt-fixture": fixtureOptions,
			"gpt-fast":    fastOptions,
		},
	}
}

// cloneACPConfigOptions 为 fake Agent 返回非 nil 的 ACP 配置切片。
func cloneACPConfigOptions(options []acp.SessionConfigOption) []acp.SessionConfigOption {
	return append([]acp.SessionConfigOption{}, options...)
}

// Initialize 返回 Codex 测试所需的 Session 能力。
func (agent *fakeCodexACPAgent) Initialize(
	context.Context,
	acp.InitializeRequest,
) (acp.InitializeResponse, error) {
	version := agent.runtimeVersion
	if version == "" {
		version = "0.149.1"
	}
	sessionCapabilities := acp.SessionCapabilities{
		AdditionalDirectories: &acp.SessionAdditionalDirectoriesCapabilities{},
		Close:                 &acp.SessionCloseCapabilities{},
		Resume:                &acp.SessionResumeCapabilities{},
	}
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: true,
			McpCapabilities: acp.McpCapabilities{
				Http: true,
				Sse:  true,
			},
			PromptCapabilities: acp.PromptCapabilities{
				Image: true,
			},
			SessionCapabilities: sessionCapabilities,
		},
		AgentInfo: &acp.Implementation{
			Name:    "ally-test-acp-agent",
			Version: "fake-agent 1.2.3",
			Meta: map[string]any{
				"runtime": map[string]any{"version": version},
			},
		},
		Meta: map[string]any{
			"steering": map[string]any{"supported": true},
		},
	}, nil
}

// NewSession 返回固定的新 Session。
func (agent *fakeCodexACPAgent) NewSession(
	_ context.Context,
	_ acp.NewSessionRequest,
) (acp.NewSessionResponse, error) {
	agent.mutex.Lock()
	agent.newCalls++
	sessionID := agent.sessionID
	if sessionID == "" {
		sessionID = "codex-acp-session"
	}
	newSessionError := agent.newSessionError
	agent.mutex.Unlock()
	if newSessionError != nil {
		return acp.NewSessionResponse{}, newSessionError
	}
	return acp.NewSessionResponse{
		SessionId:     sessionID,
		ConfigOptions: cloneACPConfigOptions(agent.configOptions),
	}, nil
}

// SetSessionConfigOption 记录模型和推理配置。
func (agent *fakeCodexACPAgent) SetSessionConfigOption(
	_ context.Context,
	request acp.SetSessionConfigOptionRequest,
) (acp.SetSessionConfigOptionResponse, error) {
	options := cloneACPConfigOptions(agent.configOptions)
	if request.ValueId != nil {
		agent.mutex.Lock()
		agent.configCalls++
		agent.selectedConfigs[request.ValueId.ConfigId] = request.ValueId.Value
		if request.ValueId.ConfigId == acp.SessionConfigId("model") {
			if configured, exists := agent.configOptionsByModel[request.ValueId.Value]; exists {
				options = cloneACPConfigOptions(configured)
			}
		}
		agent.mutex.Unlock()
	}
	return acp.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
}

// Cancel 满足 Agent；测试 Prompt 不阻塞。
func (*fakeCodexACPAgent) Cancel(context.Context, acp.CancelNotification) error {
	return nil
}

// CloseSession 满足 Agent 的 Session 资源释放接口。
func (agent *fakeCodexACPAgent) CloseSession(
	context.Context,
	acp.CloseSessionRequest,
) (acp.CloseSessionResponse, error) {
	agent.mutex.Lock()
	agent.closeSessionCalls++
	agent.mutex.Unlock()
	return acp.CloseSessionResponse{}, nil
}

// ListSessions 返回未公开能力错误。
func (*fakeCodexACPAgent) ListSessions(
	context.Context,
	acp.ListSessionsRequest,
) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionList)
}

// Authenticate 返回未使用的空响应。
func (*fakeCodexACPAgent) Authenticate(
	context.Context,
	acp.AuthenticateRequest,
) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

// Logout 返回未使用的空响应。
func (*fakeCodexACPAgent) Logout(
	context.Context,
	acp.LogoutRequest,
) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, nil
}

// Close 记录 acpserver 已释放 Adapter。
func (agent *fakeCodexACPAgent) Close(context.Context) error {
	agent.mutex.Lock()
	agent.closed = true
	agent.mutex.Unlock()
	return nil
}

// Prompt 使任何意外模型调用可被测试观察并立即失败。
func (agent *fakeCodexACPAgent) Prompt(context.Context, acp.PromptRequest) (acp.PromptResponse, error) {
	agent.mutex.Lock()
	agent.promptCalls++
	agent.mutex.Unlock()
	return acp.PromptResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionPrompt)
}

// SetSessionMode 不参与目录读取，保留标准接口的明确拒绝。
func (*fakeCodexACPAgent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionSetMode)
}

var _ acp.Agent = (*fakeCodexACPAgent)(nil)

// ResumeSession 拒绝管理能力之外的恢复请求，防止探测重用真实会话。
func (*fakeCodexACPAgent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionResume)
}
