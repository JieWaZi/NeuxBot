package contract

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// FailureDisplayMessage 从 Go 错误链或 ACP 结构化错误中读取最内层原因。
func FailureDisplayMessage(err error) string {
	if err == nil {
		return ""
	}
	var requestError *acp.RequestError
	if errors.As(err, &requestError) {
		if message := acpFailureDetail(requestError.Data); message != "" {
			return message
		}
		return strings.TrimSpace(requestError.Message)
	}
	for inner := errors.Unwrap(err); inner != nil; inner = errors.Unwrap(err) {
		err = inner
	}
	return strings.TrimSpace(err.Error())
}

// RecordedFailureDisplayMessage 从旧日志中已序列化的 ACP 错误读取原始原因。
func RecordedFailureDisplayMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	if !json.Valid([]byte(message)) {
		return message
	}
	var requestError acp.RequestError
	if err := json.Unmarshal([]byte(message), &requestError); err != nil {
		return message
	}
	return FailureDisplayMessage(&requestError)
}

func acpFailureDetail(data any) string {
	metadata, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"cause", "error", "message", "details"} {
		if detail, ok := metadata[key].(string); ok && strings.TrimSpace(detail) != "" {
			return strings.TrimSpace(detail)
		}
	}
	return ""
}

// ErrorCode 返回 Runtime 包内 status_error 对应的稳定 FailureCode。
func ErrorCode(err error) (FailureCode, bool) {
	switch {
	case errors.Is(err, status_error.BadRequest):
		return FailureCodeInvalidRequest, true
	case errors.Is(err, status_error.RuntimeUnavailable):
		return FailureCodeRuntimeUnavailable, true
	case errors.Is(err, status_error.RuntimeEventQueueOverflow):
		return FailureCodeEventDelivery, true
	case errors.Is(err, status_error.SkillUnavailable):
		return FailureCodeUnsupportedCapability, true
	case errors.Is(err, status_error.RuntimePolicyUnsupported):
		return FailureCodePolicyUnsupported, true
	case errors.Is(err, status_error.RuntimeProtocolIncompatible):
		return FailureCodeProtocolIncompatible, true
	case errors.Is(err, status_error.RuntimeInactivityTimeout):
		return FailureCodeInactivityTimeout, true
	case errors.Is(err, status_error.OperationTimeout):
		return constants.RuntimeFailureCodeRequestTimeout, true
	case errors.Is(err, status_error.OperationCanceled):
		return FailureCodeContextCanceled, true
	default:
		return "", false
	}
}

// runtimeContextStatus 把 Context 终态收敛为可跨 internal 边界传递的稳定错误。
func RuntimeContextStatus(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status_error.OperationTimeout
	}
	return status_error.OperationCanceled
}

