package model

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/neux-knowledge/core"
)

// runtimeFunc 提供不启动 CLI 的文本模型端口。
type runtimeFunc func(context.Context, runtimes.TextRequest) runtimes.TextResult

func (run runtimeFunc) GenerateText(ctx context.Context, request runtimes.TextRequest) runtimes.TextResult {
	return run(ctx, request)
}

func TestGeneratorUsesDirectTextAndReleasesEveryAccountLease(t *testing.T) {
	prepared, released, calls := 0, 0, 0
	generator := New(Config{Model: "model-a", Prepare: func(context.Context) (runtimes.RuntimeTarget, runtimes.DetectionResult, func(), error) {
		prepared++
		return runtimes.RuntimeTarget{RuntimeID: "codex", AccountID: "account"}, runtimes.DetectionResult{}, func() { released++ }, nil
	}, Runtime: runtimeFunc(func(_ context.Context, request runtimes.TextRequest) runtimes.TextResult {
		calls++
		if request.Thinking || request.Model != "model-a" || request.MaxTokens != 32768 || request.Temperature != .3 || len(request.Messages) != 2 {
			t.Fatalf("request=%+v", request)
		}
		return runtimes.TextResult{Text: "raw model text", StopReason: "max_tokens", Usage: &runtimes.TurnUsageData{InputTokens: 3, CacheReadTokens: 5, OutputTokens: 7}, ActualReasoning: "low", ReasoningFallback: "thinking_off_unsupported"}
	})})
	request := core.GenerateRequest{Messages: []core.ModelMessage{{Role: "system", Content: "rules"}, {Role: "user", Content: "source"}}, MaxTokens: 32768, Temperature: .3}
	for range 2 {
		result, err := generator.Generate(context.Background(), request)
		if err != nil || result.Text != "raw model text" || result.FinishReason != "length" || result.Usage == nil || result.Usage.InputTokens != 8 || result.Usage.TotalTokens != 15 || result.Diagnostics == nil || result.Diagnostics.ActualReasoning != "low" || result.Diagnostics.ThinkingDisabled {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	if prepared != 2 || released != 2 || calls != 2 {
		t.Fatalf("prepared=%d released=%d calls=%d", prepared, released, calls)
	}
}

func TestGeneratorFailureKeepsStableErrorAndReleasesLease(t *testing.T) {
	released := false
	generator := New(Config{Prepare: func(context.Context) (runtimes.RuntimeTarget, runtimes.DetectionResult, func(), error) {
		return runtimes.RuntimeTarget{}, runtimes.DetectionResult{}, func() { released = true }, nil
	}, Runtime: runtimeFunc(func(context.Context, runtimes.TextRequest) runtimes.TextResult {
		return runtimes.TextResult{Text: "must not publish", Failure: &runtimes.Failure{Code: runtimes.FailureCodePolicyUnsupported, Message: "tool attempted"}}
	})})
	result, err := generator.Generate(context.Background(), core.GenerateRequest{})
	if !errors.Is(err, status_error.RuntimePolicyUnsupported) || result.Text != "" || !released {
		t.Fatalf("result=%+v err=%v released=%v", result, err, released)
	}
}
