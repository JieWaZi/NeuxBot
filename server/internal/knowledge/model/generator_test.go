package model

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/JieWaZi/neux-knowledge/core"
)

// runtimeFunc 提供不启动原生进程的宿主结果端口。
type runtimeFunc func(context.Context, agent.TextRequest) (agent.TextResult, error)

func (run runtimeFunc) GenerateText(ctx context.Context, request agent.TextRequest) (agent.TextResult, error) {
	return run(ctx, request)
}

func TestGeneratorPreservesMessagesAndMapsResultExactlyOnce(t *testing.T) {
	calls := 0
	generator := New(Config{Connection: agent.ProviderConnectionRequest{Account: agent.AccountRequest{Provider: "claude", AccountID: "account", Model: "model-a"}}, Reasoning: "high", Runtime: runtimeFunc(func(_ context.Context, request agent.TextRequest) (agent.TextResult, error) {
		calls++
		if request.Connection.Account.Provider != "claude" || request.Connection.Account.AccountID != "account" || request.Connection.Account.Model != "model-a" || request.Selection.Reasoning != "high" || !request.Selection.PreferNoThinking || request.UsageSource != "knowledge_generation" || request.MaxTokens != 32768 || request.Temperature != .3 || len(request.Messages) != 2 || request.Messages[0].Content != "rules" || request.Messages[1].Content != "source" {
			t.Fatalf("request=%+v", request)
		}
		input, read, write, output := int64(3), int64(5), int64(0), int64(7)
		return agent.TextResult{Text: "raw model text", ToolsDisabled: true, Turn: agent.TurnResult{
			Outcome: &agent.AgentNativeOutcome{StopReason: "max_tokens", Turn: acpmeta.NativeTurn{
				Usage: &acpmeta.TurnUsage{InputTokens: &input, CacheReadTokens: &read, CacheWriteTokens: &write, OutputTokens: &output, Complete: true},
			}},
			Effective: agent.AgentSession{Options: []agent.AgentSessionOption{{Category: "thought_level", Value: agent.AgentOptionValue{ID: "high"}}}},
		}}, nil
	})})
	result, err := generator.Generate(context.Background(), core.GenerateRequest{Messages: []core.ModelMessage{{Role: "system", Content: "rules"}, {Role: "user", Content: "source"}}, MaxTokens: 32768, Temperature: .3})
	if err != nil || calls != 1 || result.Text != "raw model text" || result.FinishReason != "length" || result.Usage == nil || result.Usage.InputTokens != 8 || result.Usage.TotalTokens != 15 || result.Diagnostics.ActualReasoning != "high" || !result.Diagnostics.ToolsDisabled {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestGeneratorPreservesStructuredNoRetryAndUnknownUsage(t *testing.T) {
	cause := status_error.WithoutRetry(status_error.RuntimeInactivityTimeout)
	generator := New(Config{Runtime: runtimeFunc(func(context.Context, agent.TextRequest) (agent.TextResult, error) {
		return agent.TextResult{Text: "partial must not publish"}, cause
	})})
	result, err := generator.Generate(context.Background(), core.GenerateRequest{})
	var policy interface{ Retryable() bool }
	if !errors.Is(err, status_error.RuntimeInactivityTimeout) || !errors.As(err, &policy) || policy.Retryable() || result.Text != "" || result.Usage != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestGeneratorDoesNotTreatMissingUsageAsZero(t *testing.T) {
	value := int64(2)
	generator := New(Config{Runtime: runtimeFunc(func(context.Context, agent.TextRequest) (agent.TextResult, error) {
		return agent.TextResult{Turn: agent.TurnResult{Outcome: &agent.AgentNativeOutcome{Turn: acpmeta.NativeTurn{
			Usage: &acpmeta.TurnUsage{InputTokens: &value, OutputTokens: &value, Complete: true},
		}}}}, nil
	})})
	result, err := generator.Generate(context.Background(), core.GenerateRequest{})
	if err != nil || result.Usage != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
