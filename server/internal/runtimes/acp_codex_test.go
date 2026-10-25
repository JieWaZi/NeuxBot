package runtimes

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// fakeCodexACPAgent 提供完整双向 ACP 行为，不启动真实 Codex。
type fakeCodexACPAgent struct {
	// mutex 保护 connection、调用计数和关闭状态。
	mutex sync.Mutex
	// connection 是 acpserver 注入的 Agent 侧连接。
	connection *acp.AgentSideConnection
	// runtimeVersion 模拟当前安装的 CLI 版本；空值沿用测试默认版本。
	runtimeVersion string
	// newCalls 记录 session/new 次数。
	newCalls int
	// resumeCalls 记录 session/resume 次数。
	resumeCalls int
	// promptCalls 记录 session/prompt 次数。
	promptCalls int
	// closeSessionCalls 记录原生 session/close 次数。
	closeSessionCalls int
	// selectedPermission 保存 Client 自动选择的权限选项。
	selectedPermission acp.PermissionOptionId
	// selectedMode 保存最近一次 Session Mode。
	selectedMode acp.SessionModeId
	// selectedConfigs 保存按配置标识记录的选择。
	selectedConfigs map[acp.SessionConfigId]acp.SessionConfigValueId
	// configCalls 记录配置请求次数，用于验证热会话不会重复初始化同一模型。
	configCalls int
	// configOptions 保存新建 Session 返回的初始 ACP 配置。
	configOptions []acp.SessionConfigOption
	// configOptionsByModel 保存选择不同模型后返回的完整 ACP 配置。
	configOptionsByModel map[acp.SessionConfigValueId][]acp.SessionConfigOption
	// sessionID 覆盖测试 Agent 新建 Session 返回的标识；空值使用 Codex 固定值。
	sessionID acp.SessionId
	// sessionIDs 按 session/new 顺序提供不同标识，用于并发隔离测试。
	sessionIDs []acp.SessionId
	// newSessionRequest 保存最近一次标准 ACP session/new 输入。
	newSessionRequest acp.NewSessionRequest
	// promptRequest 保存最近一次标准 ACP session/prompt 输入。
	promptRequest acp.PromptRequest
	// missingResume 表示恢复请求返回标准 ResourceNotFound。
	missingResume bool
	// blockPrompt 表示 Prompt 等待 Context 取消后返回。
	blockPrompt bool
	// promptError 是 Prompt 写入成功后返回的协议错误。
	promptError error
	// newSessionError 是配置探测或执行创建 Session 时返回的协议错误。
	newSessionError error
	// simplePrompt 表示 Prompt 只返回正文，不模拟工具或权限请求。
	simplePrompt bool
	// supportsSSE 表示测试 Agent 是否像 Claude upstream 一样声明 SSE MCP。
	supportsSSE bool
	// disableSessionPersistence 表示测试 Agent 不声明可恢复和可关闭 Session。
	disableSessionPersistence bool
	// scriptedUpdates 保存测试需要按序发送的标准 ACP Session Update。
	scriptedUpdates []acp.SessionUpdate
	// scriptedResponse 保存脚本化更新完成后的标准 PromptResponse。
	scriptedResponse *acp.PromptResponse
	// promptStarted 在阻塞 Prompt 已收到请求时关闭。
	promptStarted chan struct{}
	// promptStartOnce 保证 promptStarted 只关闭一次。
	promptStartOnce sync.Once
	// closed 表示 acpserver 已调用 Adapter Close。
	closed bool
}

