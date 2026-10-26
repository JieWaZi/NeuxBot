package runtimes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	acp "github.com/JieWaZi/acp-go/sdk"
)

type approvalHandlerFunc func(context.Context, ApprovalRequest) (ApprovalDecision, error)

func (f approvalHandlerFunc) Request(
	ctx context.Context,
	request ApprovalRequest,
) (ApprovalDecision, error) {
	return f(ctx, request)
}

// acpRuntimeMatrixCase 保存一种 Runtime 的通用 ACP 验收绑定。
type acpRuntimeMatrixCase struct {
	// name 是子测试展示名称。
	name string
	// runtimeID 是内置 Runtime 标识。
	runtimeID string
	// binding 是该 Runtime 的薄 ACP Binding。
	binding acpRuntimeBinding
}

// acpRuntimeMatrix 返回 Codex 与 Claude 共用的产品语义验收集合。
func acpRuntimeMatrix() []acpRuntimeMatrixCase {
	return []acpRuntimeMatrixCase{
		{name: "codex", runtimeID: RuntimeIDCodex, binding: codexACPRuntimeBinding()},
		{name: "claude", runtimeID: RuntimeIDClaude, binding: claudeACPRuntimeBinding()},
	}
}

// TestACPRuntimeProductProjectionMatrix 验证两种 Runtime 经 Runtime.Run 投影为同一组产品能力。
func TestACPRuntimeProductProjectionMatrix(t *testing.T) {
	t.Parallel()

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range acpRuntimeMatrix() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sdk, newErr := New(Config{})
			if newErr != nil {
				t.Fatal(newErr)
			}
			backend, ok := sdk.acp.(*acpBackend)
			if !ok {
				t.Fatalf("ACP backend = %T", sdk.acp)
			}
			commentaryMeta := map[string]any{"codex": map[string]any{"phase": "commentary"}}
			updates := []acp.SessionUpdate{
				{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.TextBlock("过程说明"), Meta: commentaryMeta,
				}},
				acp.UpdateAgentMessageText("正文"),
				acp.UpdateAgentThoughtText("推理"),
				acp.StartToolCall(
					"mcp-1",
					"MCP call",
					acp.WithStartKind(acp.ToolKindExecute),
					acp.WithStartStatus(acp.ToolCallStatusInProgress),
					acp.WithStartRawInput(map[string]any{
						"server": "ally_memory", "tool": "memory_search",
					}),
				),
				acp.UpdateToolCall("mcp-1", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)),
				acp.StartToolCall(
					"skill-1",
					"Load skill: review",
					acp.WithStartKind(acp.ToolKindOther),
					acp.WithStartStatus(acp.ToolCallStatusInProgress),
					acp.WithStartRawInput(map[string]any{"skill": "review"}),
				),
				acp.UpdateToolCall("skill-1", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)),
				acp.StartEditToolCall(
					"edit-1",
					"Edit file",
					"/workspace/main.go",
					map[string]any{"replacement": "new"},
				),
				acp.UpdateToolCall(
					"edit-1",
					acp.WithUpdateKind(acp.ToolKindEdit),
					acp.WithUpdateStatus(acp.ToolCallStatusCompleted),
				),
				acp.UpdatePlan(acp.PlanEntry{
					Content: "实现", Priority: acp.PlanEntryPriorityMedium,
					Status: acp.PlanEntryStatusInProgress,
				}),
				acp.UpdatePlan(acp.PlanEntry{
					Content: "实现", Priority: acp.PlanEntryPriorityMedium,
					Status: acp.PlanEntryStatusCompleted,
				}),
				{UsageUpdate: &acp.SessionUsageUpdate{Used: 120, Size: 200000}},
			}
			response := acp.PromptResponse{
				StopReason: acp.StopReasonEndTurn,
				Usage: &acp.Usage{
					InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
				},
			}
			agent := testCodexACPAgentWithCatalog()
			agent.sessionID = acp.SessionId(test.runtimeID + "-projection-session")
			agent.supportsSSE = test.runtimeID == RuntimeIDClaude
			agent.scriptedUpdates = updates
			agent.scriptedResponse = &response
			replaceACPAgentFactory(backend, test.runtimeID, func(
				context.Context,
				acpAdapterConfig,
			) (acp.Agent, error) {
				return agent, nil
			})
			t.Cleanup(func() {
				if closeErr := sdk.Close(context.Background()); closeErr != nil {
					t.Errorf("Close() error = %v", closeErr)
				}
			})

			sink := &recordingSink{}
			result := sdk.Run(context.Background(), RunRequest{
				Target:    RuntimeTarget{RuntimeID: test.runtimeID, Binary: testBinary},
				Detection: readyDetectionForRun(t, sdk, test.runtimeID, testBinary),
				Workspace: Workspace{Path: t.TempDir(), Managed: true},
				Message:   Message{Text: "检查产品投影"},
				Session:   SessionDirective{Mode: SessionModeNew},
				Policy: RunPolicy{Execution: ExecutionPolicy{
					PermissionLevel: PermissionLevelAuto,
				}},
			}, sink)
			if result.Failure != nil || result.Session.OutputID == "" {
				t.Fatalf("Runtime.Run result = %#v", result)
			}
			sink.mutex.Lock()
			events := append([]Event(nil), sink.events...)
			sink.mutex.Unlock()
			if len(events) == 0 || events[0].Type != EventTypeInvocationStarted {
				t.Fatalf("events = %#v", events)
			}
			assertACPProjectionCapabilities(t, events)
			if !result.SideEffects.VisibleOutput || !result.SideEffects.ReasoningOutput ||
				!result.SideEffects.ToolUse || !result.SideEffects.FileWrite {
				t.Fatalf("side effects = %#v", result.SideEffects)
			}
		})
	}
}

