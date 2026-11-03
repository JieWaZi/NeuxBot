package execution

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	acp "github.com/JieWaZi/acp-go/sdk"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func TestExecutionResultStatusPublishesInnermostRuntimeFailure(t *testing.T) {
	result := agentapi.Result{Failure: &agentapi.Failure{
		Code:           agentapi.FailureCodePlanUpgradeRequired,
		Stage:          constants.RuntimeACPFailureStagePrompt,
		Message:        "start Runtime: send prompt: API Error: 402 Insufficient Balance",
		DisplayMessage: "API Error: 402 Insufficient Balance",
		Transience:     agentapi.FailureTransiencePermanent,
	}}

	status, errorCode, errorMessage := executionResultStatus(result)
	if status != constants.ExecutionStatusFailed {
		t.Fatalf("status = %q, want failed", status)
	}
	if errorCode != string(agentapi.FailureCodePlanUpgradeRequired) {
		t.Fatalf("errorCode = %q, want plan upgrade", errorCode)
	}
	if errorMessage != "API Error: 402 Insufficient Balance" {
		t.Fatalf("errorMessage = %q, want innermost failure", errorMessage)
	}
}

// TestRuntimeStartErrorUsesSameReadableCauseAsExecution 验证执行前配置失败与已接纳执行展示相同的底层原因。
func TestRuntimeStartErrorUsesSameReadableCauseAsExecution(t *testing.T) {
	for _, test := range []struct {
		// name 标识不同原生 ACP 错误字段的回归场景。
		name string
		// data 是 CLI 通过标准 ACP 返回的原始错误数据。
		data map[string]any
		// want 是用户应直接看到的错误文字。
		want string
	}{
		{"WorkBuddy details", map[string]any{"details": "Current model does not support disabling thinking (modelId=step-5-preview)"}, "Current model does not support disabling thinking (modelId=step-5-preview)"},
		{"Codex error", map[string]any{"error": "The requested model does not support this reasoning effort"}, "The requested model does not support this reasoning effort"},
		{"native ACP message", map[string]any{"message": "Requested configuration is not supported"}, "Requested configuration is not supported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := acp.NewInternalError(test.data)
			result := agentapi.Result{Failure: &agentapi.Failure{
				Code:           constants.RuntimeFailureCodeExecutionFailed,
				Stage:          constants.RuntimeACPFailureStageSessionConfig,
				Message:        upstream.Error(),
				DisplayMessage: agentapi.FailureDisplayMessage(upstream),
			}}
			err := runtimeStartError(result)
			var statusError *statuserror.StatusErr
			if !errors.As(err, &statusError) || statusError.Key() != status_error.RuntimeExecutionFailed.Key() || statusError.Params["Detail"] != test.want {
				t.Fatalf("start error = %#v, want readable cause %q", statusError, test.want)
			}
			_, _, message := executionResultStatus(result)
			if message != test.want {
				t.Fatalf("accepted execution cause = %q, want %q", message, test.want)
			}
			if result.Failure.Message != upstream.Error() {
				t.Fatal("display extraction changed the original diagnostic")
			}
		})
	}
}

// TestRuntimeStartErrorRetainsBusinessErrorsAndPlainFallback 验证已有业务错误和无结构化信息的原文仍保持语义。
func TestRuntimeStartErrorRetainsBusinessErrorsAndPlainFallback(t *testing.T) {
	for _, test := range []struct {
		// code 是执行前明确识别的稳定失败类别。
		code agentapi.FailureCode
		// want 是 API 已有的业务状态错误。
		want statuserror.CommonError
	}{
		{agentapi.FailureCodeSessionNotFound, status_error.RuntimeSessionNotFound},
		{agentapi.FailureCodePolicyUnsupported, status_error.RuntimePolicyUnsupported},
		{agentapi.FailureCodeAuthRequired, status_error.RuntimeAuthRequired},
		{agentapi.FailureCodeRuntimeUnavailable, status_error.RuntimeUnavailable},
	} {
		if err := runtimeStartError(agentapi.Result{Failure: &agentapi.Failure{Code: test.code, Message: "diagnostic"}}); !errors.Is(err, test.want) {
			t.Fatalf("%s: start error = %v, want %v", test.code, err, test.want)
		}
	}
	result := agentapi.Result{Failure: &agentapi.Failure{Code: constants.RuntimeFailureCodeExecutionFailed, Message: "  native process refused the configuration  "}}
	var statusError *statuserror.StatusErr
	if err := runtimeStartError(result); !errors.As(err, &statusError) || statusError.Params["Detail"] != "native process refused the configuration" {
		t.Fatalf("plain fallback = %#v", statusError)
	}
	if err := runtimeStartError(agentapi.Result{}); !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("missing failure = %v, want runtime unavailable", err)
	}
}

// 人工等待期限在原始调用事实与执行终态中保留同一个明确原因，不能退化为模型超时。
func TestHumanWaitTimeoutInvocationAndExecutionShareCause(t *testing.T) {
	for _, code := range []native.FaultCode{native.FaultInputTimeout, native.FaultApprovalTimeout} {
		t.Run(string(code), func(t *testing.T) {
			message := "waiting for user input timed out"
			if code == native.FaultApprovalTimeout {
				message = "waiting for approval timed out"
			}
			fault := &native.Fault{Code: code, Stage: "interaction", Message: message, Cause: context.DeadlineExceeded}
			result := agentapi.Result{Failure: &agentapi.Failure{Code: agentapi.ClassifyFailure(agentapi.RuntimeIDWorkBuddy, fault), Stage: constants.RuntimeACPFailureStagePrompt, Message: fault.Error(), DisplayMessage: agentapi.FailureDisplayMessage(fault), Transience: agentapi.FailureTransiencePermanent}}
			store, err := eventlog.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			chat := models.Chat{ID: "53535353535353535353535353535353"}
			if _, err := store.Create(chat.ID, []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated}}); err != nil {
				t.Fatal(err)
			}
			manager := &service{journal: &runtimeEventTestJournal{store: store}}
			if err := manager.recordInvocationResult(chat, t.TempDir(), models.Execution{ID: "execution"}, result, 1, constants.InvocationRecoveryInitial, constants.InvocationTriggerUnspecified); err != nil {
				t.Fatal(err)
			}
			records, err := store.ReadAll(chat.ID)
			if err != nil {
				t.Fatal(err)
			}
			var invocation eventlog.InvocationFinishedData
			if len(records) != 2 || json.Unmarshal(records[1].Data, &invocation) != nil {
				t.Fatalf("missing invocation: %+v", records)
			}
			status, errorCode, errorMessage := executionResultStatus(result)
			if status != constants.ExecutionStatusFailed || errorCode != string(invocation.FailureCode) || errorMessage != message || !strings.Contains(invocation.FailureMessage, message) {
				t.Fatalf("inconsistent facts: invocation=%+v execution=%s/%s/%s", invocation, status, errorCode, errorMessage)
			}
			if runtimeFailureRetryable(result) {
				t.Fatal("human waiting deadline permitted replay")
			}
		})
	}
}