// SetAgentConnection 保存 Agent 主动通知 Client 所需的连接。
func (agent *fakeCodexACPAgent) SetAgentConnection(
	connection *acp.AgentSideConnection,
) {
	agent.mutex.Lock()
	agent.connection = connection
	agent.mutex.Unlock()
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
	if agent.disableSessionPersistence {
		sessionCapabilities = acp.SessionCapabilities{}
	}
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: true,
			McpCapabilities: acp.McpCapabilities{
				Http: true,
				Sse:  agent.supportsSSE,
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
	request acp.NewSessionRequest,
) (acp.NewSessionResponse, error) {
	agent.mutex.Lock()
	agent.newCalls++
	agent.newSessionRequest = request
	sessionID := agent.sessionID
	callIndex := agent.newCalls - 1
	if callIndex >= 0 && callIndex < len(agent.sessionIDs) {
		sessionID = agent.sessionIDs[callIndex]
	}
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

// ResumeSession 恢复固定 Session，或返回标准资源缺失错误。
func (agent *fakeCodexACPAgent) ResumeSession(
	context.Context,
	acp.ResumeSessionRequest,
) (acp.ResumeSessionResponse, error) {
	agent.mutex.Lock()
	agent.resumeCalls++
	missing := agent.missingResume
	agent.mutex.Unlock()
	if missing {
		return acp.ResumeSessionResponse{}, &acp.RequestError{
			Code:    -32002,
			Message: "Resource not found",
		}
	}
	return acp.ResumeSessionResponse{}, nil
}

// Prompt 发送消息、思考、工具、上下文 Usage，并返回 Turn Usage。
func (agent *fakeCodexACPAgent) Prompt(
	ctx context.Context,
	request acp.PromptRequest,
) (acp.PromptResponse, error) {
	agent.mutex.Lock()
	agent.promptCalls++
	agent.promptRequest = request
	connection := agent.connection
	blockPrompt := agent.blockPrompt
	promptError := agent.promptError
	simplePrompt := agent.simplePrompt
	scriptedUpdates := append([]acp.SessionUpdate(nil), agent.scriptedUpdates...)
	scriptedResponse := agent.scriptedResponse
	agent.mutex.Unlock()
	if blockPrompt {
		if err := connection.SessionUpdate(ctx, acp.SessionNotification{
			SessionId: request.SessionId,
			Update:    acp.UpdateAgentMessageText("已开始。"),
		}); err != nil {
			return acp.PromptResponse{}, err
		}
		agent.promptStartOnce.Do(func() {
			close(agent.promptStarted)
		})
		<-ctx.Done()
		if promptError != nil {
			return acp.PromptResponse{}, promptError
		}
		return acp.PromptResponse{}, ctx.Err()
	}
	if promptError != nil {
		return acp.PromptResponse{}, promptError
	}
	if scriptedResponse != nil {
		for _, update := range scriptedUpdates {
			if err := connection.SessionUpdate(ctx, acp.SessionNotification{
				SessionId: request.SessionId,
				Update:    update,
			}); err != nil {
				return acp.PromptResponse{}, err
			}
		}
		return *scriptedResponse, nil
	}
	if simplePrompt {
		if err := connection.SessionUpdate(ctx, acp.SessionNotification{
			SessionId: request.SessionId,
			Update:    acp.UpdateAgentMessageText("OK"),
		}); err != nil {
			return acp.PromptResponse{}, err
		}
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}
	permission, err := connection.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: request.SessionId,
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "permission-1",
		},
		Options: []acp.PermissionOption{
			{
				OptionId: "allow_once",
				Name:     "Allow Once",
				Kind:     acp.PermissionOptionKindAllowOnce,
			},
			{
				OptionId: "reject_once",
				Name:     "Reject",
				Kind:     acp.PermissionOptionKindRejectOnce,
			},
		},
	})
	if err != nil {
		return acp.PromptResponse{}, err
	}
	if permission.Outcome.Selected != nil {
		agent.mutex.Lock()
		agent.selectedPermission = permission.Outcome.Selected.OptionId
		agent.mutex.Unlock()
	}
	updates := []acp.SessionUpdate{
		{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Meta: map[string]any{
					"codex": map[string]any{"phase": "commentary"},
				},
				Content: acp.TextBlock("正在检查。"),
			},
		},
		acp.UpdateAgentThoughtText("分析调用链。"),
		acp.StartEditToolCall(
			"tool-1",
			"Edit file",
			"/workspace/main.go",
			map[string]any{"replacement": "new"},
		),
		acp.UpdateToolCall(
			"tool-1",
			acp.WithUpdateKind(acp.ToolKindEdit),
			acp.WithUpdateStatus(acp.ToolCallStatusCompleted),
		),
		{
			UsageUpdate: &acp.SessionUsageUpdate{
				Used: 120,
				Size: 200000,
			},
		},
		acp.UpdateAgentMessageText("完成。"),
	}
	for _, update := range updates {
		if err := connection.SessionUpdate(ctx, acp.SessionNotification{
			SessionId: request.SessionId,
			Update:    update,
		}); err != nil {
			return acp.PromptResponse{}, err
		}
	}
	cacheRead := 3
	cacheWrite := 2
	thought := 4
	return acp.PromptResponse{
		StopReason: acp.StopReasonEndTurn,
		Usage: &acp.Usage{
			InputTokens:       10,
			CachedReadTokens:  &cacheRead,
			CachedWriteTokens: &cacheWrite,
			OutputTokens:      5,
			ThoughtTokens:     &thought,
			TotalTokens:       20,
		},
	}, nil
}

