package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

func TestNativeExecutionFailureKeepsSafeBusinessStatus(t *testing.T) {
	cases := []struct {
		// name identifies the failure source.
		name string
		// result is the runtime evidence to project.
		result agent.TurnResult
		// status is the public business status.
		status constants.ExecutionStatus
		// code is the public failure classification.
		code constants.RuntimeFailureCode
	}{
		{"auth", agent.TurnResult{OperationError: status_error.RuntimeAuthRequired}, constants.ExecutionStatusFailed, constants.RuntimeFailureCodeAuthRequired},
		{"delivery", agent.TurnResult{DeliveryError: errors.New("private diagnostic"), Outcome: &agent.AgentNativeOutcome{Turn: acpmeta.NativeTurn{Status: acpmeta.NativeTurnCompleted}}}, constants.ExecutionStatusFailed, constants.RuntimeFailureCodeEventDelivery},
		{"cancel", agent.TurnResult{OperationError: context.Canceled}, constants.ExecutionStatusCanceled, constants.RuntimeFailureCodeContextCanceled},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := nativeExecutionOutcome(test.result)
			if got.status != test.status || got.code != string(test.code) || got.message == "private diagnostic" {
				t.Fatalf("unexpected public failure: %+v", got)
			}
		})
	}
}
