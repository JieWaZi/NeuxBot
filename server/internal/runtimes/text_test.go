package runtimes

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// textACPAgent 按真实配置回执确认每次模型和推理选择。
type textACPAgent struct {
	// fakeCodexACPAgent 复用标准双向 ACP 测试代理。
	*fakeCodexACPAgent
}

func (agent *textACPAgent) SetSessionConfigOption(ctx context.Context, request acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	response, err := agent.fakeCodexACPAgent.SetSessionConfigOption(ctx, request)
	if request.ValueId != nil {
		for _, option := range response.ConfigOptions {
			if option.Select != nil && option.Select.Id == request.ValueId.ConfigId {
				option.Select.CurrentValue = request.ValueId.Value
			}
		}
	}
	return response, err
}

func textTestOptions(reasoning ...string) []acp.SessionConfigOption {
	models := acp.SessionConfigSelectOptionsUngrouped{{Value: "model-a", Name: "Model A"}}
	values := acp.SessionConfigSelectOptionsUngrouped{}
	for _, value := range reasoning {
		values = append(values, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueId(value), Name: value})
	}
	result := []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{Id: "model", CurrentValue: "model-a", Options: acp.SessionConfigSelectOptions{Ungrouped: &models}}}}
	if len(values) > 0 {
		result = append(result, acp.SessionConfigOption{Select: &acp.SessionConfigOptionSelect{Id: codexACPReasoningConfigID, CurrentValue: values[len(values)-1].Value, Options: acp.SessionConfigSelectOptions{Ungrouped: &values}}})
	}
	return result
}

