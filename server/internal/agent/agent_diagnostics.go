package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	acp "github.com/JieWaZi/acp-go-sdk"
	"github.com/JieWaZi/acp-go/pkg/acpclient"
	native "github.com/JieWaZi/acp-go/pkg/agentclient"
	"github.com/JieWaZi/acp-go/pkg/cursor"
	"github.com/JieWaZi/acp-go/pkg/qoder"
	"go.opentelemetry.io/otel/trace"
)

const configureCodexTurnOperation = "session/configure_codex_turn"

// logNativeSessionDecision separates provider-session preparation from input admission.
func logNativeSessionDecision(t *Turn, provider string, previous *NativeBinding, action, reason string, err error) {
	previousRef, sessionRef := "-", "-"
	if previous != nil && previous.Session.SessionID != "" {
		previousRef = configurationDigest(previous.Session)[:16]
	}
	if t.binding.Session.SessionID != "" {
		sessionRef = configurationDigest(t.binding.Session)[:16]
	}
	result, code, handoff := "ready", "-", "none"
	if err != nil {
		result, code = "failed", util.ErrorCode(err, "unknown")
	} else if seed := t.binding.Handoff; seed != nil && seed.Claim == nil {
		if seed.Summary != nil {
			handoff = "summary"
		} else if len(seed.History) != 0 {
			handoff = "recent"
		}
	}
	logx.Infof("native session decision: trace_id=%s chat_id=%s execution_id=%s provider=%s action=%s reason=%s result=%s error_code=%s previous_session_ref=%s session_ref=%s handoff=%s",
		trace.SpanContextFromContext(t.ctx).TraceID(), t.thread.chatID, t.input.ExecutionID, provider, action, reason, result, code, previousRef, sessionRef, handoff)
}

func nativePreparationError(ctx context.Context, operation string, err error, fallback status_error.StatusError) error {
	if err == nil {
		return nil
	}
	mapped := safeAgentError(err, fallback)
	logNativePreparationFailure(ctx, operation, "", err, mapped)
	return mapped
}

// logNativePreparationFailure projects protocol and process evidence without logging raw errors.
func logNativePreparationFailure(ctx context.Context, operation, knownReason string, err, mapped error) {
	code := util.ErrorCode(mapped, "unknown")
	switch {
	case errors.Is(mapped, context.DeadlineExceeded):
		code = status_error.OperationTimeout.Key()
	case errors.Is(mapped, context.Canceled):
		code = status_error.OperationCanceled.Key()
	}
	fields := []string{
		"trace_id=" + trace.SpanContextFromContext(ctx).TraceID().String(),
		"operation=" + operation,
		"error_code=" + code,
	}
	var rpc *acp.RequestError
	var kind, detail string
	detailWithheld := false
	if errors.As(err, &rpc) && rpc != nil {
		fields = append(fields, "native_rpc_code="+strconv.Itoa(rpc.Code))
		kind = safeNativeErrorKind(rpc)
		if kind != "" {
			fields = append(fields, "native_kind="+kind)
		}
		if operation == configureCodexTurnOperation {
			knownReason = codexTurnConfigurationCause(rpc)
		}
		if knownReason == "" && (operation == acp.AgentMethodInitialize || strings.HasPrefix(operation, "session/")) {
			public := status_error.PublicMessage(mapped)
			detail = safeNativePreparationDetail(public)
			detailWithheld = public != "" && detail == ""
		}
	}
	if knownReason == "" {
		var processField string
		knownReason, processField = nativePreparationCause(err, rpc, kind, detail, detailWithheld)
		if processField != "" {
			fields = append(fields, processField)
		}
	}
	fields = append(fields, "reason="+knownReason)
	if detail != "" {
		fields = append(fields, "detail="+strconv.Quote(detail))
	}
	logx.Warnf("native agent operation failed: %s", strings.Join(fields, " "))
}

func safeNativePreparationDetail(message string) string {
	// A public RPC rejection can still contain caller text. Persist only exact
	// protocol and locked-adapter messages with no interpolated values.
	message = native.NativeFailureMessage(message)
	switch message {
	case "Authentication required",
		"ACP agent did not verify restored history",
		"ACP agent session is absent from its native directory",
		"ACP agent rejected the request parameters",
		"ephemeral is supported only when starting a new thread",
		"invalid Codex permission mode",
		"model and reasoning effort cannot be empty",
		"Codex workspace directories must be absolute",
		"Codex model is unavailable",
		"Codex reasoning effort is unavailable for the model",
		"Codex Fast is unavailable for the model",
		"cannot resume an unloaded multi-agent v2 sub-agent through its parent; resume the parent first, or use thread/read to inspect it":
		return message
	}
	return ""
}

func codexTurnConfigurationCause(rpc *acp.RequestError) string {
	if rpc.Code != -32602 {
		return ""
	}
	// ConfigureTurn creates these exact values locally as SDK error data. Only
	// their classification is logged, never the data itself.
	switch value, _ := rpc.Data.(string); value {
	case "Codex model is unavailable":
		return "model_unavailable"
	case "Codex reasoning effort is unavailable for the model":
		return "reasoning_effort_unavailable"
	case "Codex Fast is unavailable for the model":
		return "fast_mode_unavailable"
	}
	return ""
}