// TestACPRuntimeFailureProjectionMatrix 验证全部五种 Runtime 的标准与 Provider 失败保持同一产品分类。
func TestACPRuntimeFailureProjectionMatrix(t *testing.T) {
	t.Parallel()

	failures := []struct {
		// name 是失败场景名称。
		name string
		// err 是 ACP Agent 在 Prompt 边界返回的错误。
		err error
		// code 是 NeuxBot 对外稳定失败码。
		code FailureCode
	}{
		{name: "authentication", err: acp.NewAuthRequired(nil), code: FailureCodeAuthRequired},
		{name: "quota exhausted", err: acp.NewInternalError(map[string]any{"errorKind": "billing_error"}), code: FailureCodeQuotaExhausted},
		{name: "rate limit", err: acp.NewInternalError(map[string]any{"errorKind": "rate_limit"}), code: FailureCodeRateLimited},
		{name: "upstream", err: acp.NewInternalError(map[string]any{"errorKind": "overloaded"}), code: FailureCodeUpstreamUnavailable},
		{name: "network", err: acp.NewInternalError(map[string]any{"errorKind": "network_error"}), code: constants.RuntimeFailureCodeNetworkError},
		{name: "timeout", err: acp.NewInternalError(map[string]any{"errorKind": "timeout"}), code: constants.RuntimeFailureCodeRequestTimeout},
		{name: "context limit", err: acp.NewInternalError(map[string]any{"errorKind": "context_window_exceeded"}), code: constants.RuntimeFailureCodeContextTooLarge},
		{name: "unknown", err: acp.NewInternalError(map[string]any{"errorKind": "unexpected_provider_error"}), code: constants.RuntimeFailureCodeExecutionFailed},
		{name: "native process exit", err: errors.New("native acp process exited: exit status 1"), code: FailureCodeProcessExited},
		{name: "pi process exit", err: errors.New("pi process exited: exit status 1"), code: FailureCodeProcessExited},
		{name: "cursor process exit", err: errors.New("cursor interactive process exited: exit status 1"), code: FailureCodeProcessExited},
		{name: "protocol", err: acp.NewMethodNotFound("session/prompt"), code: FailureCodeProtocolIncompatible},
		{name: "oversized frame", err: errors.New("claude frame too large"), code: FailureCodeFrameTooLarge},
		{name: "process exit", err: errors.New("codex app-server exited with code 7"), code: FailureCodeProcessExited},
	}
	runtimeCases := append(acpRuntimeMatrix(),
		acpRuntimeMatrixCase{name: "cursor", runtimeID: RuntimeIDCursor, binding: cursorACPRuntimeBinding()},
		acpRuntimeMatrixCase{name: "kimi", runtimeID: RuntimeIDKimi, binding: kimiACPRuntimeBinding()},
		acpRuntimeMatrixCase{name: "pi", runtimeID: RuntimeIDPi, binding: piACPRuntimeBinding()},
	)
	for _, runtimeCase := range runtimeCases {
		runtimeCase := runtimeCase
		for _, failureCase := range failures {
			failureCase := failureCase
			t.Run(runtimeCase.name+"/"+failureCase.name, func(t *testing.T) {
				t.Parallel()
				backend := newACPBackend()
				agent := &fakeCodexACPAgent{
					sessionID:       acp.SessionId(runtimeCase.runtimeID + "-failure-session"),
					selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
					promptError:     failureCase.err,
				}
				replaceACPAgentFactory(backend, runtimeCase.runtimeID, func(
					context.Context,
					acpAdapterConfig,
				) (acp.Agent, error) {
					return agent, nil
				})
				t.Cleanup(func() { _ = backend.Close(context.Background()) })

				result := backend.Run(context.Background(), matrixACPRunRequest(
					t,
					runtimeCase.runtimeID,
					SessionDirective{Mode: SessionModeNew},
				))
				if result.Failure == nil || result.Failure.Code != failureCase.code {
					t.Fatalf("result = %#v, want failure %q", result, failureCase.code)
				}
			})
		}
	}
}

// TestACPProjectionKeepsToolProgressWithoutContent 验证 status/rawOutput 进度不会被共享投影吞掉。
func TestACPProjectionKeepsToolProgressWithoutContent(t *testing.T) {
	t.Parallel()
	bridge := newACPBridge(
		"session",
		"invocation",
		nil,
		Workspace{},
		ExecutionPolicy{},
		claudeACPRuntimeBinding(),
		0,
	)
	status := acp.ToolCallStatusInProgress
	events, err := bridge.mapNotification(acp.SessionNotification{
		SessionId: "session",
		Update: acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: "tool-1",
			Status:     &status,
			RawOutput:  map[string]any{"elapsedTimeSeconds": 1.5},
		}},
	})
	if err != nil || len(events) != 1 || events[0].Type != EventTypeToolUpdate {
		t.Fatalf("events = %#v, err = %v", events, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["toolCallId"] != "tool-1" || payload["rawOutput"] == nil {
		t.Fatalf("payload = %#v", payload)
	}
}

// TestACPRuntimePolicyMatrix 验证两种 Runtime 使用同一组三档产品权限。
func TestACPRuntimePolicyMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// name 是权限映射场景名称。
		name string
		// binding 是目标 Runtime 的薄 ACP Binding。
		binding acpRuntimeBinding
		// expected 保存三个产品权限等级对应的 ACP Mode。
		expected map[PermissionLevel]acp.SessionModeId
	}{
		{
			name:    "codex",
			binding: codexACPRuntimeBinding(),
			expected: map[PermissionLevel]acp.SessionModeId{
				PermissionLevelDefault:    "read-only",
				PermissionLevelAuto:       "agent",
				PermissionLevelFullAccess: "agent-full-access",
			},
		},
		{
			name:    "claude",
			binding: claudeACPRuntimeBinding(),
			expected: map[PermissionLevel]acp.SessionModeId{
				PermissionLevelDefault:    "default",
				PermissionLevelAuto:       "auto",
				PermissionLevelFullAccess: "bypassPermissions",
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for level, expected := range test.expected {
				actual, err := test.binding.modeID(ExecutionPolicy{
					PermissionLevel: level,
				}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if actual != expected {
					t.Fatalf("mode(%q) = %q, want %q", level, actual, expected)
				}
			}
		})
	}
}