func TestGenerateTextIsFreshEphemeralAndPreservesNativeResult(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("CODEX_HOME", fixtureHome)
	if err := os.WriteFile(filepath.Join(fixtureHome, "auth.json"), []byte(`{"access_token":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}

	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	var agents []*textACPAgent
	backend := sdk.acp.(*acpBackend)
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(_ context.Context, config acpAdapterConfig) (acp.Agent, error) {
		if config.UserInput {
			t.Fatal("knowledge result call installed a host question bridge")
		}
		agent := &textACPAgent{&fakeCodexACPAgent{selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, configOptions: textTestOptions("none", "high"), scriptedUpdates: []acp.SessionUpdate{acp.UpdateAgentMessageText("answer")}, scriptedResponse: &acp.PromptResponse{StopReason: acp.StopReasonMaxTokens, Usage: &acp.Usage{InputTokens: 12, OutputTokens: 7}}}}
		agent.scriptedUpdates = append([]acp.SessionUpdate{{ToolCall: &acp.SessionUpdateToolCall{ToolCallId: "native-tool", Title: "read"}}}, agent.scriptedUpdates...)
		agents = append(agents, agent)
		return agent, nil
	})
	binary, _ := os.Executable()
	request := TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Messages: []TextMessage{{Role: "system", Content: "system rules"}, {Role: "user", Content: "first source"}}, MaxTokens: 32768, Temperature: .3}
	first := sdk.GenerateText(context.Background(), request)
	if first.Failure != nil || first.Text != "answer" || first.StopReason != "max_tokens" || first.Usage == nil || first.Usage.InputTokens != 12 || first.Usage.OutputTokens != 7 || first.ActualReasoning != "none" || !first.ThinkingDisabled {
		t.Fatalf("first = %+v", first)
	}
	request.Reasoning = "high"
	request.Messages = []TextMessage{{Role: "user", Content: "second source"}}
	second := sdk.GenerateText(context.Background(), request)
	if second.Failure != nil || len(agents) != 2 || second.ActualReasoning != "high" || second.ThinkingDisabled {
		t.Fatalf("second=%+v agents=%d", second, len(agents))
	}
	for index, agent := range agents {
		if agent.newCalls != 1 || agent.resumeCalls != 0 || agent.closeSessionCalls != 1 || !agent.closed || agent.selectedMode != "read-only" {
			t.Fatalf("agent %d leaked: %+v", index, agent.fakeCodexACPAgent)
		}
		if len(agent.newSessionRequest.McpServers) != 0 || len(agent.newSessionRequest.AdditionalDirectories) != 0 {
			t.Fatal("model session mounted external resources")
		}
		if _, err := os.Stat(agent.newSessionRequest.Cwd); !os.IsNotExist(err) {
			t.Fatalf("temporary workspace retained: %v", err)
		}
	}
	if agents[0].newSessionRequest.Cwd == agents[1].newSessionRequest.Cwd || strings.Contains(agents[1].promptRequest.Prompt[0].Text.Text, "first source") {
		t.Fatal("model contexts were reused")
	}
}

// textEventSpyBackend 观察文本调用是否误入聊天统一事件通道，仍执行真实 ACP 生命周期。
type textEventSpyBackend struct {
	// acpRunner 是实际执行会话和协议收尾的后端。
	acpRunner
	// events 只记录意外进入聊天通道的事件。
	events *recordingSink
}

func (backend *textEventSpyBackend) Run(ctx context.Context, request acpRunRequest) Result {
	request.Sink = &textEventSpySink{next: request.Sink, events: backend.events}
	return backend.acpRunner.Run(ctx, request)
}

// textEventSpySink 保留原接收器行为，同时记录所有进入聊天通道的事件。
type textEventSpySink struct {
	// next 是调用方原有的接收器，纯文本快速路径允许为空。
	next EventSink
	// events 保存本次观察到的统一事件。
	events *recordingSink
}

func (sink *textEventSpySink) Accept(ctx context.Context, event Event) error {
	if err := sink.events.Accept(ctx, event); err != nil {
		return err
	}
	if sink.next != nil {
		return sink.next.Accept(ctx, event)
	}
	return nil
}

// TestGenerateTextCollectsFinalResultWithoutCanonicalEvents 防止 Wiki 文本和思考片段进入聊天事件队列。
func TestGenerateTextCollectsFinalResultWithoutCanonicalEvents(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("CODEX_HOME", fixtureHome)
	if err := os.WriteFile(filepath.Join(fixtureHome, "auth.json"), []byte(`{"access_token":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	updates := []acp.SessionUpdate{}
	for range 600 {
		updates = append(updates,
			acp.UpdateAgentMessageText("正文\n"),
			acp.UpdateAgentThoughtText("内部推理"),
			acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Meta: map[string]any{"codex": map[string]any{"phase": "commentary"}}, Content: acp.TextBlock("过程说明")}},
			acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{Used: 100, Size: 200000}},
		)
	}
	agent := &textACPAgent{&fakeCodexACPAgent{
		selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, configOptions: textTestOptions("none"), scriptedUpdates: updates,
		scriptedResponse: &acp.PromptResponse{StopReason: acp.StopReasonMaxTokens, Usage: &acp.Usage{InputTokens: 12, OutputTokens: 7, CachedReadTokens: acp.Ptr(4), CachedWriteTokens: acp.Ptr(3), ThoughtTokens: acp.Ptr(2)}},
	}}
	backend := sdk.acp.(*acpBackend)
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
	spy := &textEventSpyBackend{acpRunner: backend, events: &recordingSink{}}
	sdk.acp = spy
	binary, _ := os.Executable()
	result := sdk.GenerateText(context.Background(), TextRequest{
		Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyDetectionForRun(t, sdk, RuntimeIDCodex, binary),
		Model: "model-a", Messages: []TextMessage{{Role: "user", Content: "source"}},
	})
	if result.Failure != nil || result.Text != strings.Repeat("正文\n", 600) || result.StopReason != "max_tokens" {
		t.Fatalf("final text lost or mixed with process output: result=%+v", result)
	}
	wantUsage := &TurnUsageData{InputTokens: 12, OutputTokens: 7, CacheReadTokens: 4, CacheCreationTokens: 3, ReasoningOutputTokens: 2}
	if !reflect.DeepEqual(result.Usage, wantUsage) {
		t.Fatalf("final usage=%+v, want %+v", result.Usage, wantUsage)
	}
	if len(spy.events.events) != 0 {
		t.Fatalf("text-only generation emitted %d canonical events", len(spy.events.events))
	}
	if !agent.closed || agent.closeSessionCalls != 1 {
		t.Fatalf("text process/session not reclaimed: closed=%v sessionCloses=%d", agent.closed, agent.closeSessionCalls)
	}
}

// TestTextACPUpdatesBypassDeliveryQueue 验证大量正文和过程更新不占用聊天队列，并继续作为超时活动。
func TestTextACPUpdatesBypassDeliveryQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector := &textCollector{}
	bridge := newACPBridge("session", "text", nil, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	bridge.textCollector = collector
	client := newACPClient()
	if err := client.register(bridge); err != nil {
		t.Fatal(err)
	}
	defer client.unregister(bridge)
	updates := []acp.SessionUpdate{
		acp.UpdateAgentMessageText("正文"),
		acp.UpdateAgentThoughtText("推理"),
		{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Meta: map[string]any{"codex": map[string]any{"phase": "commentary"}}, Content: acp.TextBlock("过程")}},
		{UsageUpdate: &acp.SessionUsageUpdate{Used: 200, Size: 200000}},
	}
	for range 600 {
		for _, update := range updates {
			if err := client.SessionUpdate(ctx, acp.SessionNotification{SessionId: "session", Update: update}); err != nil {
				t.Fatal(err)
			}
			if queued := len(bridge.updates); queued != 0 {
				t.Fatalf("pure text update entered chat delivery queue: queued=%d", queued)
			}
			select {
			case <-bridge.activity:
			default:
				t.Fatal("ignored process update did not refresh ACP activity")
			}
		}
	}
	if collector.text.String() != strings.Repeat("正文", 600) || collector.usage != nil || ctx.Err() != nil {
		t.Fatalf("text collector changed result semantics: text=%q usage=%+v err=%v", collector.text.String(), collector.usage, ctx.Err())
	}
}

