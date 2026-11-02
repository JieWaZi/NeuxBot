package chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// InitialExecutionState 是产品入口对账首次接纳与运行结果的窄权威视图。
type InitialExecutionState struct {
	// Chat 是当前 Chat 的身份及待处理交互事实。
	Chat models.Chat
	// CreateRequestID 是首发时提交的稳定请求身份。
	CreateRequestID string
	// CreateRequestHash 是首发规范化业务内容摘要。
	CreateRequestHash string
	// InitialExecution 是首次 Execution 的当前或最终状态，后续续聊不覆盖它。
	InitialExecution *models.Execution
}

// ReadExecution 只折叠原索引定位的指定执行生命周期。
func (m *service) ReadExecution(ctx context.Context, chatID, executionID string) (models.Execution, error) {
	result, err := m.ReadExecutionOutcome(ctx, chatID, executionID)
	return result.Execution, err
}

// ReadInitialExecution 读取首发身份、精确首次执行及当前交互摘要。
func (m *service) ReadInitialExecution(ctx context.Context, chatID string) (InitialExecutionState, error) {
	if err := ctx.Err(); err != nil {
		return InitialExecutionState{}, chatContextStatus(err)
	}
	window, err := m.readExecutionWindow(ctx, chatID, "", "")
	if err != nil {
		return InitialExecutionState{}, err
	}
	state, err := replayChatState(window.Records)
	if err != nil {
		return InitialExecutionState{}, err
	}
	chat, err := m.LoadExecutionChat(ctx, chatID)
	if err != nil {
		return InitialExecutionState{}, err
	}
	return InitialExecutionState{Chat: chat, CreateRequestID: state.createRequestID, CreateRequestHash: state.createRequestHash, InitialExecution: cloneExecution(state.initialExecution)}, nil
}

// ReadSubmission 只按原 submission 身份定位已接纳回执。
func (m *service) ReadSubmission(ctx context.Context, chatID, submissionID string) (*models.Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, chatContextStatus(err)
	}
	if strings.TrimSpace(submissionID) == "" {
		return nil, status_error.BadRequest
	}
	window, err := m.readExecutionWindow(ctx, chatID, "", submissionID)
	if err != nil {
		return nil, err
	}
	state, err := replayChatState(window.Records)
	if err != nil {
		return nil, err
	}
	execution := cloneExecution(state.initialExecution)
	if execution != nil && execution.SubmissionID != submissionID {
		return nil, status_error.ChatLogCorrupted
	}
	return execution, nil
}

// ReadExecutionMessages 按执行身份读取本轮公开消息，不展开其他回合。
func (m *service) ReadExecutionMessages(ctx context.Context, chatID, executionID string) ([]messageview.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, chatContextStatus(err)
	}
	records, err := m.ReadExecutionRecords(chatID, executionID)
	if err != nil {
		return nil, err
	}
	var created eventlog.ExecutionCreatedData
	for _, record := range records {
		if record.Type == constants.ChatEventTypeExecutionCreated {
			if json.Unmarshal(record.Data, &created) != nil {
				return nil, status_error.ChatLogCorrupted
			}
			break
		}
	}
	if created.UserMessageID != "" {
		location, _, err := m.locateExecution(ctx, chatID, created.UserMessageID, "")
		if err != nil {
			return nil, err
		}
		if location == nil {
			return nil, status_error.ChatLogCorrupted
		}
		input, err := m.logStore.ReadMessageAt(chatID, location.Sequence)
		if err != nil {
			return nil, err
		}
		if len(input) > 1 {
			records = append(append(input[:1:1], input[1:]...), records[1:]...)
		}
	}
	messages, err := messageview.ReplayMessages(records)
	if err != nil {
		return nil, status_error.ChatLogCorrupted
	}
	return messages, nil
}

// ExecutionOutcome 是指定执行及其最后一次已持久化 Runtime 调用的窄事实视图。
// 调用方必须同时检查 Execution.Status 和 TerminationReason；normal 不能覆盖交付失败。
type ExecutionOutcome struct {
	// StartSequence 是本次执行接纳事实的持久起点。
	StartSequence int64
	// LatestSequence 是本次读取快照的持久日志游标。
	LatestSequence int64
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
func (m *service) ReadExecutionOutcome(ctx context.Context, chatID, executionID string) (ExecutionOutcome, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionOutcome{}, chatContextStatus(err)
	}
	if strings.TrimSpace(executionID) == "" {
		return ExecutionOutcome{}, status_error.BadRequest
	}
	window, err := m.readExecutionWindow(ctx, chatID, executionID, "")
	if err != nil {
		return ExecutionOutcome{}, err
	}
	records := window.Records
	state, err := replayChatState(records)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	execution := state.executions[executionID]
	if execution == nil {
		return ExecutionOutcome{}, status_error.ExecutionNotFound
	}
	result := ExecutionOutcome{StartSequence: window.StartSequence, LatestSequence: window.LatestSequence, Execution: *execution, TerminationReason: constants.RuntimeTerminationReasonUnknown}
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