func TestACPWorkspaceReadOnlyPolicyOverridesWritablePermissionMode(t *testing.T) {
	t.Parallel()
	policy := ExecutionPolicy{
		PermissionLevel:   PermissionLevelFullAccess,
		WorkspaceReadOnly: true,
	}
	if actual, err := codexACPRuntimeBinding().modeID(policy, nil); err != nil || actual != "read-only" {
		t.Fatalf("Codex read-only mode = %q, want read-only", actual)
	}
	if actual, err := claudeACPRuntimeBinding().modeID(policy, nil); err != nil || actual != "default" {
		t.Fatalf("Claude read-only mode = %q, want default", actual)
	}
}

func TestManagedNativeWorkspaceReadOnlyDoesNotRequireKnowledgeMCP(t *testing.T) {
	t.Parallel()
	policy := ExecutionPolicy{
		PermissionLevel:   PermissionLevelDefault,
		WorkspaceReadOnly: true,
	}
	for _, id := range []string{RuntimeIDOpenCode, RuntimeIDGrok, RuntimeIDGemini} {
		t.Run(id, func(t *testing.T) {
			binding := defaultACPRuntimeBindings()[id]
			if _, err := binding.modeID(policy, nil); err != nil {
				t.Fatalf("modeID: %v", err)
			}
		})
	}
}

func TestValidateExecutionPolicyAcceptsProductLevels(t *testing.T) {
	t.Parallel()
	levels := []PermissionLevel{
		PermissionLevelDefault,
		PermissionLevelAuto,
		PermissionLevelFullAccess,
	}
	for _, level := range levels {
		if err := validateExecutionPolicy(ExecutionPolicy{PermissionLevel: level}); err != nil {
			t.Fatalf("validateExecutionPolicy(%q) = %v", level, err)
		}
	}
	if err := validateExecutionPolicy(ExecutionPolicy{}); err == nil {
		t.Fatal("empty policy was accepted")
	}
	trusted := []TrustedControlTool{{
		Server:  "ally_channel_tasks",
		Tool:    "channel_tasks",
		Actions: []string{"list"},
	}}
	if err := validateExecutionPolicy(ExecutionPolicy{
		PermissionLevel:     PermissionLevelDefault,
		WorkspaceReadOnly:   true,
		TrustedControlTools: trusted,
	}); err != nil {
		t.Fatalf("read-only trusted control policy = %v", err)
	}
	// 内部工具由入口签发精确范围，不依赖整个会话是否只读。
	for _, level := range levels {
		if err := validateExecutionPolicy(ExecutionPolicy{
			PermissionLevel:     level,
			TrustedControlTools: trusted,
		}); err != nil {
			t.Fatalf("signed control policy at %q = %v", level, err)
		}
	}
}

func TestRunAppliesProductPermissionLevels(t *testing.T) {
	t.Parallel()

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expectedModes := map[string]map[PermissionLevel]acp.SessionModeId{
		RuntimeIDCodex: {
			PermissionLevelDefault:    "read-only",
			PermissionLevelAuto:       "agent",
			PermissionLevelFullAccess: "agent-full-access",
		},
		RuntimeIDClaude: {
			PermissionLevelDefault:    "default",
			PermissionLevelAuto:       "auto",
			PermissionLevelFullAccess: "bypassPermissions",
		},
	}
	for _, test := range acpRuntimeMatrix() {
		test := test
		for _, level := range []PermissionLevel{
			PermissionLevelDefault,
			PermissionLevelAuto,
			PermissionLevelFullAccess,
		} {
			level := level
			t.Run(test.name+"/"+string(level), func(t *testing.T) {
				t.Parallel()
				sdk, newErr := New(Config{})
				if newErr != nil {
					t.Fatal(newErr)
				}
				backend, ok := sdk.acp.(*acpBackend)
				if !ok {
					t.Fatalf("ACP backend = %T", sdk.acp)
				}
				agent := &fakeCodexACPAgent{
					sessionID:       acp.SessionId(test.runtimeID + "-permission-session"),
					selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
					simplePrompt:    true,
					supportsSSE:     test.runtimeID == RuntimeIDClaude,
				}
				replaceACPAgentFactory(backend, test.runtimeID, func(
					context.Context,
					acpAdapterConfig,
				) (acp.Agent, error) {
					return agent, nil
				})
				t.Cleanup(func() {
					if closeErr := sdk.Close(context.Background()); closeErr != nil {
						t.Errorf("Close() error = %v", closeErr)
					}
				})
				result := sdk.Run(context.Background(), RunRequest{
					Target:    RuntimeTarget{RuntimeID: test.runtimeID, Binary: testBinary},
					Detection: readyDetectionForRun(t, sdk, test.runtimeID, testBinary),
					Workspace: Workspace{Path: t.TempDir(), Managed: true},
					Message:   Message{Text: "应用产品权限"},
					Session:   SessionDirective{Mode: SessionModeNew},
					Policy: RunPolicy{Execution: ExecutionPolicy{
						PermissionLevel: level,
					}},
				}, &recordingSink{})
				if result.Failure != nil {
					t.Fatalf("Run(%s %s) failure = %#v", test.runtimeID, level, result.Failure)
				}
				agent.mutex.Lock()
				selectedMode := agent.selectedMode
				agent.mutex.Unlock()
				if selectedMode != expectedModes[test.runtimeID][level] {
					t.Fatalf(
						"selectedMode = %q, want %q",
						selectedMode,
						expectedModes[test.runtimeID][level],
					)
				}
			})
		}
	}
}

func TestACPPermissionLevelsPreferCurrentModelConfig(t *testing.T) {
	t.Parallel()
	category := acp.SessionConfigOptionCategoryMode
	values := acp.SessionConfigSelectOptionsUngrouped{
		{Value: "default", Name: "Default"},
		{Value: "auto", Name: "Auto"},
	}
	levels := acpPermissionLevels([]acp.SessionConfigOption{{
		Select: &acp.SessionConfigOptionSelect{
			Id:       "mode",
			Name:     "Mode",
			Category: &category,
			Options: acp.SessionConfigSelectOptions{
				Ungrouped: &values,
			},
			Type: "select",
		},
	}}, []acp.SessionMode{{Id: "bypassPermissions", Name: "Bypass"}})
	if !slices.Equal(levels, []PermissionLevel{
		PermissionLevelDefault,
		PermissionLevelAuto,
	}) {
		t.Fatalf("permission levels = %v", levels)
	}
}