// TestLegacyTextToolsKeepOnlyLastMessage 保留未迁移 CLI 的原生工具执行，同时只消费最后一条正文。
func TestLegacyTextToolsKeepOnlyLastMessage(t *testing.T) {
	collector := &textCollector{}
	first, final := "before-tool", "final"
	collector.collectUpdate(acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{MessageId: &first, Content: acp.TextBlock("working")}}, standardACPRuntimeBinding())
	collector.collectUpdate(acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{ToolCallId: "native-tool", Title: "read"}}, standardACPRuntimeBinding())
	collector.collectUpdate(acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{MessageId: &final, Content: acp.TextBlock("answer")}}, standardACPRuntimeBinding())
	collector.collectUpdate(acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{MessageId: &final, Content: acp.TextBlock(" continued")}}, standardACPRuntimeBinding())
	if collector.text.String() != "answer continued" {
		t.Fatalf("body=%q", collector.text.String())
	}
}

// TestTextACPRejectsToolsPlansAndInteractions 验证快速路径仍拒绝所有已观察到的工具与用户交互。

func TestTextReasoningNeverCallsLowDisabled(t *testing.T) {
	for _, test := range []struct {
		// options 是模型实际声明的推理选项。
		options []string
		// selected 是预期的真实选择。
		selected string
		// disabled 只在原生关闭选项存在时成立。
		disabled bool
	}{{[]string{"low", "high"}, "low", false}, {[]string{"off", "high"}, "off", true}, {[]string{"high"}, "high", false}, {[]string{"high", "medium"}, "medium", false}, {nil, "", false}} {
		selection, disabled, fallback := textReasoningSelection(textTestOptions(test.options...), codexACPReasoningConfigID, false)
		if selection != test.selected || disabled != test.disabled || (!disabled && fallback == "") {
			t.Fatalf("options=%v selection=%q disabled=%v fallback=%q", test.options, selection, disabled, fallback)
		}
	}
}

// textWorkBuddyACPAgent 提供原生只读模式，用真实 ACP 流程检查文本调用的启动边界。
type textWorkBuddyACPAgent struct {
	// textACPAgent 复用配置回执和脚本化会话更新，不启动真实 CLI。
	*textACPAgent
}

func (agent *textWorkBuddyACPAgent) NewSession(ctx context.Context, request acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	response, err := agent.textACPAgent.NewSession(ctx, request)
	response.Modes = &acp.SessionModeState{AvailableModes: []acp.SessionMode{{Id: "default", Name: "Default"}, {Id: "plan", Name: "Plan"}}}
	return response, err
}

