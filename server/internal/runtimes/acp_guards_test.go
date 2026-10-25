package runtimes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/coder/acp-go-sdk"
)

// blockingUntilCanceledSink 等待事件交付 Context 超时。
type blockingUntilCanceledSink struct{}

// Accept 模拟不会主动返回的产品事件持久化。
func (*blockingUntilCanceledSink) Accept(ctx context.Context, _ Event) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestACPBridgeRejectsUpdatesWhenBoundedQueueIsFull(t *testing.T) {
	bridge := newACPBridge(
		"session",
		"invocation",
		nil,
		Workspace{},
		ExecutionPolicy{},
		codexACPRuntimeBinding(),
		time.Second,
	)
	for range acpUpdateQueueCapacity {
		if err := bridge.enqueue(acpDelivery{}); err != nil {
			t.Fatalf("fill ACP queue: %v", err)
		}
	}
	if err := bridge.enqueue(acpDelivery{}); !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestACPBridgeBoundsEventDelivery(t *testing.T) {
	bridge := newACPBridge(
		"session",
		"invocation",
		&blockingUntilCanceledSink{},
		Workspace{},
		ExecutionPolicy{},
		codexACPRuntimeBinding(),
		20*time.Millisecond,
	)
	go bridge.run(context.Background())
	bridge.activateDelivery()
	event, err := newEvent(EventTypeTextDelta, map[string]any{"text": "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	if err := bridge.enqueue(acpDelivery{events: []Event{event}}); err != nil {
		t.Fatal(err)
	}
	closeContext, cancelClose := context.WithTimeout(context.Background(), time.Second)
	defer cancelClose()
	if err := bridge.closeDelivery(closeContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("delivery error = %v", err)
	}
}

func TestACPFailureCodeUsesStandardErrorsAndProviderEvidence(t *testing.T) {
	tests := []struct {
		// name 是失败映射场景。
		name string
		// err 是 ACP 或 Provider 返回的错误。
		err error
		// expected 是 NeuxBot 保持的稳定失败码。
		expected FailureCode
	}{
		{name: "cancelled", err: acp.NewRequestCancelled(nil), expected: FailureCodeContextCanceled},
		{name: "auth", err: acp.NewAuthRequired(nil), expected: FailureCodeAuthRequired},
		{name: "provider auth", err: acp.NewInternalError(map[string]any{"errorKind": "authentication_failed"}), expected: FailureCodeAuthRequired},
		{name: "provider billing", err: acp.NewInternalError(map[string]any{"errorKind": "billing_error"}), expected: FailureCodeQuotaExhausted},
		{name: "provider plan upgrade", err: acp.NewInternalError(map[string]any{"errorKind": "plan_upgrade_required"}), expected: FailureCodePlanUpgradeRequired},
		{name: "provider overloaded", err: acp.NewInternalError(map[string]any{"errorKind": "overloaded"}), expected: FailureCodeUpstreamUnavailable},
		{name: "provider unknown billing", err: &acp.RequestError{Code: -32603, Message: "API Error: 402 Insufficient Balance", Data: map[string]any{"errorKind": "unknown"}}, expected: FailureCodeQuotaExhausted},
		{name: "invalid params", err: acp.NewInvalidParams(nil), expected: FailureCodeInvalidRequest},
		{name: "session not found via invalid params", err: acp.NewInvalidParams(map[string]any{"message": "Session not found"}), expected: FailureCodeSessionNotFound},
		{name: "method", err: acp.NewMethodNotFound("session/resume"), expected: FailureCodeProtocolIncompatible},
		{name: "oversized frame", err: errors.New("claude frame too large"), expected: FailureCodeFrameTooLarge},
		{name: "process exit", err: errors.New("codex app-server exited with code 7"), expected: FailureCodeProcessExited},
		{name: "rate limit", err: errors.New("provider rate limit"), expected: FailureCodeRateLimited},
		{name: "upstream", err: errors.New("service unavailable"), expected: FailureCodeUpstreamUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := acpFailureCode(test.err); actual != test.expected {
				t.Fatalf("acpFailureCode() = %q, want %q", actual, test.expected)
			}
		})
	}
}
