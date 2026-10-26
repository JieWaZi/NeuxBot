package runtimes

import (
	"context"
	"strconv"
	"testing"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// cursorThinkingAgent 模拟原生模型目录和配置生效回执。
type cursorThinkingAgent struct {
	// fakeCodexACPAgent 复用会话、流式响应与生命周期测试替身。
	*fakeCodexACPAgent
	// confirm 控制是否确认实际选择，用于验证错误回执不得继续执行。
	confirm bool
}

// SetSessionConfigOption 回传选择后生效的思考值。
func (agent *cursorThinkingAgent) SetSessionConfigOption(ctx context.Context, request acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	result, err := agent.fakeCodexACPAgent.SetSessionConfigOption(ctx, request)
	if err == nil && agent.confirm && request.ValueId != nil && request.ValueId.ConfigId == "reasoning" {
		for _, option := range result.ConfigOptions {
			if option.Select != nil && option.Select.Id == "reasoning" {
				option.Select.CurrentValue = request.ValueId.Value
			}
		}
	}
	return result, err
}

// TestCursorThinkingSelectionAndDefault 验证显式强度、恢复默认值与回执校验共用同一会话。
func TestCursorThinkingSelectionAndDefault(t *testing.T) {
	backend := newACPBackend()
	defer backend.Close(context.Background())
	agent := &cursorThinkingAgent{fakeCodexACPAgent: testCodexACPAgentWithCatalog(), confirm: true}
	agent.simplePrompt = true
	values := acp.SessionConfigSelectOptionsUngrouped{{Value: "medium", Name: "Medium"}, {Value: "high", Name: "High"}}
	thought := acp.SessionConfigOption{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: "medium", Options: acp.SessionConfigSelectOptions{Ungrouped: &values}}}
	agent.configOptions = append(agent.configOptions, thought)
	for _, config := range agent.configOptions {
		if config.Select != nil && config.Select.Id == "model" {
			for index := range *config.Select.Options.Ungrouped {
				(*config.Select.Options.Ungrouped)[index].Meta = map[string]any{"acp-go/model-config-options": []acp.SessionConfigOption{thought}}
			}
		}
	}
	replaceACPAgentFactory(backend, RuntimeIDCursor, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
	request := matrixACPRunRequest(t, RuntimeIDCursor, SessionDirective{Mode: SessionModeNew})
	request.Request.Policy.Model = "gpt-fixture"
	request.Request.Policy.Reasoning = "high"
	first := backend.Run(context.Background(), request)
	if first.Failure != nil || agent.selectedConfigs["reasoning"] != "high" {
		t.Fatalf("high: %+v", first)
	}
	request.Request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	request.Request.Policy.Reasoning = "medium"
	request.InvocationID = "cursor-thinking-default"
	if result := backend.Run(context.Background(), request); result.Failure != nil || agent.selectedConfigs["reasoning"] != "medium" {
		t.Fatalf("default: %+v", result)
	}
	agent.confirm = false
	request.Request.Policy.Reasoning = "high"
	request.InvocationID = "cursor-thinking-unconfirmed"
	if result := backend.Run(context.Background(), request); result.Failure == nil || agent.promptCalls != 2 {
		t.Fatalf("unconfirmed thinking executed: %+v prompts=%d", result, agent.promptCalls)
	}
}

// TestCursorFailureCodeUsesNativeUpgradeEvidence 验证 Cursor Adapter 独立维护原生错误解析。
func TestCursorFailureCodeUsesNativeUpgradeEvidence(t *testing.T) {
	err := acp.NewInternalError(map[string]any{
		"errorKind":       "billing_error",
		"cursorErrorCode": "upgrade",
	})
	if code := cursorACPFailureCode(err); code != FailureCodePlanUpgradeRequired {
		t.Fatalf("cursorACPFailureCode() = %q, want plan upgrade", code)
	}
}

// TestCursorRetrySwitchesModelOnWarmSession 验证套餐失败后换模型会送达同一会话，且不会重启进程。
func TestCursorRetrySwitchesModelOnWarmSession(t *testing.T) {
	backend := newACPBackend()
	defer backend.Close(context.Background())
	var agents []*fakeCodexACPAgent
	replaceACPAgentFactory(backend, RuntimeIDCursor, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
		agent := testCodexACPAgentWithCatalog()
		agent.disableSessionPersistence = true
		agent.simplePrompt = true
		agent.sessionID = acp.SessionId("cursor-chat-" + strconv.Itoa(len(agents)))
		agents = append(agents, agent)
		return agent, nil
	})
	request := matrixACPRunRequest(t, RuntimeIDCursor, SessionDirective{Mode: SessionModeNew})
	request.Request.Policy.Model = "gpt-fixture"
	first := backend.Run(context.Background(), request)
	if first.Failure != nil {
		t.Fatalf("first: %+v", first)
	}
	request.Request.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	request.InvocationID = "cursor-rejected"
	agents[0].mutex.Lock()
	agents[0].promptError = acp.NewInternalError(map[string]any{"message": "Please upgrade your plan to use this model"})
	agents[0].mutex.Unlock()
	failed := backend.Run(context.Background(), request)
	if failed.Failure == nil || failed.Failure.Code != FailureCodePlanUpgradeRequired {
		t.Fatalf("provider rejection lost: %+v; processes=%d", failed, len(agents))
	}
	agents[0].mutex.Lock()
	agents[0].promptError = nil
	agents[0].mutex.Unlock()
	request.InvocationID = "cursor-retry"
	request.Request.Policy.Model = "gpt-fast"
	retried := backend.Run(context.Background(), request)
	if retried.Failure != nil || len(agents) != 1 || agents[0].selectedConfigs["model"] != "gpt-fast" || agents[0].newCalls != 1 || agents[0].resumeCalls != 0 {
		t.Fatalf("retry: %+v; processes=%d model=%s", retried, len(agents), agents[0].selectedConfigs["model"])
	}
	configCalls := agents[0].configCalls
	request.InvocationID = "cursor-same-model"
	if result := backend.Run(context.Background(), request); result.Failure != nil || agents[0].configCalls != configCalls {
		t.Fatalf("unchanged model was initialized again: %+v", result)
	}
	request.Request.Policy.Model = "gpt-fixture"
	request.InvocationID = "cursor-default-model"
	if result := backend.Run(context.Background(), request); result.Failure != nil || agents[0].selectedConfigs["model"] != "gpt-fixture" {
		t.Fatalf("default kept previous model: %+v", result)
	}
	// 同目录下另一聊天必须拥有独立进程，避免 Cursor 全局模型设置串扰。
	other := request
	other.InvocationID = "cursor-another-chat"
	other.Request.Session = SessionDirective{Mode: SessionModeNew}
	if result := backend.Run(context.Background(), other); result.Failure != nil || len(agents) != 2 {
		t.Fatalf("another chat: %+v; processes=%d", result, len(agents))
	}
	if err := backend.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, agent := range agents {
		if !agent.closed {
			t.Fatal("Cursor process leaked after SDK shutdown")
		}
	}
}

// TestCursorRejectsUnconfirmedModelSelection 验证模型回执不一致时不能按旧模型发送用户消息。
func TestCursorRejectsUnconfirmedModelSelection(t *testing.T) {
	backend := newACPBackend()
	defer backend.Close(context.Background())
	agent := testCodexACPAgentWithCatalog()
	agent.simplePrompt = true
	agent.configOptionsByModel["gpt-fast"] = agent.configOptions
	replaceACPAgentFactory(backend, RuntimeIDCursor, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
	request := matrixACPRunRequest(t, RuntimeIDCursor, SessionDirective{Mode: SessionModeNew})
	request.Request.Policy.Model = "gpt-fast"
	result := backend.Run(context.Background(), request)
	if result.Failure == nil || agent.promptCalls != 0 {
		t.Fatalf("unconfirmed model was used: %+v, promptCalls=%d", result, agent.promptCalls)
	}
}