// TestACPPermissionMatrix 验证共享审批把 ACP 选项原样交给 Host，而不解析私有权限元数据。
func TestACPPermissionMatrix(t *testing.T) {
	t.Parallel()

	title := "写入 main.go"
	kind := acp.ToolKindEdit
	bridge := newACPBridge(
		"session",
		"invocation",
		nil,
		Workspace{Path: "/workspace"},
		ExecutionPolicy{PermissionLevel: PermissionLevelDefault},
		codexACPRuntimeBinding(),
		0,
	)
	bridge.approvalHandler = approvalHandlerFunc(func(
		_ context.Context,
		request ApprovalRequest,
	) (ApprovalDecision, error) {
		if request.ToolCallID != "tool-1" || request.Title != title ||
			len(request.Options) != 2 {
			t.Fatalf("ApprovalRequest = %#v", request)
		}
		return ApprovalDecision{OptionID: "allow_once"}, nil
	})
	response := bridge.requestPermission(context.Background(), acp.RequestPermissionRequest{
		SessionId: "session",
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "tool-1",
			Title:      &title,
			Kind:       &kind,
			RawInput:   map[string]any{"secret": "must-not-project"},
		},
		Options: []acp.PermissionOption{
			{OptionId: "allow_once", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
			{OptionId: "reject_once", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
		},
	})
	if response.Outcome.Selected == nil ||
		response.Outcome.Selected.OptionId != "allow_once" {
		t.Fatalf("RequestPermission outcome = %#v", response.Outcome)
	}
}

// TestACPRuntimeInputAndConfigurationMatrix 验证两种 Runtime 使用同一 ACP 输入与配置链路。
func TestACPRuntimeInputAndConfigurationMatrix(t *testing.T) {
	t.Parallel()

	for _, test := range acpRuntimeMatrix() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			backend := newACPBackend()
			agent := &fakeCodexACPAgent{
				sessionID:       acp.SessionId(test.runtimeID + "-input-session"),
				selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
				simplePrompt:    true,
				supportsSSE:     test.runtimeID == RuntimeIDClaude,
			}
			var adapterConfig acpAdapterConfig
			replaceACPAgentFactory(backend, test.runtimeID, func(
				_ context.Context,
				config acpAdapterConfig,
			) (acp.Agent, error) {
				adapterConfig = config
				return agent, nil
			})
			t.Cleanup(func() { _ = backend.Close(context.Background()) })

			request := matrixACPRunRequest(t, test.runtimeID, SessionDirective{Mode: SessionModeNew})
			additional := t.TempDir()
			imagePath := request.Request.Workspace.Path + "/image.png"
			if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
				t.Fatal(err)
			}
			request.Prepared.definition.prefixArgs = []string{"--profile", "matrix"}
			request.Prepared.environment = environmentSnapshot{
				values:      map[string]string{"ACP_MATRIX_ENV": "value"},
				names:       map[string]string{"ACP_MATRIX_ENV": "ACP_MATRIX_ENV"},
				fingerprint: test.runtimeID + "-input-environment",
			}
			request.Request.Workspace.AdditionalWritableDirectories = []string{additional}
			request.Request.Message.Attachments = []Attachment{{
				Name: "image.png", Path: "image.png", MediaType: "image/png", RuntimePath: imagePath,
			}}
			request.Request.Connectors = []Connector{
				{
					ID: "stdio", Name: "STDIO", RuntimeKey: "ally_stdio", Selected: false,
					Transport: ConnectorTransportStdio, Command: "mcp-stdio", Arguments: []string{"serve"},
					Environment: map[string]string{"Z_TOKEN": "z", "A_TOKEN": "a"},
				},
				{
					ID: "http", Name: "HTTP", RuntimeKey: "ally_http", Selected: true,
					Transport: ConnectorTransportStreamableHTTP, URL: "https://mcp.example/http",
					Headers: map[string]string{"Z-Token": "z", "A-Token": "a"},
				},
				{
					ID: "sse", Name: "SSE", RuntimeKey: "ally_sse", Selected: false,
					Transport: ConnectorTransportSSE, URL: "https://mcp.example/sse",
				},
			}
			request.Request.Policy.Model = "model-matrix"
			request.Request.Policy.Reasoning = "high"
			request.RuntimeFiles.selectedSkills = []skillSelection{{name: "review"}}

			result := backend.Run(context.Background(), request)
			if result.Failure != nil {
				t.Fatalf("result = %#v", result)
			}
			if adapterConfig.Executable != "/fake/"+test.runtimeID ||
				!reflect.DeepEqual(adapterConfig.PrefixArgs, []string{"--profile", "matrix"}) ||
				!slices.Contains(adapterConfig.Environment, "ACP_MATRIX_ENV=value") {
				t.Fatalf("adapter config = %#v", adapterConfig)
			}

			agent.mutex.Lock()
			newRequest := agent.newSessionRequest
			promptRequest := agent.promptRequest
			selectedMode := agent.selectedMode
			selectedConfigs := make(map[acp.SessionConfigId]acp.SessionConfigValueId, len(agent.selectedConfigs))
			for key, value := range agent.selectedConfigs {
				selectedConfigs[key] = value
			}
			agent.mutex.Unlock()
			expectedMCPServers := 3
			if test.runtimeID == RuntimeIDClaude {
				expectedMCPServers = 4
			}
			if newRequest.Cwd != request.Request.Workspace.Path ||
				!reflect.DeepEqual(newRequest.AdditionalDirectories, []string{additional}) ||
				len(newRequest.McpServers) != expectedMCPServers ||
				newRequest.McpServers[expectedMCPServers-1].Http == nil || newRequest.McpServers[expectedMCPServers-1].Http.Meta["acp-go/user-input"] != true ||
				newRequest.McpServers[0].Stdio == nil ||
				newRequest.McpServers[1].Http == nil ||
				(test.runtimeID == RuntimeIDClaude && newRequest.McpServers[2].Sse == nil) {
				t.Fatalf("session/new = %#v", newRequest)
			}
			if environment := newRequest.McpServers[0].Stdio.Env; len(environment) != 2 ||
				environment[0].Name != "A_TOKEN" || environment[1].Name != "Z_TOKEN" {
				t.Fatalf("STDIO environment = %#v", environment)
			}
			if headers := newRequest.McpServers[1].Http.Headers; len(headers) != 2 ||
				headers[0].Name != "A-Token" || headers[1].Name != "Z-Token" {
				t.Fatalf("HTTP headers = %#v", headers)
			}
			if len(promptRequest.Prompt) != 2 || promptRequest.Prompt[0].Text == nil ||
				promptRequest.Prompt[1].Image == nil ||
				!strings.Contains(promptRequest.Prompt[0].Text.Text, "ally_http (HTTP)") {
				t.Fatalf("session/prompt = %#v", promptRequest)
			}
			skillPrefix := "$review"
			if test.runtimeID == RuntimeIDClaude {
				skillPrefix = "/review"
			}
			expectedMode, modeErr := test.binding.modeID(request.Request.Policy.Execution, nil)
			if modeErr != nil || !strings.HasPrefix(promptRequest.Prompt[0].Text.Text, skillPrefix) ||
				selectedMode != expectedMode ||
				selectedConfigs[test.binding.modelConfigID] != "model-matrix" ||
				selectedConfigs[test.binding.reasoningConfigID] != "high" {
				t.Fatalf("prompt=%q mode=%q configs=%#v", promptRequest.Prompt[0].Text.Text, selectedMode, selectedConfigs)
			}
			assertSSECompatibilityCommentary(
				t,
				request.Sink.(*recordingSink),
				test.runtimeID == RuntimeIDCodex,
			)
		})
	}
}

