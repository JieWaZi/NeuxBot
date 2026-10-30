package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

// presentationChat 只替换适配器使用的权威查询，避免模拟其他 Chat 行为。
type presentationChat struct {
	// Module 保留不属于本用例的 Chat 接口。
	chat.Module
	// cursors 记录适配器实际读取的历史窗口。
	cursors []int64
	// initial 在需要时覆盖首次接纳身份和状态。
	initial *chat.InitialExecutionState
	// executions 记录按准确执行身份读取的结果。
	executions []string
}

func (c *presentationChat) ReadInitialExecution(context.Context, string) (chat.InitialExecutionState, error) {
	if c.initial != nil {
		return *c.initial, nil
	}
	return chat.InitialExecutionState{CreateRequestID: "attempt", InitialExecution: &models.Execution{ID: "first", Status: constants.ExecutionStatusCompleted}}, nil
}
func (c *presentationChat) ReadMessageHistory(_ context.Context, _ string, before int64, _ int) (chat.MessageHistoryPage, error) {
	c.cursors = append(c.cursors, before)
	if before == 0 {
		return chat.MessageHistoryPage{Resources: []messageview.Message{{ID: "continued", ExecutionID: "later", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "续聊文本"}}}}, HasMore: true, NextBeforeSequence: 99}, nil
	}
	return chat.MessageHistoryPage{Resources: []messageview.Message{
		{ID: "process", ExecutionID: "first", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartProcessCommentary, Text: "过程"}}},
		{ID: "final", ExecutionID: "first", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "最终正文"}}},
	}}, nil
}
func (c *presentationChat) ReadExecutionMessages(_ context.Context, chatID, executionID string) ([]messageview.Message, error) {
	c.executions = append(c.executions, chatID+":"+executionID)
	return []messageview.Message{
		{ID: "process", ExecutionID: "first", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartProcessCommentary, Text: "过程"}}},
		{ID: "final", ExecutionID: "first", Role: "assistant", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "最终正文"}}},
	}, nil
}
func TestCoreWorkerAdapterKeepsFinalMessageAcrossHistoryAndContinuedChat(t *testing.T) {
	port := &presentationChat{}
	adapter := CoreWorkerAdapter{Chats: port}
	receipt, err := adapter.Inspect(context.Background(), models.WorkflowEffect{ID: "attempt", ChatID: "worker-chat", ExecutionID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.FinalPresent || receipt.FinalMessageID != "final" || receipt.Text != "最终正文" || (len(port.cursors) != 0 || len(port.executions) != 1 || port.executions[0] != "worker-chat:first") {
		t.Fatalf("wrong final message: %+v cursors=%v executions=%v", receipt, port.cursors, port.executions)
	}
}

func TestCoreWorkerAdapterRejectsChangedInitialIdentityWithoutReadingMessages(t *testing.T) {
	for _, initial := range []chat.InitialExecutionState{
		{CreateRequestID: "other-attempt", InitialExecution: &models.Execution{ID: "first", Status: constants.ExecutionStatusCompleted}},
		{CreateRequestID: "attempt", InitialExecution: &models.Execution{ID: "continued", Status: constants.ExecutionStatusCompleted}},
	} {
		port := &presentationChat{initial: &initial}
		_, err := (CoreWorkerAdapter{Chats: port}).Inspect(context.Background(), models.WorkflowEffect{ID: "attempt", ChatID: "worker-chat", ExecutionID: "first"})
		if !errors.Is(err, status_error.WorkflowStateConflict) || len(port.executions) != 0 || len(port.cursors) != 0 {
			t.Fatalf("changed initial identity was consumed: %v %+v", err, port)
		}
	}
}
func TestCoreWorkerAdapterWaitingAndFailedExecutionDoNotReadMessageBodies(t *testing.T) {
	for _, status := range []constants.ExecutionStatus{constants.ExecutionStatusPending, constants.ExecutionStatusRunning, constants.ExecutionStatusCanceled, constants.ExecutionStatusFailed} {
		initial := chat.InitialExecutionState{CreateRequestID: "attempt", InitialExecution: &models.Execution{ID: "first", Status: status}}
		port := &presentationChat{initial: &initial}
		receipt, err := (CoreWorkerAdapter{Chats: port}).Inspect(context.Background(), models.WorkflowEffect{ID: "attempt", ChatID: "worker-chat", ExecutionID: "first"})
		if err != nil || receipt.Status != status || len(port.executions) != 0 || len(port.cursors) != 0 {
			t.Fatalf("non-completed execution expanded messages: %+v %v", receipt, err)
		}
	}
}
