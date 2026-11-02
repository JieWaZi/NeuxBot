package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	acp "github.com/JieWaZi/acp-go/sdk"
	"os"
	"testing"
)

func TestNativeAuthenticationFaultKeepsProductClassification(t *testing.T) {
	err := &native.Fault{Code: native.FaultAuthenticationRequired, Stage: "credentials", Message: "credential file unavailable"}
	if code := ClassifyFailure(RuntimeIDCodex, err); code != FailureCodeAuthRequired {
		t.Fatalf("native authentication failure changed product classification: %s", code)
	}
}

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

// realCLIFailureCapture 是真实 CLI 连接本机故障接口后采集的 ACP 终态。
type realCLIFailureCapture struct {
	// CLI 标识实际执行的 CLI。
	CLI string `json:"cli"`
	// Version 固定采集时的程序版本，升级后应重新采集。
	Version string `json:"version"`
	// Scenario 标识认证、限流或上游故障。
	Scenario string `json:"scenario"`
	// Stage 区分创建会话与模型请求阶段。
	Stage string `json:"stage"`
	// HTTPStatus 是本机服务实际注入的状态。
	HTTPStatus int `json:"httpStatus"`
	// RequestCount 证明真实 CLI 曾到达故障接口。
	RequestCount int `json:"requestCount"`
	// AssistantText 用于确认原始错误没有成为普通正文。
	AssistantText string `json:"assistantText"`
	// Error 保留采集到的协议错误，仅将回环端口归一化。
	Error *acp.RequestError `json:"error"`
	// ExpectedCode 是对用户公开的稳定失败类别。
	ExpectedCode FailureCode `json:"expectedCode"`
}

// TestRealCLIFailureCapturesThroughClassifier 用真实输出回放公开故障分类，不以理想化 errorKind 代替原始结构。
func TestRealCLIFailureCapturesThroughClassifier(t *testing.T) {
	data, err := os.ReadFile("testdata/real_cli_errors.json")
	if err != nil {
		t.Fatal(err)
	}
	var captures []realCLIFailureCapture
	if err := json.Unmarshal(data, &captures); err != nil {
		t.Fatal(err)
	}
	coverage := map[string]int{}
	for _, capture := range captures {
		coverage[capture.CLI]++
		t.Run(capture.CLI+"/"+capture.Scenario, func(t *testing.T) {
			if capture.RequestCount == 0 || capture.Version == "" || capture.Error == nil || capture.AssistantText != "" {
				t.Fatalf("缺少真实失败证据：%+v", capture)
			}
			if code := ClassifyFailure(capture.CLI, capture.Error); code != capture.ExpectedCode {
				t.Fatalf("ClassifyFailure = %s, want %s", code, capture.ExpectedCode)
			}
		})
	}
	for _, cli := range []string{RuntimeIDCodex, RuntimeIDClaude, RuntimeIDCursor, RuntimeIDKimi, RuntimeIDPi} {
		if coverage[cli] != 3 {
			t.Fatalf("%s 的真实错误覆盖不完整：%d", cli, coverage[cli])
		}
	}
}

type credentialFieldError struct {
	Token string // Token 模拟不能进入结果 JSON 的底层凭据字段。
}

func (e *credentialFieldError) Error() string { return e.Token }

type credentialJSONError struct {
	Token string // Token 模拟自定义序列化的底层秘密。
}

func (e *credentialJSONError) Error() string { return e.Token }
func (e *credentialJSONError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"credential": e.Token})
}

func TestFaultCauseExcludedFromResultJSONButRemainsUnwrapped(t *testing.T) {
	const secret = "test-secret-credential"
	for _, cause := range []error{&credentialFieldError{Token: secret}, &credentialJSONError{Token: secret}} {
		t.Run(tName(cause), func(t *testing.T) {
			f := &native.Fault{Code: native.FaultPreparation, Stage: "credential", Message: "credential acquisition failed", Cause: cause}
			result := turnResult{Status: native.StatusFailed, Fault: f, DeliveryFault: &native.Fault{Code: native.FaultDelivery, Stage: "observer", Message: "delivery failed", Cause: cause}, CleanupFault: &native.Fault{Code: native.FaultCleanup, Stage: "cleanup", Message: "cleanup failed", Cause: cause}}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte("\"Cause\"")) {
				t.Fatalf("result serialized private cause: %s", data)
			}
			var decoded turnResult
			if err = json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Fault == nil || decoded.Fault.Code != native.FaultPreparation || decoded.Fault.Stage != "credential" || decoded.Fault.Message != "credential acquisition failed" {
				t.Fatalf("safe fault metadata lost: %#v", decoded.Fault)
			}
			if !errors.Is(f, cause) {
				t.Fatal("JSON exclusion broke root-cause unwrapping")
			}
		})
	}
}
func tName(cause error) string {
	switch cause.(type) {
	case *credentialFieldError:
		return "exported field"
	default:
		return "custom JSON"
	}
}
