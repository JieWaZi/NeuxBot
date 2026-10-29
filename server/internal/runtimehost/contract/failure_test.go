package contract

import (
	"errors"
	"fmt"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go/sdk"
)

func TestClassifyRuntimeErrorTextUsesStableACPFailureCodes(t *testing.T) {
	tests := map[string]FailureCode{
		"claude frame too large":              FailureCodeFrameTooLarge,
		"codex app-server exited":             FailureCodeProcessExited,
		"claude process exited":               FailureCodeProcessExited,
		"session not found":                   FailureCodeSessionNotFound,
		"authentication required":             FailureCodeAuthRequired,
		"provider rate limit":                 FailureCodeRateLimited,
		"API Error: 402 Insufficient Balance": FailureCodeQuotaExhausted,
		"Please upgrade your plan":            FailureCodePlanUpgradeRequired,
		"model not found":                     FailureCodeModelUnavailable,
		"service unavailable":                 FailureCodeUpstreamUnavailable,
		"unclassified runtime error":          "",
	}
	for message, expected := range tests {
		if actual := classifyRuntimeErrorText(message); actual != expected {
			t.Fatalf("classifyRuntimeErrorText(%q) = %q, want %q", message, actual, expected)
		}
	}
}

func TestCodexResumeMissingRolloutIsSessionNotFound(t *testing.T) {
	err := acp.NewInternalError(map[string]any{
		"error": "resuming codex thread abc: codex app-server error -32600: no rollout found for thread id abc",
	})
	if actual := ACPFailureCode(err); actual != FailureCodeSessionNotFound {
		t.Fatalf("ACPFailureCode(%v) = %q, want %q", err, actual, FailureCodeSessionNotFound)
	}
}

func TestFailureDisplayMessageUsesErrorChainAndACPFields(t *testing.T) {
	wrapped := fmt.Errorf("启动运行时: %w", fmt.Errorf("发送请求: %w", errors.New("API Error: 402 Insufficient Balance")))
	if actual := FailureDisplayMessage(wrapped); actual != "API Error: 402 Insufficient Balance" {
		t.Fatalf("wrapped error = %q", actual)
	}
	request := fmt.Errorf("发送请求: %w", acp.NewInternalError(map[string]any{
		"error": "认证失败: 令牌已失效",
	}))
	if actual := FailureDisplayMessage(request); actual != "认证失败: 令牌已失效" {
		t.Fatalf("ACP detail = %q", actual)
	}
	if actual := FailureDisplayMessage(acp.NewAuthRequired(nil)); actual != "Authentication required" {
		t.Fatalf("ACP message = %q", actual)
	}
}

func TestRecordedFailureDisplayMessagePreservesUnstructuredText(t *testing.T) {
	if actual := RecordedFailureDisplayMessage("API Error: 402 Insufficient Balance"); actual != "API Error: 402 Insufficient Balance" {
		t.Fatalf("plain message = %q", actual)
	}
	if actual := RecordedFailureDisplayMessage(`{"code":-32603,"message":"Internal error","data":{"error":"令牌已失效"}}`); actual != "令牌已失效" {
		t.Fatalf("recorded ACP detail = %q", actual)
	}
}

func TestErrorCodeRecognizesStatusErrors(t *testing.T) {
	if code, ok := ErrorCode(status_error.RuntimePolicyUnsupported); !ok ||
		code != FailureCodePolicyUnsupported {
		t.Fatalf("ErrorCode(RuntimePolicyUnsupported) = (%q, %v), want (%q, true)",
			code, ok, FailureCodePolicyUnsupported)
	}
	if code, ok := ErrorCode(status_error.BadRequest); !ok || code != FailureCodeInvalidRequest {
		t.Fatalf("ErrorCode(BadRequest) = (%q, %v), want (%q, true)",
			code, ok, FailureCodeInvalidRequest)
	}
	if code, ok := ErrorCode(status_error.RuntimeInactivityTimeout); !ok ||
		code != FailureCodeInactivityTimeout {
		t.Fatalf("ErrorCode(RuntimeInactivityTimeout) = (%q, %v), want (%q, true)",
			code, ok, FailureCodeInactivityTimeout)
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
		{name: "host queue overflow", err: status_error.RuntimeEventQueueOverflow, expected: FailureCodeEventDelivery},
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
			if actual := ACPFailureCode(test.err); actual != test.expected {
				t.Fatalf("ACPFailureCode() = %q, want %q", actual, test.expected)
			}
		})
	}
}
