package runtimes

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	acp "github.com/coder/acp-go-sdk"
)

func TestACPResultPreservesTerminationAndFailure(t *testing.T) {
	cases := []struct {
		// raw 是协议实际返回的原始停止原因。
		raw string
		// want 是宿主可用于判断完整性的终止结论。
		want string
	}{{"end_turn", "normal"}, {"max_tokens", "truncated"}, {"max_turn_requests", "turn_limit"}, {"refusal", "refusal"}, {"cancelled", "cancelled"}, {"future_reason", "unknown"}, {"", "unknown"}}
	for _, test := range cases {
		t.Run(test.raw, func(t *testing.T) {
			request := acpRunRequest{}
			bridge := &acpBridge{binding: defaultACPRuntimeBindings()[RuntimeIDCodex]}
			prompt := acpPromptResult{response: acp.PromptResponse{StopReason: acp.StopReason(test.raw)}}
			result := acpResult(request, "session", prompt, bridge, nil, nil)
			if result.StopReason != test.raw || string(result.TerminationReason) != test.want {
				t.Fatalf("termination = %q/%q, want %q/%q", result.StopReason, result.TerminationReason, test.raw, test.want)
			}
			failed := acpResult(request, "session", prompt, bridge, context.DeadlineExceeded, nil)
			if failed.StopReason != test.raw || string(failed.TerminationReason) != test.want || failed.Failure == nil || failed.Failure.Code != constants.RuntimeFailureCodeRequestTimeout || failed.Failure.Message == "" {
				t.Fatalf("failure erased termination or diagnostics: %+v", failed)
			}
			if test.raw == "cancelled" && (result.Failure == nil || result.Failure.Code != FailureCodeContextCanceled) {
				t.Fatalf("cancelled failure lost: %+v", result)
			}
		})
	}
}

func TestACPInitializeDoesNotAdvertiseUnimplementedSchemaOutput(t *testing.T) {
	catalog := acpInitializeCatalog(acpRuntimeBinding{}, acp.InitializeResponse{})
	if catalog.Capabilities[CapabilityStructuredOutput] {
		t.Fatal("ACP advertised schema output without a schema request contract")
	}
}

func TestRuntimeFailureWithoutPromptDoesNotInventTermination(t *testing.T) {
	early := failedRunResult(FailureCodeInvalidRequest, "validate", "invalid input")
	if early.TerminationReason != constants.RuntimeTerminationReasonUnknown || early.StopReason != "" {
		t.Fatalf("early failure invented a prompt result: %+v", early)
	}
	acpFailure := acpFailedResult(acpRunRequest{}, constants.RuntimeACPFailureStageConnect, context.DeadlineExceeded)
	if acpFailure.TerminationReason != constants.RuntimeTerminationReasonUnknown || acpFailure.StopReason != "" || acpFailure.Failure == nil {
		t.Fatalf("ACP startup failure invented a prompt result: %+v", acpFailure)
	}
}