func TestGenerateTextCancellationClosesSessionAndProcess(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("CODEX_HOME", fixtureHome)
	if err := os.WriteFile(filepath.Join(fixtureHome, "auth.json"), []byte(`{"access_token":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}

	sdk, _ := New(Config{})
	defer sdk.Close(context.Background())
	agent := &textACPAgent{&fakeCodexACPAgent{selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, configOptions: textTestOptions("none", "high"), blockPrompt: true, promptStarted: make(chan struct{})}}
	replaceACPAgentFactory(sdk.acp.(*acpBackend), RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
	binary, _ := os.Executable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan TextResult, 1)
	go func() {
		completed <- sdk.GenerateText(ctx, TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Messages: []TextMessage{{Role: "user", Content: "input"}}})
	}()
	select {
	case <-agent.promptStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not start")
	}
	cancel()
	select {
	case result := <-completed:
		if result.Failure == nil || result.Failure.Code != FailureCodeContextCanceled || result.Text != "" || agent.closeSessionCalls != 1 || !agent.closed {
			t.Fatalf("result=%+v closed=%v sessions=%d", result, agent.closed, agent.closeSessionCalls)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled model process was not reclaimed")
	}
}

func TestGenerateTextRejectsUnconfirmedReasoning(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("CODEX_HOME", fixtureHome)
	if err := os.WriteFile(filepath.Join(fixtureHome, "auth.json"), []byte(`{"access_token":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}

	sdk, _ := New(Config{})
	defer sdk.Close(context.Background())
	agent := &fakeCodexACPAgent{selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, configOptions: textTestOptions("none", "high"), simplePrompt: true}
	replaceACPAgentFactory(sdk.acp.(*acpBackend), RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
	binary, _ := os.Executable()
	result := sdk.GenerateText(context.Background(), TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Messages: []TextMessage{{Role: "user", Content: "input"}}})
	if result.Failure == nil || result.Failure.Code != FailureCodeProtocolIncompatible || result.ThinkingDisabled || result.ActualReasoning != "high" || agent.promptCalls != 0 {
		t.Fatalf("result=%+v promptCalls=%d", result, agent.promptCalls)
	}
}

// TestGenerateTextResolvesManagedOpenCodeModel 验证知识模型沿既有账号边界转换原生 provider/model 身份。
func TestGenerateTextResolvesManagedOpenCodeModel(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("CODEX_HOME", fixtureHome)
	if err := os.WriteFile(filepath.Join(fixtureHome, "auth.json"), []byte(`{"access_token":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}

	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	agent := &textACPAgent{&fakeCodexACPAgent{selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, configOptions: textTestOptions(), scriptedUpdates: []acp.SessionUpdate{acp.UpdateAgentMessageText("answer")}, scriptedResponse: &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}}}
	replaceACPAgentFactory(sdk.acp.(*acpBackend), RuntimeIDOpenCode, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
	binary, _ := os.Executable()
	result := sdk.GenerateText(context.Background(), TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDOpenCode, AccountID: "managed-account", Isolated: true}, Detection: readyDetectionForRun(t, sdk, RuntimeIDOpenCode, binary), Model: "model-a", Messages: []TextMessage{{Role: "user", Content: "input"}}})
	if result.Failure != nil || result.Text != "answer" || agent.selectedConfigs["model"] != "ally/model-a" {
		t.Fatalf("result=%+v native model=%q", result, agent.selectedConfigs["model"])
	}
}

// textProbeAgent 模拟原生会话拒绝权限模式配置。
type textProbeAgent struct {
	*fakeCodexACPAgent      // 提供会话与配置目录。
	rejectMode         bool // 决定临时会话是否拒绝配置。
	modeCalls          int  // 记录实际原生权限配置次数。
}

func (agent *textProbeAgent) SetSessionMode(ctx context.Context, request acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	agent.modeCalls++
	if agent.rejectMode {
		return acp.SetSessionModeResponse{}, acp.NewMethodNotFound("session/set_mode")
	}
	return agent.fakeCodexACPAgent.SetSessionMode(ctx, request)
}

func TestLegacyTextSessionVerifiesNativeConfigurationBeforePrompt(t *testing.T) {
	// 受管账号必须显式提供 ConfigHome；宿主认证会被 Isolated 剥离，不能借用开发机账号。
	systemHome := t.TempDir()
	t.Setenv("HOME", systemHome)
	t.Setenv("USERPROFILE", systemHome)
	fixtureHome := t.TempDir()
	t.Setenv("CODEX_HOME", fixtureHome)
	if err := os.WriteFile(filepath.Join(fixtureHome, "auth.json"), []byte(`{"access_token":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}

	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	agent := &textProbeAgent{fakeCodexACPAgent: testCodexACPAgentWithCatalog(), rejectMode: true}
	replaceACPAgentFactory(sdk.acp.(*acpBackend), RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
	binary, _ := os.Executable()
	detection := readyDetectionForRun(t, sdk, RuntimeIDCodex, binary)
	request := TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex, AccountID: "managed-account", Isolated: true}, Detection: detection, Messages: []TextMessage{{Role: "user", Content: "source"}}}
	request.Target.ConfigHome = fixtureHome
	result := sdk.GenerateText(context.Background(), request)
	if result.Failure == nil || result.Failure.Code != FailureCodeProtocolIncompatible || agent.newCalls != 1 || agent.modeCalls != 1 || agent.promptCalls != 0 || agent.closeSessionCalls != 1 || !agent.closed {
		t.Fatalf("unverified managed session bypassed configuration: failure=%+v new=%d modes=%d prompt=%d sessionCloses=%d closed=%v", result.Failure, agent.newCalls, agent.modeCalls, agent.promptCalls, agent.closeSessionCalls, agent.closed)
	}
}

func TestGenerateTextRejectsUnsupportedExplicitReasoning(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("CODEX_HOME", fixtureHome)
	if err := os.WriteFile(filepath.Join(fixtureHome, "auth.json"), []byte(`{"access_token":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	agent := &textACPAgent{&fakeCodexACPAgent{selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, configOptions: textTestOptions("low", "high")}}
	replaceACPAgentFactory(sdk.acp.(*acpBackend), RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
	binary, _ := os.Executable()
	result := sdk.GenerateText(context.Background(), TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Reasoning: "unsupported", Messages: []TextMessage{{Role: "user", Content: "source"}}})
	if result.Failure == nil || result.Text != "" || agent.promptCalls != 0 {
		t.Fatalf("result=%+v prompts=%d", result, agent.promptCalls)
	}
}
