package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

func TestNativeExecutionFailureKeepsSafeBusinessStatus(t *testing.T) {
	cases := []struct {
		// name 标识失败发生的边界。
		name string
		// result 是需要投影的原生执行证据。
		result agent.TurnResult
		// status 是业务执行的公开状态。
		status constants.ExecutionStatus
		// code 是用于本地化的稳定应用错误身份。
		code string
		// failureCode 是独立保留的原生技术失败分类。
		failureCode constants.RuntimeFailureCode
	}{
		{"auth", agent.TurnResult{OperationError: status_error.RuntimeAuthRequired}, constants.ExecutionStatusFailed, "RuntimeAuthRequired", constants.RuntimeFailureCodeAuthRequired},
		{"delivery", agent.TurnResult{DeliveryError: errors.New("private diagnostic"), Outcome: &agent.AgentNativeOutcome{Turn: acpmeta.NativeTurn{Status: acpmeta.NativeTurnCompleted}}}, constants.ExecutionStatusFailed, "RuntimeExecutionFailed", constants.RuntimeFailureCodeEventDelivery},
		{"cancel", agent.TurnResult{OperationError: context.Canceled}, constants.ExecutionStatusCanceled, "OperationCanceled", constants.RuntimeFailureCodeContextCanceled},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := nativeExecutionOutcome(test.result)
			if got.status != test.status || got.code != test.code || got.failureCode != test.failureCode || got.message != "" || got.messageSource != constants.ErrorMessageSourceLocalized {
				t.Fatalf("unexpected public failure: %+v", got)
			}
		})
	}
}
