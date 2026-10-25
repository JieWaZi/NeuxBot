package runtimes

import (
	"errors"
	"fmt"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/coder/acp-go-sdk"
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
	if actual := acpFailureCode(err); actual != FailureCodeSessionNotFound {
		t.Fatalf("acpFailureCode(%v) = %q, want %q", err, actual, FailureCodeSessionNotFound)
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

func TestFailureTransienceSeparatesQuotaFromTransientRejection(t *testing.T) {
	if failureTransience(FailureCodeRateLimited) != FailureTransienceTransient ||
		failureTransience(FailureCodeUpstreamUnavailable) != FailureTransienceTransient {
		t.Fatal("provider failures must remain transient")
	}
	if failureTransience(FailureCodeQuotaExhausted) != FailureTransiencePermanent ||
		failureTransience(FailureCodePlanUpgradeRequired) != FailureTransiencePermanent ||
		failureTransience(FailureCodeModelUnavailable) != FailureTransiencePermanent {
		t.Fatal("configuration failures must remain permanent")
	}
	if failureTransience(FailureCodeSessionNotFound) != FailureTransiencePermanent {
		t.Fatal("missing session must remain permanent for the current attempt")
	}
}
