package workflow

import (
	"context"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
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
}

func (c *presentationChat) ReadInitialExecution(context.Context, string) (chat.InitialExecutionState, error) {
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
func TestCoreWorkerAdapterKeepsFinalMessageAcrossHistoryAndContinuedChat(t *testing.T) {
	port := &presentationChat{}
	adapter := CoreWorkerAdapter{Chats: port}
	receipt, err := adapter.Inspect(context.Background(), models.WorkflowEffect{ID: "attempt", ChatID: "worker-chat", ExecutionID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.FinalPresent || receipt.FinalMessageID != "final" || receipt.Text != "最终正文" || !reflect.DeepEqual(port.cursors, []int64{0, 99}) {
		t.Fatalf("wrong final message: %+v cursors=%v", receipt, port.cursors)
	}
}