// assertSSECompatibilityCommentary 验证能力过滤不会静默丢弃 Codex 不支持的 SSE。
func assertSSECompatibilityCommentary(
	t *testing.T,
	sink *recordingSink,
	expected bool,
) {
	t.Helper()
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	found := false
	for _, event := range sink.events {
		if event.Type != EventTypeCommentaryDelta {
			continue
		}
		var payload struct {
			// Text 是不兼容连接器的用户可见说明。
			Text string `json:"text"`
		}
		if json.Unmarshal(event.Data, &payload) == nil &&
			strings.Contains(payload.Text, "SSE") {
			found = true
			break
		}
	}
	if found != expected {
		t.Fatalf("SSE compatibility commentary = %v, want %v", found, expected)
	}
}

// TestACPRuntimePromptInputMatrix 验证权威历史、附件和代码引用经 Runtime.Run 进入标准 ACP Prompt。
func TestACPRuntimePromptInputMatrix(t *testing.T) {
	t.Parallel()

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range acpRuntimeMatrix() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sdk, newErr := New(Config{})
			if newErr != nil {
				t.Fatal(newErr)
			}
			backend, ok := sdk.acp.(*acpBackend)
			if !ok {
				t.Fatalf("ACP backend = %T", sdk.acp)
			}
			agent := testCodexACPAgentWithCatalog()
			agent.sessionID = acp.SessionId(test.runtimeID + "-prompt-session")
			agent.simplePrompt = true
			replaceACPAgentFactory(backend, test.runtimeID, func(
				context.Context,
				acpAdapterConfig,
			) (acp.Agent, error) {
				return agent, nil
			})
			t.Cleanup(func() {
				if closeErr := sdk.Close(context.Background()); closeErr != nil {
					t.Errorf("Close() error = %v", closeErr)
				}
			})

			workspace := t.TempDir()
			imagePath := workspace + "/diagram.png"
			if writeErr := os.WriteFile(imagePath, []byte("png"), 0o600); writeErr != nil {
				t.Fatal(writeErr)
			}
			request := RunRequest{
				Target:    RuntimeTarget{RuntimeID: test.runtimeID, Binary: testBinary},
				Detection: readyDetectionForRun(t, sdk, test.runtimeID, testBinary),
				Workspace: Workspace{Path: workspace, Managed: true},
				History: []HistoryMessage{
					{
						Role:    "user",
						Content: "历史问题",
						CodeReferences: []CodeReference{{
							Path: "projects/app/old.go",
						}},
					},
					{Role: "assistant", Content: "历史回答"},
				},
				Message: Message{
					Text: "当前问题",
					Attachments: []Attachment{
						{Name: "notes.txt", Path: "attachments/notes.txt", MediaType: "text/plain"},
						{Name: "diagram.png", Path: "diagram.png", MediaType: "image/png", RuntimePath: imagePath},
					},
					CodeReferences: []CodeReference{{
						Path: "projects/app/main.go", StartLine: 3, EndLine: 4, Content: "func main() {}",
					}},
				},
				Session: SessionDirective{Mode: SessionModeNew},
				Policy: RunPolicy{Execution: ExecutionPolicy{
					PermissionLevel: PermissionLevelAuto,
				}},
			}
			result := sdk.Run(context.Background(), request, &recordingSink{})
			if result.Failure != nil || result.Session.OutputID == "" {
				t.Fatalf("result = %#v", result)
			}

			agent.mutex.Lock()
			prompt := agent.promptRequest.Prompt
			agent.mutex.Unlock()
			if len(prompt) != 2 || prompt[0].Text == nil || prompt[1].Image == nil {
				t.Fatalf("prompt = %#v", prompt)
			}
			text := prompt[0].Text.Text
			for _, expected := range []string{
				"Chat history:",
				"user: 历史问题",
				"assistant: 历史回答",
				"projects/app/old.go",
				"当前问题",
				"attachments/notes.txt (notes.txt)",
				"diagram.png (diagram.png)",
				"projects/app/main.go:3-4",
				"func main() {}",
			} {
				if !strings.Contains(text, expected) {
					t.Fatalf("prompt text = %q, want %q", text, expected)
				}
			}

			request.Session = SessionDirective{Mode: SessionModeResume, ID: result.Session.OutputID}
			request.Message = Message{Text: "恢复后的当前问题"}
			resumed := sdk.Run(context.Background(), request, &recordingSink{})
			if resumed.Failure != nil || resumed.Session.OutputID != result.Session.OutputID {
				t.Fatalf("resumed = %#v", resumed)
			}
			agent.mutex.Lock()
			resumedPrompt := agent.promptRequest.Prompt
			agent.mutex.Unlock()
			if len(resumedPrompt) != 1 || resumedPrompt[0].Text == nil ||
				resumedPrompt[0].Text.Text != "恢复后的当前问题" {
				t.Fatalf("resumed prompt = %#v", resumedPrompt)
			}
		})
	}
}

