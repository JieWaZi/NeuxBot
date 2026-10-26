package runtimes

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestClaudeACPBackendMapsRuntimeContract 验证 Claude 只通过共享 ACP Backend 执行。
func TestClaudeACPBackendMapsRuntimeContract(t *testing.T) {
	t.Parallel()

	backend := newACPBackend()
	fakeAgent := &fakeCodexACPAgent{
		sessionID:       "claude-acp-session",
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		supportsSSE:     true,
	}
	var adapterConfig acpAdapterConfig
	replaceACPAgentFactory(backend, RuntimeIDClaude, func(
		_ context.Context,
		config acpAdapterConfig,
	) (acp.Agent, error) {
		adapterConfig = config
		return fakeAgent, nil
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })

	defaults, err := normalizeDefaults(Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	additional := t.TempDir()
	imagePath := workspace + "/image.png"
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	request := acpRunRequest{
		InvocationID: "invocation-claude-acp",
		Prepared: preparedRuntime{
			definition: runtimeDefinition{
				id:            RuntimeIDClaude,
				baseRuntimeID: RuntimeIDClaude,
				defaultModel:  "default",
				prefixArgs:    []string{"--setting-sources", "user"},
			},
			detection: DetectionResult{
				Runtime:        RuntimeSummary{ID: RuntimeIDClaude, BaseRuntimeID: RuntimeIDClaude},
				ResolvedBinary: "/fake/claude",
			},
			environment: environmentSnapshot{
				values:      map[string]string{"CLAUDE_CONFIG_DIR": "/custom/claude-home"},
				names:       map[string]string{"CLAUDE_CONFIG_DIR": "CLAUDE_CONFIG_DIR"},
				fingerprint: "claude-environment",
			},
			defaults: defaults,
		},
		Request: RunRequest{
			Target: RuntimeTarget{RuntimeID: RuntimeIDClaude},
			Workspace: Workspace{
				Path:                          workspace,
				Managed:                       true,
				AdditionalWritableDirectories: []string{additional},
			},
			Message: Message{
				Text: "检查实现",
				Attachments: []Attachment{{
					Name:        "image.png",
					Path:        "image.png",
					MediaType:   "image/png",
					RuntimePath: imagePath,
				}},
			},
			Connectors: []Connector{
				{
					ID: "stdio", Name: "STDIO", RuntimeKey: "ally_stdio",
					Transport: ConnectorTransportStdio, Command: "mcp-stdio",
				},
				{
					ID: "http", Name: "HTTP", RuntimeKey: "ally_http", Selected: true,
					Transport: ConnectorTransportStreamableHTTP, URL: "https://mcp.example/http",
				},
				{
					ID: "sse", Name: "SSE", RuntimeKey: "ally_sse",
					Transport: ConnectorTransportSSE, URL: "https://mcp.example/sse",
				},
			},
			Session: SessionDirective{Mode: SessionModeNew},
			Policy: RunPolicy{
				Model: "opus", Reasoning: "high",
				Execution: ExecutionPolicy{
					PermissionLevel: PermissionLevelAuto,
				},
			},
		},
		Prompt: "检查实现",
		RuntimeFiles: runtimeFiles{selectedSkills: []skillSelection{{
			name: "review", path: workspace + "/.claude/skills/review/SKILL.md",
		}}},
		Sink: sink,
	}

	result := backend.Run(context.Background(), request)
	if result.Failure != nil || result.Session.OutputID != "claude-acp-session" {
		t.Fatalf("Result = %#v", result)
	}
	if adapterConfig.Executable != "/fake/claude" ||
		!reflect.DeepEqual(adapterConfig.PrefixArgs, []string{"--setting-sources", "user"}) ||
		!reflect.DeepEqual(adapterConfig.Environment, []string{"CLAUDE_CONFIG_DIR=/custom/claude-home"}) {
		t.Fatalf("Claude Adapter Config = %#v", adapterConfig)
	}
	if fakeAgent.selectedMode != "auto" ||
		fakeAgent.selectedConfigs[claudeACPModelConfigID] != "opus" ||
		fakeAgent.selectedConfigs[claudeACPReasoningConfigID] != "high" {
		t.Fatalf(
			"mode=%q configs=%#v",
			fakeAgent.selectedMode,
			fakeAgent.selectedConfigs,
		)
	}
	newRequest := fakeAgent.newSessionRequest
	if newRequest.Cwd != workspace ||
		!reflect.DeepEqual(newRequest.AdditionalDirectories, []string{additional}) ||
		len(newRequest.McpServers) != 4 || newRequest.McpServers[0].Stdio == nil ||
		newRequest.McpServers[1].Http == nil || newRequest.McpServers[2].Sse == nil || newRequest.McpServers[3].Http == nil || newRequest.McpServers[3].Http.Meta["acp-go/user-input"] != true {
		t.Fatalf("session/new = %#v", newRequest)
	}
	promptRequest := fakeAgent.promptRequest
	if len(promptRequest.Prompt) != 2 || promptRequest.Prompt[0].Text == nil ||
		!strings.HasPrefix(promptRequest.Prompt[0].Text.Text, "/review\n\n检查实现") ||
		!strings.Contains(promptRequest.Prompt[0].Text.Text, "ally_http (HTTP)") ||
		promptRequest.Prompt[1].Image == nil {
		t.Fatalf("session/prompt = %#v", promptRequest)
	}
	if types := sink.eventTypes(); len(types) == 0 ||
		types[0] != EventTypeInvocationStarted ||
		!containsEventType(types, EventTypeTextDelta) ||
		containsEventType(types, EventTypeCommentaryDelta) {
		t.Fatalf("Claude ACP event types = %#v", types)
	}
}

// TestClaudeACPDetectOptionsUsesAdapterConfigIDs 验证 Claude model/effort 目录来自 ACP。
func TestClaudeACPDetectOptionsUsesAdapterConfigIDs(t *testing.T) {
	t.Parallel()

	models := []acp.SessionConfigSelectOption{
		{Value: "sonnet", Name: "Sonnet"},
		{Value: "opus", Name: "Opus"},
	}
	sonnetOptions := testClaudeACPConfigOptions(
		"sonnet",
		models,
		[]acp.SessionConfigSelectOption{{Value: "medium", Name: "Medium"}},
	)
	opusOptions := testClaudeACPConfigOptions(
		"opus",
		models,
		[]acp.SessionConfigSelectOption{{Value: "high", Name: "High"}},
	)
	fakeAgent := &fakeCodexACPAgent{
		sessionID:       "claude-option-session",
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		configOptions:   sonnetOptions,
		configOptionsByModel: map[acp.SessionConfigValueId][]acp.SessionConfigOption{
			"opus": opusOptions,
		},
	}
	backend := newACPBackend()
	replaceACPAgentFactory(backend, RuntimeIDClaude, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return fakeAgent, nil
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })

	catalog, err := backend.DetectOptions(context.Background(), acpOptionsRequest{
		RuntimeID: RuntimeIDClaude,
		Prepared: preparedRuntime{
			definition:  runtimeDefinition{id: RuntimeIDClaude, baseRuntimeID: RuntimeIDClaude},
			detection:   DetectionResult{ResolvedBinary: "/fake/claude"},
			environment: environmentSnapshot{fingerprint: "claude-options"},
		},
		WorkingDirectory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("DetectOptions() error = %v", err)
	}
	if len(catalog.Models) != 2 || catalog.Models[0].ID != "sonnet" ||
		len(catalog.Models[0].ReasoningOptions) != 1 ||
		catalog.Models[0].ReasoningOptions[0].ID != "medium" ||
		len(catalog.Models[1].ReasoningOptions) != 1 ||
		catalog.Models[1].ReasoningOptions[0].ID != "high" {
		t.Fatalf("catalog = %#v", catalog)
	}
}

// TestACPProjectionMapsClaudeMetadataAndPlan 验证 upstream 元数据与标准 Plan 投影。
func TestACPProjectionMapsClaudeMetadataAndPlan(t *testing.T) {
	t.Parallel()

	bridge := newACPBridge(
		"claude-session",
		"invocation",
		nil,
		Workspace{},
		ExecutionPolicy{},
		claudeACPRuntimeBinding(),
		0,
	)
	toolEvents, err := bridge.mapNotification(acp.SessionNotification{
		SessionId: "claude-session",
		Update: acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: "skill-1",
			Title:      "Load skill: review",
			Kind:       acp.ToolKindOther,
			Status:     acp.ToolCallStatusInProgress,
			Meta: map[string]any{"claudeCode": map[string]any{
				"toolName": "Skill", "skill": "review", "skillPath": "/workspace/.claude/skills/review/SKILL.md",
			}},
		}},
	})
	if err != nil || len(toolEvents) != 1 {
		t.Fatalf("tool events = %#v, %v", toolEvents, err)
	}
	var toolPayload map[string]any
	if err := jsonUnmarshalEvent(toolEvents[0], &toolPayload); err != nil {
		t.Fatal(err)
	}
	if toolPayload["name"] != "skill" || toolPayload["_meta"] == nil {
		t.Fatalf("tool payload = %#v", toolPayload)
	}

	planEvents, err := bridge.mapNotification(acp.SessionNotification{
		SessionId: "claude-session",
		Update: acp.UpdatePlan(
			acp.PlanEntry{Content: "Inspect", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusCompleted},
			acp.PlanEntry{Content: "Implement", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusInProgress},
		),
	})
	if err != nil || len(planEvents) != 1 || planEvents[0].Type != EventTypeToolUse {
		t.Fatalf("plan events = %#v, %v", planEvents, err)
	}
	completed, err := bridge.mapNotification(acp.SessionNotification{
		SessionId: "claude-session",
		Update: acp.UpdatePlan(
			acp.PlanEntry{Content: "Inspect", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusCompleted},
			acp.PlanEntry{Content: "Implement", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusCompleted},
		),
	})
	if err != nil || len(completed) != 2 || completed[1].Type != EventTypeToolResult {
		t.Fatalf("completed plan events = %#v, %v", completed, err)
	}
}

