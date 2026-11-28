package chat

import (
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
)

func TestReplyTargetUsesOnlyThePublicResponseText(t *testing.T) {
	assistant := messageview.Message{
		ID: "assistant", Role: constants.MessageRoleAssistant, CreatedSequence: 3,
		Status: constants.ExecutionStatusCompleted, Content: "原始 Runtime 文本",
		DisplayParts: []messageview.MessageDisplayPart{
			{Type: constants.MessageDisplayPartText, Text: "前序进度"},
			{Type: constants.MessageDisplayPartTool},
			{Type: constants.MessageDisplayPartReasoning, Text: "推理内容"},
			{Type: constants.MessageDisplayPartText, Text: "\n  第一段"},
			{Type: constants.MessageDisplayPartText, Text: "第二段  \n"},
			{Type: constants.MessageDisplayPartProcessCommentary, Text: "收尾过程"},
		},
	}
	if text := replyMessageBody(&assistant, false); text != "\n  第一段\n\n第二段  \n" {
		t.Fatalf("成功回合引用正文 = %q", text)
	}
	assistant.Status = constants.ExecutionStatusCanceled
	if text := replyMessageBody(&assistant, false); text != "前序进度\n\n\n  第一段\n\n第二段  \n" {
		t.Fatalf("取消回合引用正文 = %q", text)
	}
}

func TestReplyTargetRejectsNonMessageBody(t *testing.T) {
	assistant := messageview.Message{
		ID: "assistant", Role: constants.MessageRoleAssistant, CreatedSequence: 3,
		Status: constants.ExecutionStatusCompleted, Content: "未渲染的原始 Runtime 文本",
		DisplayParts: []messageview.MessageDisplayPart{
			{Type: constants.MessageDisplayPartTool},
			{Type: constants.MessageDisplayPartReasoning, Text: "推理内容"},
			{Type: constants.MessageDisplayPartProcessCommentary, Text: "进度内容"},
			{ID: "assistant:delivery", Type: constants.MessageDisplayPartText, Text: "交付摘要"},
		},
	}
	running := assistant
	running.Status = constants.ExecutionStatusRunning
	running.DisplayParts = []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "进行中的回答"}}
	question := assistant
	question.Question = &messageview.MessageQuestion{Message: "需要回答"}
	question.DisplayParts = running.DisplayParts
	for _, message := range []messageview.Message{
		assistant,
		running,
		question,
		{Role: constants.MessageRoleUser, CreatedSequence: 1, Attachments: []eventlog.AttachmentReference{{Name: "文档.txt"}}},
		{Role: constants.MessageRoleUser, CreatedSequence: 1, Actor: "automatic_task", Content: "系统任务"},
		{Role: constants.MessageRoleUser, CreatedSequence: 1, Actor: "user", SourceChatID: "other-chat", Content: "其他对话的转投"},
		{Role: constants.MessageRoleUser, Actor: "user", Content: "仍未接纳的用户文本"},
	} {
		if text := replyMessageBody(&message, false); text != "" {
			t.Errorf("不可引用消息 %+v 返回了 %q", message, text)
		}
	}
}

func TestWorkTicketAnchorCanQuoteFilesWithoutEnablingOrdinaryFileOnlyReplies(t *testing.T) {
	for _, message := range []messageview.Message{
		{Role: constants.MessageRoleUser, CreatedSequence: 1, Content: "附件：文档.txt", AttachmentOnly: true},
		{Role: constants.MessageRoleUser, CreatedSequence: 1, Content: "代码引用：service.go", CodeReferenceOnly: true},
	} {
		if text := replyMessageBody(&message, true); text != message.Content {
			t.Errorf("工单引用正文 = %q，期望 %q", text, message.Content)
		}
		if text := replyMessageBody(&message, false); text != "" {
			t.Errorf("普通引用返回了占位内容 %q", text)
		}
		message.SourceChatID = "other-chat"
		if text := replyMessageBody(&message, true); text != "" {
			t.Errorf("其他来源的输入可被引用：%q", text)
		}
	}
}

func TestQuoteReadsPureWorkTicketReportFromOriginalJournal(t *testing.T) {
	root := t.TempDir()
	journal := newTestConversationJournal(t, root)
	const chatID = "abababababababababababababababab"
	const executionID = "ticket-execution"
	_, err := journal.Create(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "ticket-input",
			Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "assigned"})},
		{Type: constants.ChatEventTypeExecutionCreated, MessageID: executionID, ExecutionID: executionID,
			Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "ticket-input"})},
		{Type: constants.ChatEventTypeExecutionDelivery, MessageID: executionID, ExecutionID: executionID,
			Data: marshalData(eventlog.ExecutionDeliveryData{SourceID: "ticket-report", Summary: "conclusion", WorkTicketReport: true})},
		{Type: constants.ChatEventTypeExecutionCompleted, MessageID: executionID, ExecutionID: executionID,
			Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
	})
	if err != nil {
		t.Fatal(err)
	}
	module := newTestService(t, Config{StorageDirectory: root, Conversations: journal.Store})
	defer module.Close()
	target, err := module.ReadReplyTarget(t.Context(), chatID, executionID)
	if err != nil || target.Text != "conclusion" || target.Reference.MessageID != executionID {
		t.Fatalf("typed report did not yield a quotable public reply: %+v, %v", target, err)
	}
}