// TestCodexACPToolPayloadNormalizesStandardToolFacts 验证 NeuxBot 只归一化 ACP 标准字段。
func TestCodexACPToolPayloadNormalizesStandardToolFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// name 是工具载荷归一化场景名称。
		name string
		// update 是 acp-go 发出的标准 ToolCall 或 ToolCallUpdate。
		update any
		// wantName 是 NeuxBot 跨 Runtime 使用的规范工具名。
		wantName string
		// wantType 是需要补充给产品投影的原生动作类型。
		wantType string
	}{
		{
			name: "mcp identity",
			update: acp.StartToolCall(
				"mcp-1",
				"mcp.ally_memory_builtin.memory_search",
				acp.WithStartKind(acp.ToolKindExecute),
				acp.WithStartStatus(acp.ToolCallStatusInProgress),
				acp.WithStartRawInput(map[string]any{
					"server":    "ally_memory_builtin",
					"tool":      "memory_search",
					"arguments": map[string]any{"query": "runtime"},
				}),
			),
			wantName: "mcp__ally_memory_builtin__memory_search",
		},
		{
			name: "web search",
			update: acp.StartToolCall(
				"web-1",
				"Web search: ACP",
				acp.WithStartKind(acp.ToolKindSearch),
				acp.WithStartStatus(acp.ToolCallStatusInProgress),
				acp.WithStartRawInput(map[string]any{
					"type":   "webSearch",
					"id":     "web-1",
					"action": map[string]any{"type": "search", "query": "ACP"},
				}),
			),
			wantName: "web_search",
			wantType: "webSearch",
		},
		{
			name: "image view",
			update: acp.StartToolCall(
				"image-1",
				"View Image /workspace/chart.png",
				acp.WithStartKind(acp.ToolKindRead),
				acp.WithStartStatus(acp.ToolCallStatusCompleted),
				acp.WithStartRawInput(map[string]any{"path": "/workspace/chart.png"}),
			),
			wantName: "view_image",
			wantType: "imageView",
		},
		{
			name: "skill",
			update: acp.StartToolCall(
				"skill-1",
				"Load skill: diagnosing-bugs",
				acp.WithStartKind(acp.ToolKindOther),
				acp.WithStartStatus(acp.ToolCallStatusInProgress),
				acp.WithStartRawInput(map[string]any{"skill": "diagnosing-bugs"}),
			),
			wantName: "skill",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			payload, err := acpToolPayload(test.update, codexACPRuntimeBinding())
			if err != nil {
				t.Fatalf("acpToolPayload() error = %v", err)
			}
			if payload["name"] != test.wantName {
				t.Fatalf("name = %#v, want %q: %#v", payload["name"], test.wantName, payload)
			}
			if test.wantType != "" && payload["type"] != test.wantType {
				t.Fatalf("type = %#v, want %q: %#v", payload["type"], test.wantType, payload)
			}
			if payload["input"] == nil {
				t.Fatalf("input is missing: %#v", payload)
			}
		})
	}
}

// TestCodexACPCompletedToolCallEmitsUseAndResult 验证单条 completed ToolCall 不会永久显示运行中。
func TestCodexACPCompletedToolCallEmitsUseAndResult(t *testing.T) {
	t.Parallel()

	bridge := newACPBridge(
		"session-1",
		"invocation-1",
		nil,
		Workspace{},
		ExecutionPolicy{},
		codexACPRuntimeBinding(),
		0,
	)
	update := acp.StartToolCall(
		"image-1",
		"View Image /workspace/chart.png",
		acp.WithStartKind(acp.ToolKindRead),
		acp.WithStartStatus(acp.ToolCallStatusCompleted),
		acp.WithStartRawInput(map[string]any{"path": "/workspace/chart.png"}),
	)
	events, err := bridge.mapNotification(acp.SessionNotification{
		SessionId: "session-1",
		Update:    update,
	})
	if err != nil {
		t.Fatalf("mapNotification() error = %v", err)
	}
	if len(events) != 2 || events[0].Type != EventTypeToolUse || events[1].Type != EventTypeToolResult {
		t.Fatalf("events = %#v", events)
	}
}