// TestACPProjectionPrefersStandardEditKindOverClaudeToolName 验证标准 ACP 文件语义不会被原生工具名覆盖。
func TestACPProjectionPrefersStandardEditKindOverClaudeToolName(t *testing.T) {
	t.Parallel()

	payload, err := acpToolPayload(acp.SessionUpdateToolCall{
		ToolCallId: "write-1",
		Title:      "Write /workspace/acp-smoke-output.txt",
		Kind:       acp.ToolKindEdit,
		Status:     acp.ToolCallStatusInProgress,
		Locations: []acp.ToolCallLocation{{
			Path: "/workspace/acp-smoke-output.txt",
		}},
		Meta: map[string]any{"claudeCode": map[string]any{
			"toolName": "Write",
		}},
	}, claudeACPRuntimeBinding())
	if err != nil {
		t.Fatal(err)
	}
	if payload["name"] != "file_modify" {
		t.Fatalf("tool payload = %#v", payload)
	}
}

// testClaudeACPConfigOptions 创建 model 与当前模型 effort 配置。
func testClaudeACPConfigOptions(
	currentModel string,
	models []acp.SessionConfigSelectOption,
	reasoning []acp.SessionConfigSelectOption,
) []acp.SessionConfigOption {
	modelValues := acp.SessionConfigSelectOptionsUngrouped(models)
	reasoningValues := acp.SessionConfigSelectOptionsUngrouped(reasoning)
	return []acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{
			Id: claudeACPModelConfigID, Name: "Model",
			CurrentValue: acp.SessionConfigValueId(currentModel),
			Options:      acp.SessionConfigSelectOptions{Ungrouped: &modelValues},
			Type:         "select",
		}},
		{Select: &acp.SessionConfigOptionSelect{
			Id: claudeACPReasoningConfigID, Name: "Effort",
			CurrentValue: reasoningValues[0].Value,
			Options:      acp.SessionConfigSelectOptions{Ungrouped: &reasoningValues},
			Type:         "select",
		}},
	}
}

// jsonUnmarshalEvent 解码测试事件载荷。
func jsonUnmarshalEvent(event Event, target any) error {
	return json.Unmarshal(event.Data, target)
}