func classifyRuntimeErrorText(text string) FailureCode {
	normalized := strings.ToLower(text)
	switch {
	case strings.Contains(normalized, "acp agent unavailable"):
		return FailureCodeRuntimeUnavailable
	case containsAny(
		normalized,
		"frame too large",
		"frame exceeds",
	):
		return FailureCodeFrameTooLarge
	case containsAny(
		normalized,
		"codex app-server exited",
		"claude process exited",
		"child process exited",
		"native acp process exited",
		"pi process exited",
		"cursor interactive process exited",
	):
		return FailureCodeProcessExited
	case containsAny(
		normalized,
		"session not found",
		"session does not exist",
		"unknown session",
		"no rollout found for thread id",
		"chat not found",
		"no chat found",
	):
		return FailureCodeSessionNotFound
	case containsAny(
		normalized,
		"unauthorized",
		"authentication required",
		"not authenticated",
		"not logged in",
		"invalid api key",
		"missing api key",
		`"status":401`,
		`"status_code":401`,
	):
		return FailureCodeAuthRequired
	case containsAny(
		normalized,
		"upgrade your plan",
		"upgrade plan",
	):
		return FailureCodePlanUpgradeRequired
	case containsAny(
		normalized,
		"model not found",
		"unknown model",
		"model unavailable",
	):
		return FailureCodeModelUnavailable
	case containsAny(
		normalized,
		"quota exceeded",
		"usage limit",
		"insufficient balance",
		"billing error",
		"spend limit",
	):
		return FailureCodeQuotaExhausted
	case containsAny(
		normalized,
		"rate limit",
		"rate_limit",
		`"status":429`,
		`"status_code":429`,
	):
		return FailureCodeRateLimited
	case containsAny(
		normalized,
		"temporarily unavailable",
		"service unavailable",
		"upstream unavailable",
		"overloaded",
		`"status":503`,
		`"status_code":503`,
	):
		return FailureCodeUpstreamUnavailable
	default:
		return ""
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

// ClassifyFailure 复用原生故障分类，供执行宿主映射统一业务错误。
func ClassifyFailure(runtimeID string, err error) FailureCode {
	if runtimeID == RuntimeIDCursor {
		return cursorACPFailureCode(err)
	}
	return ACPFailureCode(err)
}

// acpFailureCode 提升 ACP 标准错误，其余错误按 Context 或 Runtime 分类。
func ACPFailureCode(err error) FailureCode {
	if code, ok := ErrorCode(err); ok {
		return code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return constants.RuntimeFailureCodeRequestTimeout
	}
	if errors.Is(err, context.Canceled) {
		return FailureCodeContextCanceled
	}
	var requestError *acp.RequestError
	if errors.As(err, &requestError) {
		if code := acpProviderFailureCode(requestError.Data); code != "" {
			return code
		}
		if code := classifyRuntimeErrorText(err.Error()); code != "" {
			return code
		}
		if code := classifyRuntimeErrorText(acpRequestErrorMessage(requestError.Data)); code != "" {
			return code
		}
		switch requestError.Code {
		case -32800:
			return FailureCodeContextCanceled
		case -32002:
			return FailureCodeSessionNotFound
		case -32000:
			return FailureCodeAuthRequired
		case -32602:
			return FailureCodeInvalidRequest
		case -32601:
			return FailureCodeProtocolIncompatible
		}
	}
	if code := classifyRuntimeErrorText(err.Error()); code != "" {
		return code
	}
	return constants.RuntimeFailureCodeExecutionFailed
}

// acpRequestErrorMessage 提取 ACP Invalid params 等错误中的可读说明。
func acpRequestErrorMessage(data any) string {
	metadata, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	message, _ := metadata["message"].(string)
	return message
}

// acpProviderFailureCode 读取 Runtime Adapter 依据 upstream 发布的开放错误类别。
func acpProviderFailureCode(data any) FailureCode {
	metadata, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	errorKind, _ := metadata[constants.RuntimeACPMetaKeyErrorKind].(string)
	switch errorKind {
	case constants.RuntimeACPProviderErrorKindNetworkError:
		return constants.RuntimeFailureCodeNetworkError
	case constants.RuntimeACPProviderErrorKindTimeout:
		return constants.RuntimeFailureCodeRequestTimeout
	case constants.RuntimeACPProviderErrorKindContextTooLarge:
		return constants.RuntimeFailureCodeContextTooLarge
	case constants.RuntimeACPProviderErrorKindAuthenticationFailed,
		constants.RuntimeACPProviderErrorKindOAuthOrgNotAllowed:
		return FailureCodeAuthRequired
	case constants.RuntimeACPProviderErrorKindPlanUpgradeRequired:
		return FailureCodePlanUpgradeRequired
	case constants.RuntimeACPProviderErrorKindQuotaExhausted:
		return FailureCodeQuotaExhausted
	case constants.RuntimeACPProviderErrorKindBillingError:
		return FailureCodeQuotaExhausted
	case constants.RuntimeACPProviderErrorKindRateLimit:
		return FailureCodeRateLimited
	case constants.RuntimeACPProviderErrorKindOverloaded,
		constants.RuntimeACPProviderErrorKindServerError:
		return FailureCodeUpstreamUnavailable
	case constants.RuntimeACPProviderErrorKindModelNotFound:
		return FailureCodeModelUnavailable
	case constants.RuntimeACPProviderErrorKindInvalidRequest:
		return FailureCodeInvalidRequest
	default:
		return ""
	}
}

// cursorACPFailureCode 优先识别 Cursor 原生错误码，再回退到共享 ACP 分类。
func cursorACPFailureCode(err error) FailureCode {
	var requestError *acp.RequestError
	if errors.As(err, &requestError) {
		metadata, ok := requestError.Data.(map[string]any)
		if ok {
			cursorErrorCode, _ := metadata[constants.RuntimeACPMetaKeyCursorErrorCode].(string)
			if cursorErrorCode == constants.RuntimeACPCursorErrorCodeUpgrade {
				return FailureCodePlanUpgradeRequired
			}
		}
	}
	return ACPFailureCode(err)
}