// TestACPRuntimeSDKShutdownMatrix 验证共享 Backend 会关闭两种 Runtime 的长期 Agent。
func TestACPRuntimeSDKShutdownMatrix(t *testing.T) {
	t.Parallel()

	backend := newACPBackend()
	agents := make(map[string]*fakeCodexACPAgent)
	for _, test := range acpRuntimeMatrix() {
		agent := &fakeCodexACPAgent{
			sessionID:       acp.SessionId(test.runtimeID + "-shutdown-session"),
			selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
			simplePrompt:    true,
		}
		agents[test.runtimeID] = agent
		replaceACPAgentFactory(backend, test.runtimeID, func(
			context.Context,
			acpAdapterConfig,
		) (acp.Agent, error) {
			return agent, nil
		})
		result := backend.Run(context.Background(), matrixACPRunRequest(
			t,
			test.runtimeID,
			SessionDirective{Mode: SessionModeNew},
		))
		if result.Failure != nil {
			t.Fatalf("%s result = %#v", test.runtimeID, result)
		}
	}
	if err := backend.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for runtimeID, agent := range agents {
		agent.mutex.Lock()
		closed := agent.closed
		agent.mutex.Unlock()
		if !closed {
			t.Fatalf("%s ACP Agent was not closed", runtimeID)
		}
	}
}

// TestACPRuntimeSessionLifecycleMatrix 验证 new、热复用、范围变化后的 resume 和 missing 对两种 Runtime 等价。
func TestACPRuntimeSessionLifecycleMatrix(t *testing.T) {
	t.Parallel()

	for _, test := range acpRuntimeMatrix() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			backend := newACPBackend()
			agent := &fakeCodexACPAgent{
				sessionID:       acp.SessionId(test.runtimeID + "-native-session"),
				selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
				simplePrompt:    true,
			}
			replaceACPAgentFactory(backend, test.runtimeID, func(
				context.Context,
				acpAdapterConfig,
			) (acp.Agent, error) {
				return agent, nil
			})
			t.Cleanup(func() { _ = backend.Close(context.Background()) })

			createdRequest := matrixACPRunRequest(t, test.runtimeID, SessionDirective{Mode: SessionModeNew})
			created := backend.Run(context.Background(), createdRequest)
			if created.Failure != nil || created.Session.State != SessionStateCreated ||
				created.Session.OutputID != test.runtimeID+"-native-session" {
				t.Fatalf("created = %#v", created)
			}

			resumedRequest := createdRequest
			resumedRequest.InvocationID = test.runtimeID + "-resume-invocation"
			resumedRequest.Request.Session = SessionDirective{
				Mode: SessionModeResume,
				ID:   created.Session.OutputID,
			}
			resumedRequest.Request.Policy.Model = "model-next"
			resumedRequest.Request.Policy.Reasoning = "high"
			resumed := backend.Run(context.Background(), resumedRequest)
			if resumed.Failure != nil || resumed.Session.State != SessionStateResumed ||
				resumed.Session.OutputID != created.Session.OutputID {
				t.Fatalf("resumed = %#v", resumed)
			}

			agent.mutex.Lock()
			agent.missingResume = true
			agent.mutex.Unlock()
			missingRequest := resumedRequest
			missingRequest.Request.Workspace.AdditionalWritableDirectories = []string{t.TempDir()}
			missing := backend.Run(context.Background(), missingRequest)
			if missing.Failure == nil || missing.Failure.Code != FailureCodeSessionNotFound ||
				missing.Session.State != SessionStateMissing {
				t.Fatalf("missing = %#v", missing)
			}
			agent.mutex.Lock()
			newCalls := agent.newCalls
			resumeCalls := agent.resumeCalls
			closeCalls := agent.closeSessionCalls
			selectedModel := agent.selectedConfigs[test.binding.modelConfigID]
			selectedReasoning := agent.selectedConfigs[test.binding.reasoningConfigID]
			agent.mutex.Unlock()
			if newCalls != 1 || resumeCalls != 1 || closeCalls != 1 {
				t.Fatalf("new=%d resume=%d close=%d", newCalls, resumeCalls, closeCalls)
			}
			if selectedModel != "model-next" || selectedReasoning != "high" {
				t.Fatalf("model=%q reasoning=%q", selectedModel, selectedReasoning)
			}
		})
	}
}

// TestACPRuntimeReusesLiveSessionAcrossInvocations 验证连续调用不会关闭并重新恢复同一原生会话。
func TestACPRuntimeReusesLiveSessionAcrossInvocations(t *testing.T) {
	t.Parallel()

	for _, test := range acpRuntimeMatrix() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			backend := newACPBackend()
			agent := &fakeCodexACPAgent{
				sessionID:       acp.SessionId(test.runtimeID + "-live-session"),
				selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
				simplePrompt:    true,
			}
			replaceACPAgentFactory(backend, test.runtimeID, func(
				context.Context,
				acpAdapterConfig,
			) (acp.Agent, error) {
				return agent, nil
			})

			request := matrixACPRunRequest(
				t,
				test.runtimeID,
				SessionDirective{Mode: SessionModeNew},
			)
			first := backend.Run(context.Background(), request)
			request.InvocationID = test.runtimeID + "-live-session-second"
			request.Request.Session = SessionDirective{
				Mode: SessionModeResume,
				ID:   first.Session.OutputID,
			}
			second := backend.Run(context.Background(), request)
			if first.Failure != nil || second.Failure != nil {
				t.Fatalf("first=%#v second=%#v", first, second)
			}
			agent.mutex.Lock()
			newCalls := agent.newCalls
			resumeCalls := agent.resumeCalls
			promptCalls := agent.promptCalls
			closeCalls := agent.closeSessionCalls
			agent.mutex.Unlock()
			if newCalls != 1 || resumeCalls != 0 || promptCalls != 2 || closeCalls != 0 {
				t.Fatalf(
					"before Close: new=%d resume=%d prompt=%d close=%d, want 1,0,2,0",
					newCalls,
					resumeCalls,
					promptCalls,
					closeCalls,
				)
			}
			if err := backend.Close(context.Background()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			agent.mutex.Lock()
			closeCalls = agent.closeSessionCalls
			agent.mutex.Unlock()
			agent.mutex.Lock()
			closed := agent.closed
			agent.mutex.Unlock()
			if !closed {
				t.Fatal("backend Close did not release the session-owning Adapter")
			}
		})
	}
}