func nativePreparationCause(err error, rpc *acp.RequestError, kind, detail string, detailWithheld bool) (reason, processField string) {
	var exit *exec.ExitError
	var syntax *json.SyntaxError
	var wrongType *json.UnmarshalTypeError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded", ""
	case errors.Is(err, context.Canceled):
		return "canceled", ""
	case errors.As(err, &exit) && exit != nil && exit.ProcessState != nil:
		if state, ok := exit.Sys().(syscall.WaitStatus); ok && state.Signaled() {
			return "process_signaled", "process_signal=" + strconv.Itoa(int(state.Signal()))
		}
		return "process_exited", "process_exit_code=" + strconv.Itoa(exit.ExitCode())
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected_eof", ""
	case errors.Is(err, io.EOF):
		return "eof", ""
	case errors.Is(err, syscall.EPIPE), errors.Is(err, io.ErrClosedPipe):
		return "broken_pipe", ""
	case errors.Is(err, os.ErrPermission):
		return "permission_denied", ""
	case errors.Is(err, os.ErrNotExist):
		return "file_not_found", ""
	case errors.As(err, &syntax), errors.As(err, &wrongType):
		return "invalid_native_response", ""
	case errors.Is(err, acpclient.ErrRestartRequired):
		return "connection_restart_required", ""
	case errors.Is(err, acpclient.ErrBusy):
		return "native_busy", ""
	case errors.Is(err, qoder.ErrModelUnavailable):
		return "model_unavailable", ""
	case errors.Is(err, qoder.ErrContextWindowUnsupported):
		return "context_window_unsupported", ""
	case errors.Is(err, cursor.ErrQuotaUnauthorized), errors.Is(err, cursor.ErrQuotaCredential):
		return "authentication_required", ""
	case errors.Is(err, cursor.ErrPermissionModeUnsupported):
		return "native_policy_unsupported", ""
	}
	if kind != "" && kind != "native_execution_failed" {
		return kind, ""
	}
	// Only upstream status provides cause evidence. The operation's own
	// fallback may name a domain without having proven that it caused failure.
	switch util.ErrorCode(status_error.PublicError(err), "") {
	case status_error.RuntimeAuthRequired.Key():
		return "authentication_required", ""
	case status_error.RuntimeSessionNotFound.Key():
		return "session_missing", ""
	case status_error.RuntimePolicyUnsupported.Key():
		return "native_policy_unsupported", ""
	case status_error.RuntimeProtocolIncompatible.Key():
		return "native_protocol_incompatible", ""
	case status_error.OperationCanceled.Key():
		return "canceled", ""
	case status_error.OperationTimeout.Key():
		return "deadline_exceeded", ""
	case status_error.RuntimeQuotaExhausted.Key():
		return "quota_exhausted", ""
	case status_error.RuntimeRateLimited.Key():
		return "rate_limit", ""
	}
	if rpc == nil {
		if cause := singleNativeFailure(err); cause != nil {
			if reason, processField := nativePlainPreparationCause(cause.Error()); reason != "" {
				return reason, processField
			}
		}
	} else {
		switch rpc.Code {
		case -32000:
			return "authentication_required", ""
		case -32002:
			return "session_missing", ""
		case -32601:
			return "native_method_unsupported", ""
		case -32602:
			return "invalid_native_parameters", ""
		case -32700:
			return "invalid_native_response", ""
		case -32800:
			return "canceled", ""
		}
	}
	if detail != "" {
		return "native_request_rejected", ""
	}
	if detailWithheld {
		return "native_detail_withheld", ""
	}
	return "native_detail_unavailable", ""
}

// nativePlainPreparationCause recognizes fixed forms of the locked adapter.
// Only enumerated reasons and parsed integers are logged from untyped errors.
func nativePlainPreparationCause(message string) (reason, processField string) {
	if raw, ok := strings.CutPrefix(message, "ACP agent CLI exited with code "); ok {
		if code, err := strconv.Atoi(raw); err == nil {
			return "process_exited", "process_exit_code=" + strconv.Itoa(code)
		}
	}
	if raw, ok := strings.CutPrefix(message, "ACP agent CLI exited on signal "); ok {
		switch raw {
		case "killed", "terminated", "interrupt", "segmentation fault", "broken pipe", "abort trap", "aborted":
			return "process_signaled", "process_signal=" + strconv.Quote(raw)
		}
	}
	if strings.HasPrefix(message, "ACP agent ") && strings.HasSuffix(message, " transport or response failed") {
		return "native_transport_or_response_failed", ""
	}
	switch message {
	case "Codex session already has an operation in progress":
		return "native_busy", ""
	case "invalid Codex native frame", "invalid Claude stream-json frame":
		return "invalid_native_response", ""
	case "Codex returned no native result":
		return "native_result_missing", ""
	case "Codex returned an invalid thread response", "Codex returned an invalid thread identity":
		return "invalid_native_session_reply", ""
	case "Codex opened an unexpected working directory":
		return "native_workspace_mismatch", ""
	case "Codex app server stopped answering local requests":
		return "native_unresponsive", ""
	}
	return "", ""
}

// singleNativeFailure keeps a single attributable failure through wrappers.
func singleNativeFailure(err error) error {
	for err != nil {
		switch wrapped := err.(type) {
		case interface{ Unwrap() []error }:
			causes := wrapped.Unwrap()
			if len(causes) != 1 {
				return nil
			}
			err = causes[0]
		case interface{ Unwrap() error }:
			err = wrapped.Unwrap()
		default:
			return err
		}
	}
	return nil
}
