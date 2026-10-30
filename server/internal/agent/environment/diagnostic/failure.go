// Package diagnostic 解释原生错误并提供稳定诊断，不拥有业务会话。
package diagnostic

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// FailureCode 是共享 Runtime 失败码枚举的引用。
type FailureCode = constants.RuntimeFailureCode

const (
	// FailureCodeRuntimeNotFound 表示请求的开放 Runtime ID 未注册。
	FailureCodeRuntimeNotFound = constants.RuntimeFailureCodeRuntimeNotFound
	// FailureCodeRuntimeUnavailable 表示 Runtime 可执行文件当前不可调用。
	FailureCodeRuntimeUnavailable = constants.RuntimeFailureCodeRuntimeUnavailable
	// FailureCodeRuntimeIncompatible 表示 Runtime 缺少必要协议能力。
	FailureCodeRuntimeIncompatible = constants.RuntimeFailureCodeRuntimeIncompatible
	// FailureCodeInvalidRequest 表示调用参数缺失或相互冲突。
	FailureCodeInvalidRequest = constants.RuntimeFailureCodeInvalidRequest
	// FailureCodePolicyUnsupported 表示 Runtime 无法准确表达调用方权限策略。
	FailureCodePolicyUnsupported = constants.RuntimeFailureCodePolicyUnsupported
	// FailureCodeSpawnFailed 表示操作系统没有成功创建 Runtime 进程。
	FailureCodeSpawnFailed = constants.RuntimeFailureCodeSpawnFailed
	// FailureCodeProcessExited 表示 Runtime 进程以失败状态结束。
	FailureCodeProcessExited = constants.RuntimeFailureCodeProcessExited
	// FailureCodeProtocolIncompatible 表示事件流缺少必要协议语义。
	FailureCodeProtocolIncompatible = constants.RuntimeFailureCodeProtocolIncompatible
	// FailureCodeContextCanceled 表示调用方 Context 结束了 Invocation。
	FailureCodeContextCanceled = constants.RuntimeFailureCodeContextCanceled
	// FailureCodeEventDelivery 表示 Event Sink 失败或超时。
	FailureCodeEventDelivery = constants.RuntimeFailureCodeEventDelivery
	// FailureCodeFrameTooLarge 表示单个协议帧超过硬限制。
	FailureCodeFrameTooLarge = constants.RuntimeFailureCodeFrameTooLarge
	// FailureCodeInactivityTimeout 表示 Runtime 超过允许时间没有原始进程活动。
	FailureCodeInactivityTimeout = constants.RuntimeFailureCodeInactivityTimeout
	// FailureCodeUnsupportedCapability 表示当前 Runtime 不支持请求的操作。
	FailureCodeUnsupportedCapability = constants.RuntimeFailureCodeUnsupportedCapability
	// FailureCodeInvocationClosed 表示调用已经结束或输入管道已经关闭。
	FailureCodeInvocationClosed = constants.RuntimeFailureCodeInvocationClosed
	// FailureCodeSessionNotFound 表示 Runtime 无法恢复调用方传入的 Session。
	FailureCodeSessionNotFound = constants.RuntimeFailureCodeSessionNotFound
	// FailureCodeAuthRequired 表示 Runtime 缺少有效鉴权凭据。
	FailureCodeAuthRequired = constants.RuntimeFailureCodeAuthRequired
	// FailureCodePlanUpgradeRequired 表示当前套餐不允许使用所选模型。
	FailureCodePlanUpgradeRequired = constants.RuntimeFailureCodePlanUpgradeRequired
	// FailureCodeQuotaExhausted 表示模型服务的可用额度已耗尽。
	FailureCodeQuotaExhausted = constants.RuntimeFailureCodeQuotaExhausted
	// FailureCodeRateLimited 表示模型服务拒绝了当前请求频率。
	FailureCodeRateLimited = constants.RuntimeFailureCodeRateLimited
	// FailureCodeModelUnavailable 表示所选模型不存在或当前不可用。
	FailureCodeModelUnavailable = constants.RuntimeFailureCodeModelUnavailable
	// FailureCodeUpstreamUnavailable 表示模型或 Provider 暂时不可用。
	FailureCodeUpstreamUnavailable = constants.RuntimeFailureCodeUpstreamUnavailable
)

// FailureTransience 是共享 Runtime 失败瞬时性枚举的引用。
type FailureTransience = constants.RuntimeFailureTransience

const (
	// FailureTransienceUnknown 表示 SDK 无法可靠判断失败是否瞬时。
	FailureTransienceUnknown = constants.RuntimeFailureTransienceUnknown
	// FailureTransiencePermanent 表示相同输入不应直接自动重试。
	FailureTransiencePermanent = constants.RuntimeFailureTransiencePermanent
	// FailureTransienceTransient 表示失败可能由短暂上游状态导致。
	FailureTransienceTransient = constants.RuntimeFailureTransienceTransient
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
	if runtimeID == runtimeconfig.RuntimeIDCursor {
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