// TestCodexACPIntermediateDiffUpdateEmitsToolRefinement 验证标准 ACP diff 更新可刷新工具摘要。
func TestCodexACPIntermediateDiffUpdateEmitsToolRefinement(t *testing.T) {
	t.Parallel()

	bridge := newACPBridge(
		"session-1",
		"invocation-1",
		nil,
		Workspace{},
		ExecutionPolicy{},
		codexACPRuntimeBinding(),
		0,
	)
	update := acp.UpdateToolCall(
		"file-1",
		acp.WithUpdateContent([]acp.ToolCallContent{
			acp.ToolDiffContent("/workspace/main.go", "new\n", "old\n"),
		}),
	)
	events, err := bridge.mapNotification(acp.SessionNotification{
		SessionId: "session-1",
		Update:    update,
	})
	if err != nil {
		t.Fatalf("mapNotification() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != EventTypeToolUse {
		t.Fatalf("events = %#v", events)
	}
	var payload map[string]any
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["name"] != "file_modify" || payload["content"] == nil {
		t.Fatalf("payload = %#v", payload)
	}
}

// SetSessionMode 记录当前产品权限映射得到的原生模式。
func (agent *fakeCodexACPAgent) SetSessionMode(
	_ context.Context,
	request acp.SetSessionModeRequest,
) (acp.SetSessionModeResponse, error) {
	agent.mutex.Lock()
	agent.selectedMode = request.ModeId
	agent.mutex.Unlock()
	return acp.SetSessionModeResponse{}, nil
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
		if request.ValueId.ConfigId == codexACPModelConfigID {
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

// TestCodexACPBackendMapsRuntimeContractAndReusesAgent 锁住完整内嵌 ACP 行为。
func TestCodexACPBackendMapsRuntimeContractAndReusesAgent(t *testing.T) {
	t.Parallel()

	backend := newACPBackend()
	fakeAgent := &fakeCodexACPAgent{
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
	}
	factoryCalls := 0
	var adapterConfig acpAdapterConfig
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(
		_ context.Context,
		config acpAdapterConfig,
	) (acp.Agent, error) {
		factoryCalls++
		adapterConfig = config
		return fakeAgent, nil
	})
	t.Cleanup(func() {
		if err := backend.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	defaults, err := normalizeDefaults(Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	request := acpRunRequest{
		InvocationID: "invocation-acp-1",
		Prepared: preparedRuntime{
			definition: runtimeDefinition{
				id:            RuntimeIDCodex,
				baseRuntimeID: RuntimeIDCodex,
				prefixArgs:    []string{"--profile", "team"},
			},
			detection: DetectionResult{
				ResolvedBinary: "/fake/codex",
				Version:        "codex 1.0.0",
			},
			environment: environmentSnapshot{
				values:      map[string]string{"CODEX_HOME": "/custom/codex-home"},
				names:       map[string]string{"CODEX_HOME": "CODEX_HOME"},
				fingerprint: "environment-1",
			},
			defaults: defaults,
		},
		Request: RunRequest{
			Target:    RuntimeTarget{RuntimeID: RuntimeIDCodex},
			Workspace: Workspace{Path: workspace, Managed: true},
			Session:   SessionDirective{Mode: SessionModeNew},
			ApprovalHandler: approvalHandlerFunc(func(
				_ context.Context,
				request ApprovalRequest,
			) (ApprovalDecision, error) {
				for _, option := range request.Options {
					if option.Kind == string(acp.PermissionOptionKindAllowOnce) {
						return ApprovalDecision{OptionID: option.ID}, nil
					}
				}
				return ApprovalDecision{Cancelled: true}, nil
			}),
			Policy: RunPolicy{
				Model:     "gpt-test",
				Reasoning: "high",
				Execution: ExecutionPolicy{
					PermissionLevel: PermissionLevelAuto,
				},
			},
		},
		Prompt: "inspect",
		Sink:   &recordingSink{},
	}
	result := backend.Run(context.Background(), request)
	if result.Failure != nil || result.Session.State != SessionStateCreated ||
		result.Session.OutputID != "codex-acp-session" {
		t.Fatalf("first Result = %#v", result)
	}
	if adapterConfig.Executable != "/fake/codex" ||
		!reflect.DeepEqual(adapterConfig.PrefixArgs, []string{"--profile", "team"}) ||
		!reflect.DeepEqual(adapterConfig.Environment, []string{"CODEX_HOME=/custom/codex-home"}) {
		t.Fatalf("Codex Adapter Config = %#v", adapterConfig)
	}
	assertCodexACPEvents(t, request.Sink.(*recordingSink).events)
	if !result.SideEffects.VisibleOutput || !result.SideEffects.ReasoningOutput ||
		!result.SideEffects.ToolUse || !result.SideEffects.FileWrite {
		t.Fatalf("SideEffects = %#v", result.SideEffects)
	}

	request.InvocationID = "invocation-acp-2"
	request.Request.Session = SessionDirective{
		Mode: SessionModeResume,
		ID:   "codex-acp-session",
	}
	request.Sink = &recordingSink{}
	result = backend.Run(context.Background(), request)
	if result.Failure != nil || result.Session.State != SessionStateResumed {
		t.Fatalf("resume Result = %#v", result)
	}
	if factoryCalls != 1 {
		t.Fatalf("factoryCalls = %d, want 1", factoryCalls)
	}
	fakeAgent.mutex.Lock()
	if fakeAgent.newCalls != 1 || fakeAgent.resumeCalls != 0 ||
		fakeAgent.promptCalls != 2 || fakeAgent.closeSessionCalls != 0 ||
		fakeAgent.selectedPermission != "allow_once" ||
		fakeAgent.selectedMode != "agent" ||
		fakeAgent.selectedConfigs["model"] != "gpt-test" ||
		fakeAgent.selectedConfigs["reasoning_effort"] != "high" {
		fakeAgent.mutex.Unlock()
		t.Fatalf("fake Agent state = %#v", fakeAgent)
	}
	fakeAgent.mutex.Unlock()
	if err := backend.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	fakeAgent.mutex.Lock()
	closed := fakeAgent.closed
	closeSessionCalls := fakeAgent.closeSessionCalls
	fakeAgent.mutex.Unlock()
	if !closed {
		t.Fatal("embedded Agent was not closed")
	}
	if closeSessionCalls != 1 {
		t.Fatalf("closeSessionCalls = %d, want 1", closeSessionCalls)
	}
}

// TestSDKHasOnlyACPBindingsForBuiltinsAndProfiles 锁住内置 Runtime 与 Profile 只使用 ACP。
func TestSDKHasOnlyACPBindingsForBuiltinsAndProfiles(t *testing.T) {
	t.Parallel()

	sdk, err := New(Config{Profiles: []ProfileSpec{
		{ID: "codex-profile", BaseRuntimeID: RuntimeIDCodex},
		{ID: "claude-profile", BaseRuntimeID: RuntimeIDClaude},
	}})
	if err != nil {
		t.Fatal(err)
	}
	backend, ok := sdk.acp.(*acpBackend)
	if !ok {
		t.Fatalf("ACP backend = %T", sdk.acp)
	}
	for _, runtimeID := range []string{
		RuntimeIDCodex,
		RuntimeIDClaude,
		"codex-profile",
		"claude-profile",
	} {
		definition := sdk.definitions[runtimeID]
		if _, err := backend.binding(definition.baseRuntimeID); err != nil {
			t.Fatalf("runtime %q ACP binding: %v", runtimeID, err)
		}
	}
	if got := runtimeSkillRoot(DetectionResult{Runtime: RuntimeSummary{
		BaseRuntimeID:      RuntimeIDCodex,
		SkillDirectoryName: ".agents",
	}}, "/workspace"); got != "/workspace/.agents/skills" {
		t.Fatalf("runtimeSkillRoot = %q", got)
	}
	if got := runtimeSkillRoot(DetectionResult{Runtime: RuntimeSummary{
		BaseRuntimeID:      RuntimeIDClaude,
		SkillDirectoryName: ".claude",
	}}, "/workspace"); got != "/workspace/.claude/skills" {
		t.Fatalf("Claude runtimeSkillRoot = %q", got)
	}
}

// TestCodexACPAgentKeySeparatesPrefixArgs 验证不同前置参数不会错误复用同一 ACP Agent。
func TestCodexACPAgentKeySeparatesPrefixArgs(t *testing.T) {
	t.Parallel()

	request := acpRunRequest{
		Prepared: preparedRuntime{
			definition:  runtimeDefinition{prefixArgs: []string{"--profile", "one"}},
			detection:   DetectionResult{ResolvedBinary: "/fake/codex"},
			environment: environmentSnapshot{fingerprint: "environment"},
		},
		Request: RunRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}},
	}
	first := acpAgentKey(request)
	request.Prepared.definition.prefixArgs = []string{"--profile", "two"}
	second := acpAgentKey(request)
	if first == second {
		t.Fatalf("不同前置参数生成了相同 Agent Key %q", first)
	}
	request.Prepared.definition.prefixArgs = nil
	withoutArgument := acpAgentKey(request)
	request.Prepared.definition.prefixArgs = []string{""}
	withEmptyArgument := acpAgentKey(request)
	if withoutArgument == withEmptyArgument {
		t.Fatalf("无前置参数与空前置参数生成了相同 Agent Key %q", withoutArgument)
	}
}

// TestACPAgentKeySeparatesDetectedVersions 防止同一路径升级后继续复用旧进程。
func TestACPAgentKeySeparatesDetectedVersions(t *testing.T) {
	request := acpRunRequest{
		Prepared: preparedRuntime{
			definition:  runtimeDefinition{baseRuntimeID: RuntimeIDCodex},
			detection:   DetectionResult{ResolvedBinary: "/fake/codex", Version: "a"},
			environment: environmentSnapshot{fingerprint: "environment"},
		},
		Request: RunRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}},
	}
	first := acpAgentKey(request)
	request.Prepared.detection.Version = "b"
	if first == acpAgentKey(request) {
		t.Fatal("CLI 升级后仍复用旧版本 Agent")
	}
}

// TestACPAgentKeyUsesResolvedBinding 验证复用键服从当前 Backend 已解析的厂商隔离策略。
func TestACPAgentKeyUsesResolvedBinding(t *testing.T) {
	t.Parallel()

	request := acpRunRequest{
		Prepared: preparedRuntime{
			detection:   DetectionResult{ResolvedBinary: "/fake/agent"},
			environment: environmentSnapshot{fingerprint: "environment"},
		},
		Request: RunRequest{Target: RuntimeTarget{RuntimeID: "custom-profile"}},
	}
	firstBinding := standardACPRuntimeBinding()
	firstBinding.agentScope = func(acpRunRequest) string { return "scope-one" }
	secondBinding := standardACPRuntimeBinding()
	secondBinding.agentScope = func(acpRunRequest) string { return "scope-two" }

	first := acpAgentKeyForBinding(request, firstBinding)
	second := acpAgentKeyForBinding(request, secondBinding)
	if first == second {
		t.Fatalf("不同绑定隔离策略生成了相同 Agent Key %q", first)
	}
}

// TestCodexACPBackendMapsMissingSession 验证标准资源缺失不依赖错误文本。
func TestCodexACPBackendMapsMissingSession(t *testing.T) {
	t.Parallel()

	backend := newACPBackend()
	fakeAgent := &fakeCodexACPAgent{
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		missingResume:   true,
	}
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return fakeAgent, nil
	})
	t.Cleanup(func() {
		if err := backend.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	defaults, err := normalizeDefaults(Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	result := backend.Run(context.Background(), acpRunRequest{
		InvocationID: "invocation-missing",
		Prepared: preparedRuntime{
			definition:  runtimeDefinition{id: RuntimeIDCodex, baseRuntimeID: RuntimeIDCodex},
			detection:   DetectionResult{ResolvedBinary: "/fake/codex"},
			environment: environmentSnapshot{fingerprint: "environment-missing"},
			defaults:    defaults,
		},
		Request: RunRequest{
			Target:    RuntimeTarget{RuntimeID: RuntimeIDCodex},
			Workspace: Workspace{Path: t.TempDir(), Managed: true},
			Message:   Message{Text: "continue"},
			Session: SessionDirective{
				Mode: SessionModeResume,
				ID:   "missing-session",
			},
			Policy: RunPolicy{Execution: ExecutionPolicy{
				PermissionLevel: PermissionLevelAuto,
			}},
		},
		Prompt: "continue",
		Sink:   &recordingSink{},
	})
	if result.Failure == nil || result.Failure.Code != FailureCodeSessionNotFound ||
		result.Session.State != SessionStateMissing {
		t.Fatalf("Result = %#v", result)
	}
}

// TestCodexACPBackendPreservesAcceptedOutputOnCancel 验证取消后保留已交付消息。
func TestCodexACPBackendPreservesAcceptedOutputOnCancel(t *testing.T) {
	t.Parallel()

	backend := newACPBackend()
	fakeAgent := &fakeCodexACPAgent{
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		blockPrompt:     true,
		promptStarted:   make(chan struct{}),
	}
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return fakeAgent, nil
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	request := testCodexACPRunRequest(t, "invocation-cancel")
	sink := newSignalingRecordingSink()
	request.Sink = sink
	runContext, cancelRun := context.WithCancel(context.Background())
	resultChannel := make(chan Result, 1)
	go func() {
		resultChannel <- backend.Run(runContext, request)
	}()
	<-fakeAgent.promptStarted
	sink.waitForEvent(t, EventTypeTextDelta)
	cancelRun()
	result := <-resultChannel
	if result.Failure == nil ||
		result.Failure.Code != FailureCodeContextCanceled ||
		result.Session.State != SessionStateCreated ||
		!result.SideEffects.VisibleOutput {
		t.Fatalf(
			"Failure=%+v Session=%+v SideEffects=%+v Events=%#v",
			result.Failure,
			result.Session,
			result.SideEffects,
			sink.eventTypes(),
		)
	}
	types := sink.eventTypes()
	if len(types) != 2 || types[0] != EventTypeInvocationStarted ||
		types[1] != EventTypeTextDelta {
		t.Fatalf("event types = %#v", types)
	}
}

// TestACPBackendStopsInactivePrompt 验证共享 ACP Client 的语义无活动保护。
func TestACPBackendStopsInactivePrompt(t *testing.T) {
	t.Parallel()

	backend := newACPBackend()
	agent := &fakeCodexACPAgent{
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		blockPrompt:     true,
		promptStarted:   make(chan struct{}),
	}
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return agent, nil
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	defaults, err := normalizeDefaults(Defaults{Inactivity: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	request := acpRunRequest{
		InvocationID: "invocation-inactivity",
		Prepared: preparedRuntime{
			definition: runtimeDefinition{id: RuntimeIDCodex, baseRuntimeID: RuntimeIDCodex},
			detection: DetectionResult{
				Runtime:        RuntimeSummary{ID: RuntimeIDCodex, BaseRuntimeID: RuntimeIDCodex},
				ResolvedBinary: "/fake/codex",
			},
			environment: environmentSnapshot{fingerprint: "inactivity"},
			defaults:    defaults,
		},
		Request: RunRequest{
			Target:    RuntimeTarget{RuntimeID: RuntimeIDCodex},
			Workspace: Workspace{Path: t.TempDir(), Managed: true},
			Session:   SessionDirective{Mode: SessionModeNew},
			Policy: RunPolicy{Execution: ExecutionPolicy{
				PermissionLevel: PermissionLevelAuto,
			}},
		},
		Prompt: "wait",
		Sink:   &recordingSink{},
	}
	result := backend.Run(context.Background(), request)
	if result.Failure == nil || result.Failure.Code != FailureCodeInactivityTimeout {
		t.Fatalf("Result = %#v, want inactivity timeout", result)
	}
}

func TestACPBackendPausesInactivityTimeoutWhileApprovalIsPending(t *testing.T) {
	t.Parallel()

	backend := newACPBackend()
	agent := &fakeCodexACPAgent{
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
	}
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return agent, nil
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	defaults, err := normalizeDefaults(Defaults{Inactivity: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	request := testCodexACPRunRequest(t, "invocation-approval-inactivity")
	request.Prepared.defaults = defaults
	request.Sink = &recordingSink{}
	request.Request.ApprovalHandler = approvalHandlerFunc(func(
		ctx context.Context,
		request ApprovalRequest,
	) (ApprovalDecision, error) {
		select {
		case <-time.After(60 * time.Millisecond):
		case <-ctx.Done():
			return ApprovalDecision{Cancelled: true}, ctx.Err()
		}
		return ApprovalDecision{OptionID: request.Options[0].ID}, nil
	})

	result := backend.Run(context.Background(), request)
	if result.Failure != nil {
		t.Fatalf("Result = %#v, want approval wait without inactivity failure", result)
	}
}

// TestCodexACPBackendRecreatesUnavailableAgent 验证连接级失败淘汰长期 Agent。
func TestCodexACPBackendRecreatesUnavailableAgent(t *testing.T) {
	t.Parallel()

	backend := newACPBackend()
	created := 0
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		created++
		agent := &fakeCodexACPAgent{
			selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		}
		if created == 1 {
			agent.promptError = errors.New("Codex ACP agent unavailable")
		}
		return agent, nil
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	request := testCodexACPRunRequest(t, "invocation-unavailable-1")
	request.Sink = &recordingSink{}
	first := backend.Run(context.Background(), request)
	if first.Failure == nil || first.Failure.Code != FailureCodeRuntimeUnavailable {
		t.Fatalf("first Result = %#v", first)
	}
	request.InvocationID = "invocation-unavailable-2"
	request.Sink = &recordingSink{}
	second := backend.Run(context.Background(), request)
	if second.Failure != nil || created != 2 {
		t.Fatalf("second Result = %#v, created=%d", second, created)
	}
}

// TestCodexACPPermissionDoesNotProjectPrivateMeta 验证审批展示不解析私有权限元数据。
func TestCodexACPPermissionDoesNotProjectPrivateMeta(t *testing.T) {
	t.Parallel()

	title := "执行命令"
	request, valid := normalizeACPApprovalRequest(acp.RequestPermissionRequest{
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "tool-1",
			Title:      &title,
			RawInput: map[string]any{
				"command": "curl   https://chatgpt.com/",
				"headers": map[string]any{"Authorization": "must-not-project"},
			},
		},
		Options: []acp.PermissionOption{{
			OptionId: "allow_once", Name: "Allow once",
			Kind: acp.PermissionOptionKindAllowOnce,
			Meta: map[string]any{
				"permission": map[string]any{"secret": "must-not-project"},
			},
		}}})
	if !valid || request.ToolCallID != "tool-1" || request.Title != title ||
		len(request.Options) != 1 || request.Summary != title ||
		request.Command != "curl https://chatgpt.com/" {
		t.Fatalf("normalizeACPApprovalRequest() = %#v, %v", request, valid)
	}
}

func TestCodexACPPermissionBoundsCommandSummary(t *testing.T) {
	t.Parallel()

	request, valid := normalizeACPApprovalRequest(acp.RequestPermissionRequest{
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "tool-1",
			RawInput: map[string]any{
				"command": strings.Repeat("命", acpApprovalMaxCommandRunes+20),
			},
		},
		Options: []acp.PermissionOption{{
			OptionId: "allow_once",
			Name:     "Allow once",
			Kind:     acp.PermissionOptionKindAllowOnce,
		}},
	})
	if !valid || len([]rune(request.Command)) != acpApprovalMaxCommandRunes ||
		!strings.HasSuffix(request.Command, "…") {
		t.Fatalf(
			"normalizeACPApprovalRequest() command = %q, valid=%v",
			request.Command,
			valid,
		)
	}
}

func TestCodexACPPermissionRejectsInvalidSafeFields(t *testing.T) {
	t.Parallel()
	invalidLine := -1
	_, valid := normalizeACPApprovalRequest(acp.RequestPermissionRequest{
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "tool-1",
			Locations: []acp.ToolCallLocation{{
				Path: "/workspace/main.go",
				Line: &invalidLine,
			}},
		},
		Options: []acp.PermissionOption{{
			OptionId: "allow_once",
			Name:     "Allow once",
			Kind:     acp.PermissionOptionKindAllowOnce,
		}},
	})
	if valid {
		t.Fatal("normalizeACPApprovalRequest() accepted an invalid location")
	}
}

// assertCodexACPEvents 验证事件顺序和最终 Usage 自包含。
func assertCodexACPEvents(t *testing.T, events []Event) {
	t.Helper()
	types := make([]EventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	want := []EventType{
		EventTypeInvocationStarted,
		EventTypeCommentaryDelta,
		EventTypeThinkingStart,
		EventTypeThinkingDelta,
		EventTypeToolUse,
		EventTypeToolResult,
		EventTypeUsage,
		EventTypeTextDelta,
		EventTypeUsage,
	}
	if len(types) != len(want) {
		t.Fatalf("event types = %#v", types)
	}
	for index := range want {
		if types[index] != want[index] {
			t.Fatalf("event types = %#v", types)
		}
	}
	var usage UsageData
	if err := json.Unmarshal(events[len(events)-1].Data, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.Turn == nil || usage.Context == nil ||
		usage.Turn.InputTokens != 10 || usage.Turn.CacheReadTokens != 3 ||
		usage.Turn.CacheCreationTokens != 2 ||
		usage.Turn.ReasoningOutputTokens != 4 ||
		usage.Context.UsedTokens != 120 || usage.Context.WindowTokens != 200000 {
		t.Fatalf("Usage = %#v", usage)
	}
}

// testCodexACPRunRequest 创建独立 Workspace 的标准 ACP 调用输入。
func testCodexACPRunRequest(
	t *testing.T,
	invocationID string,
) acpRunRequest {
	t.Helper()
	defaults, err := normalizeDefaults(Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	return acpRunRequest{
		InvocationID: invocationID,
		Prepared: preparedRuntime{
			definition: runtimeDefinition{
				id:            RuntimeIDCodex,
				baseRuntimeID: RuntimeIDCodex,
			},
			detection: DetectionResult{
				ResolvedBinary: "/fake/codex",
				Version:        "codex 1.0.0",
			},
			environment: environmentSnapshot{fingerprint: invocationID},
			defaults:    defaults,
		},
		Request: RunRequest{
			Target:    RuntimeTarget{RuntimeID: RuntimeIDCodex},
			Workspace: Workspace{Path: t.TempDir(), Managed: true},
			Message:   Message{Text: "inspect"},
			Session:   SessionDirective{Mode: SessionModeNew},
			Policy: RunPolicy{Execution: ExecutionPolicy{
				PermissionLevel: PermissionLevelAuto,
			}},
		},
		Prompt: "inspect",
	}
}

// installFakeCodexACPAgent 替换 SDK 的 Codex Agent 工厂并在测试结束时释放资源。
func installFakeCodexACPAgent(
	t *testing.T,
	sdk *SDK,
	agent *fakeCodexACPAgent,
) {
	t.Helper()
	backend, ok := sdk.acp.(*acpBackend)
	if !ok {
		t.Fatalf("ACP backend = %T", sdk.acp)
	}
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(
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
	binding := backend.bindings[baseRuntimeID]
	binding.newAgent = factory
	backend.bindings[baseRuntimeID] = binding
}

// testCodexACPConfigOptions 创建 model 与当前模型 reasoning_effort 配置。
func testCodexACPConfigOptions(
	currentModel string,
	models []acp.SessionConfigSelectOption,
	reasoning []acp.SessionConfigSelectOption,
) []acp.SessionConfigOption {
	modelValues := acp.SessionConfigSelectOptionsUngrouped(models)
	options := []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{
		Id:           codexACPModelConfigID,
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
		Id:           codexACPReasoningConfigID,
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

var (
	_ acp.Agent = (*fakeCodexACPAgent)(nil)
	_ interface {
		Close(context.Context) error
	} = (*fakeCodexACPAgent)(nil)
)
