package chat

import (
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/schedulecalc"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestTimedTaskReceiptRecoversIntoChatHistoryAndEventStream(t *testing.T) {
	database, chat := newRecoveryTestService(t, t.TempDir())
	commands := controllers.NewAgentCommandController(database)
	chat.managedCommands = commands
	const chatID, executionID = "12121212121212121212121212121212", "execution"
	ctx := t.Context()
	workspace := t.TempDir()
	if _, err := chat.CreateDormantChats(ctx, []DormantChat{{Chat: models.Chat{
		ID: chatID, WakerID: "waker", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace},
	}}}); err != nil {
		t.Fatal(err)
	}
	records, err := chat.AppendExecutionFacts(ctx, chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "request", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "Change the reminder."})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "request"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	start := records[len(records)-1].Sequence
	receipt := capability.Receipt{
		ID: "committed-edit", Digest: "input",
		Origin: capability.ReceiptOrigin{ChatID: chatID, ExecutionID: executionID, WakerID: "waker", Operation: capability.RuleUpdate},
		Data: map[string]any{constants.AutomaticTaskReceiptDataKey: eventlog.AutomaticTaskReceipt{
			TaskID: "saved-task", Name: "Reminder", Updated: true, Enabled: true, NextRunAt: 1795914000000,
			Schedule: schedulecalc.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindOnce, Timezone: "Asia/Shanghai", OnceAt: "2026-11-29T09:00"},
		}},
	}
	if err := commands.Create(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		history, err := chat.ReadMessageTimeline(ctx, chatID, 0, 0, 20)
		if err != nil || len(history.Resources) != 2 {
			t.Fatalf("history = %+v, err = %v", history, err)
		}
		message := history.Resources[1]
		if message.ID != executionID || !message.Readable || len(message.AutomaticTaskReceipts) != 1 ||
			message.AutomaticTaskReceipts[0].ReceiptID != receipt.ID || message.AutomaticTaskReceipts[0].NextRunAt != 1795914000000 {
			t.Fatalf("recovered message = %+v", message)
		}
	}
	events, err := chat.ListEvents(ctx, chatID, start, 5)
	if err != nil || len(events) != 1 || events[0].Type != constants.ChatEventTypeExecutionDelivery ||
		events[0].Message == nil || len(events[0].Message.AutomaticTaskReceipts) != 1 {
		t.Fatalf("events = %+v, err = %v", events, err)
	}
}