// TestACPRuntimeReopensLiveSessionWhenMCPChanges 验证连接器范围变化会重新挂载原生 Session。
func TestACPRuntimeReopensLiveSessionWhenMCPChanges(t *testing.T) {
	t.Parallel()

	for _, test := range acpRuntimeMatrix() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			backend := newACPBackend()
			agent := &fakeCodexACPAgent{
				sessionID:       acp.SessionId(test.runtimeID + "-mcp-session"),
				selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
				simplePrompt:    true,
				supportsSSE:     test.runtimeID == RuntimeIDClaude,
			}
			replaceACPAgentFactory(backend, test.runtimeID, func(
				context.Context,
				acpAdapterConfig,
			) (acp.Agent, error) {
				return agent, nil
			})

			request := matrixACPRunRequest(
				t,
				test.runtimeID,
				SessionDirective{Mode: SessionModeNew},
			)
			first := backend.Run(context.Background(), request)
			request.InvocationID = test.runtimeID + "-mcp-session-second"
			request.Request.Session = SessionDirective{
				Mode: SessionModeResume,
				ID:   first.Session.OutputID,
			}
			request.Request.Connectors = []Connector{{
				ID:         "connector-1",
				Name:       "Connector",
				RuntimeKey: "ally_connector",
				Transport:  ConnectorTransportStdio,
				Command:    "connector-command",
			}}
			second := backend.Run(context.Background(), request)
			if first.Failure != nil || second.Failure != nil {
				t.Fatalf("first=%#v second=%#v", first, second)
			}
			agent.mutex.Lock()
			resumeCalls := agent.resumeCalls
			closeCalls := agent.closeSessionCalls
			agent.mutex.Unlock()
			if resumeCalls != 1 || closeCalls != 1 {
				t.Fatalf("resume=%d close=%d, want 1 and 1", resumeCalls, closeCalls)
			}
			if err := backend.Close(context.Background()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			agent.mutex.Lock()
			closeCalls = agent.closeSessionCalls
			agent.mutex.Unlock()
			agent.mutex.Lock()
			closed := agent.closed
			agent.mutex.Unlock()
			if !closed {
				t.Fatal("backend Close did not release the reopened session-owning Adapter")
			}
		})
	}
}

// TestACPRuntimeClosesSessionWithoutPersistenceCapabilities 验证不支持长期会话的 Agent 沿用短期释放策略。
func TestACPRuntimeClosesSessionWithoutPersistenceCapabilities(t *testing.T) {
	t.Parallel()

	backend := newACPBackend()
	agent := &fakeCodexACPAgent{
		selectedConfigs:           make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		simplePrompt:              true,
		disableSessionPersistence: true,
	}
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return agent, nil
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	result := backend.Run(context.Background(), matrixACPRunRequest(
		t,
		RuntimeIDCodex,
		SessionDirective{Mode: SessionModeNew},
	))
	if result.Failure != nil {
		t.Fatalf("result = %#v", result)
	}
	agent.mutex.Lock()
	closeCalls := agent.closeSessionCalls
	agent.mutex.Unlock()
	if closeCalls != 1 {
		t.Fatalf("close=%d, want 1", closeCalls)
	}
}

// TestACPRuntimeConcurrentSessionsStayIsolated 验证共享 Agent 不复用并发 Chat 的 Session ID。
func TestACPRuntimeConcurrentSessionsStayIsolated(t *testing.T) {
	t.Parallel()

	for _, test := range acpRuntimeMatrix() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			backend := newACPBackend()
			agent := &fakeCodexACPAgent{
				sessionIDs: []acp.SessionId{
					acp.SessionId(test.runtimeID + "-session-1"),
					acp.SessionId(test.runtimeID + "-session-2"),
				},
				selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
				simplePrompt:    true,
			}
			var factoryMutex sync.Mutex
			factoryCalls := 0
			replaceACPAgentFactory(backend, test.runtimeID, func(
				context.Context,
				acpAdapterConfig,
			) (acp.Agent, error) {
				if test.runtimeID == RuntimeIDClaude {
					factoryMutex.Lock()
					factoryCalls++
					index := factoryCalls
					factoryMutex.Unlock()
					return &fakeCodexACPAgent{sessionID: acp.SessionId(fmt.Sprintf("claude-session-%d", index)), selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId), simplePrompt: true}, nil
				}
				return agent, nil
			})
			t.Cleanup(func() { _ = backend.Close(context.Background()) })

			results := make([]Result, 2)
			var group sync.WaitGroup
			for index := range results {
				group.Add(1)
				go func(resultIndex int) {
					defer group.Done()
					request := matrixACPRunRequest(
						t,
						test.runtimeID,
						SessionDirective{Mode: SessionModeNew},
					)
					request.InvocationID = fmt.Sprintf("%s-concurrent-%d", test.runtimeID, resultIndex)
					results[resultIndex] = backend.Run(context.Background(), request)
				}(index)
			}
			group.Wait()
			if results[0].Failure != nil || results[1].Failure != nil ||
				results[0].Session.OutputID == results[1].Session.OutputID {
				t.Fatalf("results = %#v", results)
			}
		})
	}
}

