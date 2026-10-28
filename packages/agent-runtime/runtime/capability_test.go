package runtime_test

import (
	"context"
	"strings"
	"testing"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	r "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
)

// TestRichToolContentUsesEventBudget 验证新增文件差异和公开工具参数受现有事件预算约束。
func TestRichToolContentUsesEventBudget(t *testing.T) {
	large := strings.Repeat("x", 1000)
	for name, event := range map[string]c.DriverEvent{
		"diff":            {Kind: c.EventTool, Tool: &c.ToolEvent{ID: "edit", Content: []c.ContentBlock{{Type: "diff", Diff: &c.Diff{Path: "file", NewText: large}}}}},
		"input":           {Kind: c.EventTool, Tool: &c.ToolEvent{ID: "tool", Input: []byte("\"" + large + "\"")}},
		"output":          {Kind: c.EventTool, Tool: &c.ToolEvent{ID: "tool", Output: []byte("\"" + large + "\"")}},
		"location":        {Kind: c.EventTool, Tool: &c.ToolEvent{ID: "tool", Locations: []c.Location{{Path: large}}}},
		"assistant image": {Kind: c.EventAssistantMessage, Message: &c.AssistantMessage{MessageID: "answer", Content: []c.ContentBlock{{Type: "image", Data: large, MIMEType: "image/png"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			driver := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
				return &fakeSession{prompt: func(ctx context.Context, request c.TurnRequest, sink c.DriverSink) c.DriverResult {
					_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
					_ = sink.Accept(ctx, event)
					return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
				}}, nil
			}}
			runtime := started(t, driver, nil)
			input := spec()
			input.Limits.MaxEventBytes = 400
			result := runtime.Invoke(context.Background(), input)
			requireCode(t, result.Fault, c.FaultLimit)
			if event.Tool != nil && !result.Effects.ToolActivity {
				t.Fatal("observed tool side effect disappeared on budget failure")
			}
		})
	}
}

// TestUnattendedApprovalUsesExactPublicOption 验证无人值守授权仍回传实际公开选项身份。
func TestUnattendedApprovalUsesExactPublicOption(t *testing.T) {
	driver := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, request c.TurnRequest, sink c.DriverSink) c.DriverResult {
			_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			response, err := sink.Interact(ctx, c.InteractionRequest{ID: "approval", Kind: c.InteractionApproval, Approval: &c.ApprovalRequest{Options: []c.ApprovalOption{{ID: "reject", Kind: "reject_once"}, {ID: "allow", Kind: "allow_once"}}}})
			if err != nil || response.Action != c.InteractionAccept || response.OptionID != "allow" {
				t.Errorf("wrong exact decision: %+v %v", response, err)
			}
			message := c.AssistantMessage{MessageID: "final", Phase: c.PhaseFinal, Delta: "done", Complete: true}
			_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &message})
			return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
		}}, nil
	}}
	runtime := started(t, driver, func(config *r.Config) { config.Grants.AllowAllTools = true })
	input := spec()
	input.Policies.Tools = c.ToolPolicy{Mode: c.ToolsAllowAll}
	result := runtime.Invoke(context.Background(), input)
	if result.Status != c.StatusCompleted {
		t.Fatal(result)
	}
}

// TestStructuredInputUsesInteractionBudget 验证表单结构和提交内容不会绕过交互预算。
func TestStructuredInputUsesInteractionBudget(t *testing.T) {
	for _, largeRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "response", true: "request"}[largeRequest], func(t *testing.T) {
			driver := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
				return &fakeSession{prompt: func(ctx context.Context, request c.TurnRequest, sink c.DriverSink) c.DriverResult {
					_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
					input := &c.InputRequest{Mode: "form"}
					if largeRequest {
						input.RequestedSchema = map[string]any{"description": strings.Repeat("x", 1000)}
					}
					_, _ = sink.Interact(ctx, c.InteractionRequest{ID: "question", Kind: c.InteractionInput, Input: input})
					return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
				}}, nil
			}}
			runtime := started(t, driver, nil)
			input := spec()
			input.Policies.Interaction = c.InteractionInteractive
			input.Limits.MaxEventBytes = 600
			result := runtime.Run(context.Background(), input, c.RunOptions{InteractionHandler: interactionFunc(func(ctx context.Context, request c.InteractionRequest) (c.InteractionResponse, error) {
				return c.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: c.InteractionAccept, Content: map[string]any{"answer": strings.Repeat("x", 1000)}}, nil
			})})
			requireCode(t, result.Fault, c.FaultLimit)
		})
	}
}
