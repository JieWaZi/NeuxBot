package runtimes

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/coder/acp-go-sdk"
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
	var configs []acpAdapterConfig
	backend := sdk.acp.(*acpBackend)
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(_ context.Context, config acpAdapterConfig) (acp.Agent, error) {
		agent := &textACPAgent{&fakeCodexACPAgent{selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, configOptions: textTestOptions("none", "high"), scriptedUpdates: []acp.SessionUpdate{acp.UpdateAgentMessageText("answer")}, scriptedResponse: &acp.PromptResponse{StopReason: acp.StopReasonMaxTokens, Usage: &acp.Usage{InputTokens: 12, OutputTokens: 7}}}}
		agents = append(agents, agent)
		configs = append(configs, config)
		return agent, nil
	})
	binary, _ := os.Executable()
	request := TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyTextDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Messages: []TextMessage{{Role: "system", Content: "system rules"}, {Role: "user", Content: "first source"}}, MaxTokens: 32768, Temperature: .3}
	first := sdk.GenerateText(context.Background(), request)
	if first.Failure != nil || first.Text != "answer" || first.StopReason != "max_tokens" || first.Usage == nil || first.Usage.InputTokens != 12 || first.Usage.OutputTokens != 7 || first.ActualReasoning != "none" || !first.ThinkingDisabled || first.ToolsDisabled {
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
	if !strings.Contains(strings.Join(configs[0].PrefixArgs, " "), "web_search") {
		t.Fatal("Codex web search was not disabled")
	}
}

