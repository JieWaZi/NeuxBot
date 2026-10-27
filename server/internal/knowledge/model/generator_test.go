package model

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimehost"
	"github.com/JieWaZi/neux-knowledge/core"
)

// runtimeFunc 提供不启动原生进程的宿主结果端口。
type runtimeFunc func(context.Context, runtimehost.TextRequest) (runtimehost.TextResult, error)

func (run runtimeFunc) GenerateText(ctx context.Context, request runtimehost.TextRequest) (runtimehost.TextResult, error) {
	return run(ctx, request)
}

func TestGeneratorPreservesMessagesAndMapsResultExactlyOnce(t *testing.T) {
	calls := 0
	generator := New(Config{RuntimeID: "codex-profile", AccountID: "account", Model: "model-a", Reasoning: "high", Runtime: runtimeFunc(func(_ context.Context, request runtimehost.TextRequest) (runtimehost.TextResult, error) {
		calls++
		if request.RuntimeID != "codex-profile" || request.AccountID != "account" || request.Model != "model-a" || request.Reasoning != "high" || request.Thinking || request.MaxTokens != 32768 || request.Temperature != .3 || len(request.Messages) != 2 || request.Messages[0].Content != "rules" || request.Messages[1].Content != "source" {
			t.Fatalf("request=%+v", request)
		}
		return runtimehost.TextResult{Text: "raw model text", StopReason: "max_tokens", Usage: &runtimehost.Usage{InputTokens: 3, CacheReadTokens: 5, OutputTokens: 7}, ActualReasoning: "low", ReasoningFallback: "thinking_off_unsupported"}, nil
	})})
	result, err := generator.Generate(context.Background(), core.GenerateRequest{Messages: []core.ModelMessage{{Role: "system", Content: "rules"}, {Role: "user", Content: "source"}}, MaxTokens: 32768, Temperature: .3})
	if err != nil || calls != 1 || result.Text != "raw model text" || result.FinishReason != "length" || result.Usage == nil || result.Usage.InputTokens != 8 || result.Usage.TotalTokens != 15 || result.Diagnostics.ActualReasoning != "low" || result.Diagnostics.ToolsDisabled {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestGeneratorPreservesStructuredNoRetryAndUnknownUsage(t *testing.T) {
	cause := status_error.WithoutRetry(status_error.RuntimeInactivityTimeout)
	generator := New(Config{Runtime: runtimeFunc(func(context.Context, runtimehost.TextRequest) (runtimehost.TextResult, error) {
		return runtimehost.TextResult{Text: "partial must not publish"}, cause
	})})
	result, err := generator.Generate(context.Background(), core.GenerateRequest{})
	var policy interface{ Retryable() bool }
	if !errors.Is(err, status_error.RuntimeInactivityTimeout) || !errors.As(err, &policy) || policy.Retryable() || result.Text != "" || result.Usage != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
