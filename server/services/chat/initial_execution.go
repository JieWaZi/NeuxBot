package chat

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// InitialExecutionState 是产品入口对账首次接纳与运行结果的窄权威视图。
type InitialExecutionState struct {
	// Chat 是当前 Chat 的身份及待处理交互事实。
	Chat models.Chat
	// CreateRequestID 是首发时提交的稳定请求身份。
	CreateRequestID string
	// InitialExecution 是首次 Execution 的当前或最终状态，后续续聊不覆盖它。
	InitialExecution *models.Execution
}

// ReadExecution 从同一权威重放结果读取指定回合，不把后续回合误当成首次接纳。
func (m *service) ReadExecution(_ context.Context, chatID, executionID string) (models.Execution, error) {
	_, state, err := m.loadChatState(chatID)
	if err != nil {
		return models.Execution{}, err
	}
	value := state.executions[executionID]
	if value == nil {
		return models.Execution{}, status_error.ExecutionNotFound
	}
	result := *value
	if value.CompletedAt != nil {
		completed := *value.CompletedAt
		result.CompletedAt = &completed
	}
	return result, nil
}

// ReadInitialExecution 复用 Chat 权威重放，仅返回首次执行和交互状态。
func (m *service) ReadInitialExecution(_ context.Context, chatID string) (InitialExecutionState, error) {
	_, state, err := m.loadChatState(chatID)
	if err != nil {
		return InitialExecutionState{}, err
	}
	return InitialExecutionState{Chat: state.chat, CreateRequestID: state.createRequestID, InitialExecution: state.initialExecution}, nil
}

// ExecutionOutcome 是指定执行及其最后一次已持久化 Runtime 调用的窄事实视图。
// 调用方必须同时检查 Execution.Status 和 TerminationReason；normal 不能覆盖交付失败。
type ExecutionOutcome struct {
	// Execution 是该次执行的权威生命周期事实。
	Execution models.Execution
	// InvocationID 是该次执行最后一个已持久化调用标识；无调用事实时为空。
	InvocationID string
	// StopReason 是该调用的原始协议停止原因。
	StopReason string
	// TerminationReason 是该调用的归一化终止结论，缺少证据时为 unknown。
	TerminationReason constants.RuntimeTerminationReason
	// FailureCode 是该调用保留的稳定失败码。
	FailureCode constants.RuntimeFailureCode
	// FailureStage 是该调用失败发生的技术阶段。
	FailureStage string
	// FailureMessage 是该调用不含秘密值的原始失败说明。
	FailureMessage string
}

// ReadExecutionOutcome 按精确执行身份读取可恢复终态，不从最新消息推测执行完成。
func (m *service) ReadExecutionOutcome(_ context.Context, chatID, executionID string) (ExecutionOutcome, error) {
	records, state, err := m.loadChatState(chatID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	execution := state.executions[executionID]
	if execution == nil {
		return ExecutionOutcome{}, status_error.ExecutionNotFound
	}
	result := ExecutionOutcome{Execution: *execution, TerminationReason: constants.RuntimeTerminationReasonUnknown}
	if execution.CompletedAt != nil {
		completedAt := *execution.CompletedAt
		result.Execution.CompletedAt = &completedAt
	}
	invocations, err := eventlog.ReplayInvocations(records)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	for _, invocation := range invocations {
		if invocation.ExecutionID != executionID {
			continue
		}
		result.InvocationID = invocation.ID
		result.StopReason = invocation.Facts.StopReason
		result.TerminationReason = invocation.Facts.TerminationReason
		result.FailureCode = invocation.Facts.FailureCode
		result.FailureStage = invocation.Facts.FailureStage
		result.FailureMessage = invocation.Facts.FailureMessage
	}
	return result, nil
}
