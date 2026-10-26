package runtimes

import (
	"context"
	"reflect"
	"testing"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestNewNativeRuntimeRegistry 验证运行时可发现且交付真实指令与技能目录，不伪造模型目录。
func TestNewNativeRuntimeRegistry(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"opencode", "gemini", "grok"} {
		definition, ok := definitions[id]
		if !ok {
			t.Errorf("runtime missing: %s", id)
			continue
		}
		instruction := "AGENTS.md"
		if id == "gemini" {
			instruction = "GEMINI.md"
		}
		if definition.instructionFileName != instruction || definition.skillDirectoryName != "."+id || len(definition.models) != 0 {
			t.Errorf("invented native files/models: %+v", definition)
		}
		binding, ok := defaultACPRuntimeBindings()[id]
		if !ok || binding.newAgent == nil || !binding.nativeProcess || binding.retainedProcess {
			t.Errorf("unsafe lifecycle binding: %s", id)
		}
	}
}

// TestNewNativeVersionDoesNotUseBridgeBuild 防止版本探测超时后把适配器构建号当作 CLI 版本。
func TestNewNativeVersionDoesNotUseBridgeBuild(t *testing.T) {
	for _, id := range []string{"opencode", "gemini", "grok"} {
		catalog := acpInitializeCatalog(defaultACPRuntimeBindings()[id], acp.InitializeResponse{AgentInfo: &acp.Implementation{Name: id, Version: "bridge-build"}})
		if catalog.Version != "" {
			t.Errorf("%s exposed bridge version as CLI: %s", id, catalog.Version)
		}
		actual := acpInitializeCatalog(defaultACPRuntimeBindings()[id], acp.InitializeResponse{AgentInfo: &acp.Implementation{Name: id, Version: "bridge-build", Meta: map[string]any{"runtime": map[string]any{"version": "native-1.2.3"}}}})
		if actual.Version != "native-1.2.3" {
			t.Errorf("native version lost: %s", actual.Version)
		}
	}
}

// TestExistingNativeVersionFallback 保留原有原生绑定的握手版本兼容行为。
func TestExistingNativeVersionFallback(t *testing.T) {
	catalog := acpInitializeCatalog(kimiACPRuntimeBinding(), acp.InitializeResponse{AgentInfo: &acp.Implementation{Version: "legacy-cli"}})
	if catalog.Version != "legacy-cli" {
		t.Fatalf("legacy version lost: %s", catalog.Version)
	}
}

// TestNewNativePermissionFactoriesRefresh 验证相同会话跨权限轮次创建新适配器，并应用恢复后的模式。
func TestNewNativePermissionFactoriesRefresh(t *testing.T) {
	for _, id := range []string{RuntimeIDOpenCode, RuntimeIDGemini, RuntimeIDGrok} {
		t.Run(id, func(t *testing.T) {
			backend := newACPBackend()
			defer backend.Close(context.Background())
			modes := []string{}
			agents := []*fakeCodexACPAgent{}
			replaceACPAgentFactory(backend, id, func(_ context.Context, config acpAdapterConfig) (acp.Agent, error) {
				modes = append(modes, config.PermissionMode)
				agent := &fakeCodexACPAgent{sessionID: "native-stable", selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId), simplePrompt: true}
				agents = append(agents, agent)
				return agent, nil
			})
			request := matrixACPRunRequest(t, id, SessionDirective{Mode: SessionModeNew})
			for _, level := range []PermissionLevel{PermissionLevelFullAccess, PermissionLevelDefault, PermissionLevelAuto} {
				request.Request.Policy.Execution.PermissionLevel = level
				result := backend.Run(context.Background(), request)
				if result.Failure != nil {
					t.Fatalf("%s: %+v", level, result)
				}
				request.Request.Session = SessionDirective{Mode: SessionModeResume, ID: result.Session.OutputID}
			}
			if !reflect.DeepEqual(modes, []string{"full-access", "default", "auto"}) {
				t.Fatalf("stale constructor permission: %v", modes)
			}
			if id == RuntimeIDGemini {
				for index, want := range []acp.SessionModeId{"yolo", "default", "default"} {
					if agents[index].selectedMode != want {
						t.Fatalf("stale resumed mode: %q", agents[index].selectedMode)
					}
				}
			}
		})
	}
}

// TestNewNativeCallbacksRouteExactly 验证新增绑定的审批原始选项和问答仅交给匹配执行，完全访问也保留问答。
func TestNewNativeCallbacksRouteExactly(t *testing.T) {
	for _, id := range []string{RuntimeIDOpenCode, RuntimeIDGemini, RuntimeIDGrok} {
		t.Run(id, func(t *testing.T) {
			binding := defaultACPRuntimeBindings()[id]
			client := newACPClient(binding)
			bridge := newACPBridge("native-session", "invocation", nil, Workspace{}, ExecutionPolicy{PermissionLevel: PermissionLevelFullAccess}, binding, 0)
			handler := &recordingElicitationHandler{}
			bridge.elicitationHandler = handler
			bridge.approvalHandler = approvalHandlerFunc(func(_ context.Context, request ApprovalRequest) (ApprovalDecision, error) {
				return ApprovalDecision{OptionID: request.Options[0].ID}, nil
			})
			if err := client.register(bridge); err != nil {
				t.Fatal(err)
			}
			permission, err := client.RequestPermission(context.Background(), acp.RequestPermissionRequest{SessionId: "native-session", ToolCall: acp.ToolCallUpdate{ToolCallId: "tool"}, Options: []acp.PermissionOption{{OptionId: "native-choice", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce}}})
			if err != nil || permission.Outcome.Selected == nil || permission.Outcome.Selected.OptionId != "native-choice" {
				t.Fatalf("approval lost: %+v %v", permission, err)
			}
			question := acp.NewUnstableCreateElicitationRequestForm(acp.UnstableElicitationSchema{Properties: map[string]any{"answer": map[string]any{"type": "string"}}})
			question.Form.Message = "Choose"
			question.Form.Meta = map[string]any{"sessionId": "native-session"}
			answer, err := client.UnstableCreateElicitation(context.Background(), question)
			if err != nil || answer.Accept == nil || handler.calls != 1 {
				t.Fatalf("question lost: %+v %v", answer, err)
			}
			question.Form.Meta = map[string]any{"sessionId": "other-session"}
			answer, err = client.UnstableCreateElicitation(context.Background(), question)
			if err != nil || answer.Cancel == nil || handler.calls != 1 {
				t.Fatalf("question leaked: %+v %v", answer, err)
			}
		})
	}
}
