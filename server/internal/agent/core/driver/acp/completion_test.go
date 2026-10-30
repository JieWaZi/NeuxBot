package acp_test

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	driver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// TestLastAssistantCompletionRequiresFinalActivity 捕获工具前正文、取消和缺失身份被提升为最终答案的错误。
func TestLastAssistantCompletionRequiresFinalActivity(t *testing.T) {
	for _, tt := range []struct {
		name     string         // name 是原生活动顺序场景。
		tail     string         // tail 是正文之后到达的活动类型。
		stop     sdk.StopReason // stop 是原生结束原因。
		verified bool           // verified 是是否允许验证最后正文的独立预期。
	}{{"last assistant", "", sdk.StopReasonEndTurn, true}, {"length preserves partial", "", sdk.StopReasonMaxTokens, true}, {"tool after text", "tool", sdk.StopReasonEndTurn, false}, {"tool then empty boundary", "tool-complete", sdk.StopReasonEndTurn, false}, {"known commentary stays nonfinal", "commentary-unknown", sdk.StopReasonEndTurn, false}, {"thought after text", "thought", sdk.StopReasonEndTurn, false}, {"cancelled", "", sdk.StopReasonCancelled, false}, {"refusal", "", sdk.StopReasonRefusal, false}, {"missing identity", "missing", sdk.StopReasonEndTurn, false}, {"error", "error", sdk.StopReasonEndTurn, false}} {
		t.Run(tt.name, func(t *testing.T) {
			a := newFixtureAgent()
			a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				id := "answer"
				chunk := &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("native answer")}
				if tt.tail == "missing" {
					chunk.MessageId = nil
				}
				if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: chunk}}); err != nil {
					return sdk.PromptResponse{}, err
				}
				var tail sdk.SessionUpdate
				if tt.tail == "tool" || tt.tail == "tool-complete" {
					tail.ToolCall = &sdk.SessionUpdateToolCall{ToolCallId: "tool", Title: "run", Status: sdk.ToolCallStatusCompleted}
				}
				if tt.tail == "thought" {
					tail.AgentThoughtChunk = &sdk.SessionUpdateAgentThoughtChunk{Content: sdk.TextBlock("still thinking")}
				}
				if tail.ToolCall != nil || tail.AgentThoughtChunk != nil {
					if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: tail}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				if tt.tail == "tool-complete" || tt.tail == "commentary-unknown" {
					phase := "unknown"
					if tt.tail == "commentary-unknown" {
						phase = "commentary"
					}
					if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock(""), Meta: map[string]any{"acp-go/message": map[string]any{"phase": phase, "complete": false, "source": "native_message"}}}}}); err != nil {
						return sdk.PromptResponse{}, err
					}
					if tt.tail == "commentary-unknown" {
						if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("late process")}}}); err != nil {
							return sdk.PromptResponse{}, err
						}
					}
				}
				if tt.tail == "error" {
					return sdk.PromptResponse{}, sdk.NewInternalError(nil)
				}
				return sdk.PromptResponse{StopReason: tt.stop}, nil
			}
			d, err := driver.New(driver.Config{ID: "generic", CompletionPolicy: contract.CompletionLastAssistant, Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) { return a, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close(context.Background())
			s, err := d.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			sink := &recordingSink{}
			s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "input"}}, sink)
			verified := false
			for _, event := range sink.snapshot() {
				if event.Message != nil && event.Message.CompletionVerified {
					verified = true
					if event.Message.Phase != contract.PhaseUnknown || !event.Message.Complete || event.Message.MessageID != "answer" {
						t.Fatalf("invented native phase: %+v", event.Message)
					}
				}
			}
			if verified != tt.verified {
				t.Fatalf("completion proof=%v, expected %v", verified, tt.verified)
			}
		})
	}
}