func TestGenerateTextRejectsToolsAndDiscardsPartialText(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("CODEX_HOME", fixtureHome)
	if err := os.WriteFile(filepath.Join(fixtureHome, "auth.json"), []byte(`{"access_token":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}

	sdk, _ := New(Config{})
	defer sdk.Close(context.Background())
	backend := sdk.acp.(*acpBackend)
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
		return &textACPAgent{&fakeCodexACPAgent{selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, configOptions: textTestOptions("low", "high"), scriptedUpdates: []acp.SessionUpdate{acp.UpdateAgentMessageText("partial"), acp.StartToolCall("tool", "Read", acp.WithStartKind(acp.ToolKindRead))}, scriptedResponse: &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}}}, nil
	})
	binary, _ := os.Executable()
	result := sdk.GenerateText(context.Background(), TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyTextDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Messages: []TextMessage{{Role: "user", Content: "source"}}})
	if result.Failure == nil || result.Text != "" || result.ThinkingDisabled || result.ActualReasoning != "low" || result.ReasoningFallback == "" {
		t.Fatalf("result=%+v", result)
	}
}

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

func TestTextClaudeLaunchDisablesToolsAndPersistence(t *testing.T) {
	config := acpAdapterConfig{PrefixArgs: []string{"--existing"}}
	actual, disabled := textAdapterConfig(config, RuntimeIDClaude)
	want := []string{"--existing", "--tools", "", "--strict-mcp-config", "--disable-slash-commands", "--no-session-persistence", "--settings", `{"disableAllHooks":true}`}
	if !disabled || !reflect.DeepEqual(actual.PrefixArgs, want) {
		t.Fatalf("args=%q disabled=%v", actual.PrefixArgs, disabled)
	}
}

func TestTextPermissionNeverInvokesHostApproval(t *testing.T) {
	rejected := false
	client := newACPClient(codexACPRuntimeBinding())
	bridge := newACPBridge("session", "model", nil, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), time.Second)
	bridge.rejectTextInteraction = func() { rejected = true }
	bridge.approvalHandler = approvalHandlerFunc(func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
		t.Fatal("model call invoked host approval")
		return ApprovalDecision{}, nil
	})
	if err := client.register(bridge); err != nil {
		t.Fatal(err)
	}
	response, err := client.RequestPermission(context.Background(), acp.RequestPermissionRequest{SessionId: "session"})
	if err != nil || !rejected || response.Outcome.Cancelled == nil {
		t.Fatalf("response=%+v err=%v rejected=%v", response, err, rejected)
	}
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
		completed <- sdk.GenerateText(ctx, TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyTextDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Messages: []TextMessage{{Role: "user", Content: "input"}}})
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
	result := sdk.GenerateText(context.Background(), TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyTextDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Messages: []TextMessage{{Role: "user", Content: "input"}}})
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
	result := sdk.GenerateText(context.Background(), TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDOpenCode, AccountID: "managed-account", Isolated: true}, Detection: readyTextDetectionForRun(t, sdk, RuntimeIDOpenCode, binary), Model: "model-a", Messages: []TextMessage{{Role: "user", Content: "input"}}})
	if result.Failure != nil || result.Text != "answer" || agent.selectedConfigs["model"] != "ally/model-a" {
		t.Fatalf("result=%+v native model=%q", result, agent.selectedConfigs["model"])
	}
}

func TestGenerateTextRejectsUnsupportedCapabilityBeforeStartingAgent(t *testing.T) {
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
	created := false
	replaceACPAgentFactory(sdk.acp.(*acpBackend), RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
		created = true
		return &textACPAgent{testCodexACPAgentWithCatalog()}, nil
	})
	binary, _ := os.Executable()
	detection := readyTextDetectionForRun(t, sdk, RuntimeIDCodex, binary)
	detection.Capabilities[CapabilityIsolatedTextGeneration] = false
	result := sdk.GenerateText(context.Background(), TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: detection, Messages: []TextMessage{{Role: "user", Content: "source"}}})
	if result.Failure == nil || result.Failure.Code != FailureCodePolicyUnsupported || created {
		t.Fatalf("unverified text capability started runtime: result=%+v created=%v", result, created)
	}
}

// textProbeAgent 模拟真实会话拒绝隔离模式，不能只凭 binding 声明能力。
type textProbeAgent struct {
	*fakeCodexACPAgent      // 提供会话与配置目录。
	rejectMode         bool // 决定临时会话是否拒绝配置。
	cancelMode         bool // 模拟探测配置被取消，不能据此认定能力不支持。
	modeCalls          int  // 记录真正进入会话隔离配置的次数。
}

func (agent *textProbeAgent) SetSessionMode(ctx context.Context, request acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	agent.modeCalls++
	if agent.cancelMode {
		return acp.SetSessionModeResponse{}, &acp.RequestError{Code: -32800, Message: "probe cancelled"}
	}
	if agent.rejectMode {
		return acp.SetSessionModeResponse{}, acp.NewMethodNotFound("session/set_mode")
	}
	return agent.fakeCodexACPAgent.SetSessionMode(ctx, request)
}
func TestTextCapabilityRequiresAcceptedSessionIsolation(t *testing.T) {
	for _, test := range []struct {
		name        string // 场景名称。
		unsupported bool   // binding无法提供隔离模式。
		reject      bool   // 真实Agent拒绝模式。
		want        bool   // 是否应提供生成能力。
		unknown     bool   // 取消只保留未知，避免把临时中断保存为不支持。
	}{{name: "accepted", want: true}, {name: "binding unsupported", unsupported: true}, {name: "session rejected", reject: true}, {name: "session cancelled", unknown: true}} {
		t.Run(test.name, func(t *testing.T) {
			sdk, err := New(Config{})
			if err != nil {
				t.Fatal(err)
			}
			defer sdk.Close(context.Background())
			backend := sdk.acp.(*acpBackend)
			agent := &textProbeAgent{fakeCodexACPAgent: testCodexACPAgentWithCatalog(), rejectMode: test.reject, cancelMode: test.unknown}
			replaceACPAgentFactory(backend, RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
			if test.unsupported {
				binding := backend.bindings[RuntimeIDCodex]
				binding.modeID = func(ExecutionPolicy, *acp.SessionModeState) (acp.SessionModeId, error) {
					return "", status_error.RuntimeProtocolIncompatible
				}
				backend.bindings[RuntimeIDCodex] = binding
			}
			binary, _ := os.Executable()
			result := sdk.Detect(context.Background(), DetectRequest{RuntimeID: RuntimeIDCodex, LaunchOverride: LaunchOverride{Binary: binary}, HomeDirectory: t.TempDir()})
			_, known := result.Capabilities[CapabilityIsolatedTextGeneration]
			if result.State != DetectionStateReady || result.Capabilities.Has(Capability("isolated_text_generation")) != test.want || known == test.unknown {
				t.Fatalf("detection=%+v want text=%v", result, test.want)
			}
		})
	}
}

// readyTextDetectionForRun 提供已经确认隔离配置能力的文本调用测试快照。
func readyTextDetectionForRun(t *testing.T, sdk *SDK, runtimeID, binary string) DetectionResult {
	detection := readyDetectionForRun(t, sdk, runtimeID, binary)
	if detection.Capabilities == nil {
		detection.Capabilities = CapabilitySet{}
	}
	detection.Capabilities[CapabilityIsolatedTextGeneration] = true
	return detection
}

func TestUnknownTextCapabilityVerifiesManagedSessionBeforePrompt(t *testing.T) {
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
	detection := readyTextDetectionForRun(t, sdk, RuntimeIDCodex, binary)
	delete(detection.Capabilities, CapabilityIsolatedTextGeneration)
	request := TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex, AccountID: "managed-account", Isolated: true}, Detection: detection, Messages: []TextMessage{{Role: "user", Content: "source"}}}
	// 系统 HOME 为空时，缺失受管配置必须拒绝，不能借用宿主 CODEX_HOME 中的 fixture。
	missingHome := sdk.GenerateText(context.Background(), request)
	if missingHome.Failure == nil || missingHome.Failure.Stage != "text_isolation" || agent.newCalls != 0 || agent.promptCalls != 0 {
		t.Fatalf("isolated target borrowed host authentication: failure=%+v new=%d prompt=%d", missingHome.Failure, agent.newCalls, agent.promptCalls)
	}
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
	result := sdk.GenerateText(context.Background(), TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyTextDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Reasoning: "unsupported", Messages: []TextMessage{{Role: "user", Content: "source"}}})
	if result.Failure == nil || result.Text != "" || agent.promptCalls != 0 {
		t.Fatalf("result=%+v prompts=%d", result, agent.promptCalls)
	}
}