// TestACPRuntimeCancellationLeavesNextInvocationUsable 验证取消已提交 Prompt 后共享 Agent 仍可继续服务。
func TestACPRuntimeCancellationLeavesNextInvocationUsable(t *testing.T) {
	t.Parallel()

	for _, test := range acpRuntimeMatrix() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			backend := newACPBackend()
			agent := &fakeCodexACPAgent{
				sessionIDs: []acp.SessionId{
					acp.SessionId(test.runtimeID + "-cancelled"),
					acp.SessionId(test.runtimeID + "-next"),
				},
				selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
				blockPrompt:     true,
				promptStarted:   make(chan struct{}),
			}
			createdAgent := false
			replaceACPAgentFactory(backend, test.runtimeID, func(
				context.Context,
				acpAdapterConfig,
			) (acp.Agent, error) {
				if createdAgent && test.runtimeID == RuntimeIDClaude {
					return &fakeCodexACPAgent{sessionID: acp.SessionId(test.runtimeID + "-next"), selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId), simplePrompt: true}, nil
				}
				createdAgent = true
				return agent, nil
			})
			t.Cleanup(func() { _ = backend.Close(context.Background()) })

			ctx, cancel := context.WithCancel(context.Background())
			cancelledResult := make(chan Result, 1)
			cancelRequest := matrixACPRunRequest(
				t,
				test.runtimeID,
				SessionDirective{Mode: SessionModeNew},
			)
			sink := newSignalingRecordingSink()
			cancelRequest.Sink = sink
			go func() {
				cancelledResult <- backend.Run(ctx, cancelRequest)
			}()
			<-agent.promptStarted
			sink.waitForEvent(t, EventTypeTextDelta)
			cancel()
			cancelled := <-cancelledResult
			if cancelled.Failure == nil || cancelled.Failure.Code != FailureCodeContextCanceled ||
				cancelled.Session.OutputID != test.runtimeID+"-cancelled" ||
				!cancelled.SideEffects.VisibleOutput {
				t.Fatalf("cancelled = %#v", cancelled)
			}

			agent.mutex.Lock()
			agent.blockPrompt = false
			agent.simplePrompt = true
			agent.mutex.Unlock()
			next := backend.Run(context.Background(), matrixACPRunRequest(
				t,
				test.runtimeID,
				SessionDirective{Mode: SessionModeNew},
			))
			if next.Failure != nil || next.Session.OutputID != test.runtimeID+"-next" {
				t.Fatalf("next = %#v", next)
			}
		})
	}
}

// TestACPRuntimeContextCancellationWinsLateTransportError 验证取消事实不会被迟到的关闭错误降级。
func TestACPRuntimeContextCancellationWinsLateTransportError(t *testing.T) {
	t.Parallel()
	backend := newACPBackend()
	agent := &fakeCodexACPAgent{
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		blockPrompt:     true,
		promptStarted:   make(chan struct{}),
		promptError:     errors.New("claude transport closed"),
	}
	replaceACPAgentFactory(backend, RuntimeIDClaude, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return agent, nil
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	resultChannel := make(chan Result, 1)
	request := matrixACPRunRequest(t, RuntimeIDClaude, SessionDirective{Mode: SessionModeNew})
	go func() { resultChannel <- backend.Run(ctx, request) }()
	<-agent.promptStarted
	cancel()
	result := <-resultChannel
	if result.Failure == nil || result.Failure.Code != FailureCodeContextCanceled {
		t.Fatalf("result = %#v", result)
	}
}

// assertACPProjectionCapabilities 检查 Runtime Event 中的统一展示语义，而不比较原始更新顺序。
func assertACPProjectionCapabilities(t *testing.T, events []Event) {
	t.Helper()
	wants := map[string]bool{
		"text": false, "reasoning": false, "mcp": false,
		"skill": false, "file": false, "plan": false, "usage": false,
	}
	for _, event := range events {
		switch event.Type {
		case EventTypeTextDelta, EventTypeCommentaryDelta:
			wants["text"] = true
		case EventTypeThinkingDelta:
			wants["reasoning"] = true
		case EventTypeToolUse, EventTypeToolInputSnapshot, EventTypeToolUpdate, EventTypeToolResult:
			var payload map[string]any
			if err := json.Unmarshal(event.Data, &payload); err != nil {
				t.Fatal(err)
			}
			switch payload["name"] {
			case "mcp__ally_memory__memory_search":
				wants["mcp"] = true
			case "skill":
				wants["skill"] = true
			case "file_modify":
				if payload["path"] == "/workspace/main.go" {
					wants["file"] = true
				}
			case "update_plan":
				wants["plan"] = true
			}
		case EventTypeUsage:
			var usage UsageData
			if err := json.Unmarshal(event.Data, &usage); err != nil {
				t.Fatal(err)
			}
			if usage.Turn != nil && usage.Context != nil &&
				usage.Turn.InputTokens == 10 && usage.Context.UsedTokens == 120 {
				wants["usage"] = true
			}
		}
	}
	for capability, present := range wants {
		if !present {
			t.Fatalf("projection missing %s: %#v", capability, events)
		}
	}
}

// matrixACPRunRequest 创建不经过真实探测的共享 ACP Backend 测试请求。
func matrixACPRunRequest(
	t *testing.T,
	runtimeID string,
	session SessionDirective,
) acpRunRequest {
	t.Helper()
	defaults, err := normalizeDefaults(Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	return acpRunRequest{
		InvocationID: runtimeID + "-matrix-invocation",
		Prepared: preparedRuntime{
			definition: runtimeDefinition{
				id: runtimeID, baseRuntimeID: runtimeID, defaultModel: "default",
			},
			detection: DetectionResult{
				Runtime:        RuntimeSummary{ID: runtimeID, BaseRuntimeID: runtimeID},
				ResolvedBinary: "/fake/" + runtimeID,
			},
			environment: environmentSnapshot{fingerprint: runtimeID + "-matrix"},
			defaults:    defaults,
		},
		Request: RunRequest{
			Target:    RuntimeTarget{RuntimeID: runtimeID},
			Workspace: Workspace{Path: t.TempDir(), Managed: true},
			Message:   Message{Text: "matrix"},
			Session:   session,
			Policy: RunPolicy{Execution: ExecutionPolicy{
				PermissionLevel: PermissionLevelAuto,
			}},
		},
		Prompt: "matrix",
		Sink:   &recordingSink{},
	}
}
