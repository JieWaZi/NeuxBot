package messageview

import (
	"encoding/json"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestTimedTaskReceiptIsReadableAndDeduplicatedByCommand(t *testing.T) {
	builder := NewPublicMessageViewBuilder()
	const execution = "execution"
	if _, err := builder.Apply(executionCreatedRecord(1, execution)); err != nil {
		t.Fatal(err)
	}
	for i, receipt := range []string{"create", "create", "update"} {
		payload, err := json.Marshal(map[string]any{
			"sourceId": receipt,
			"automaticTaskReceipt": map[string]any{
				"receiptId": receipt, "taskId": "task", "name": "Reminder",
				"schedule": map[string]any{"kind": "daily", "timezone": "UTC"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := builder.Apply(eventlog.Record{
			Sequence: int64(i + 2), Type: constants.ChatEventTypeExecutionDelivery,
			MessageID: execution, ExecutionID: execution, Data: payload,
		}); err != nil {
			t.Fatal(err)
		}
	}
	message := builder.PublicMessage(execution)
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		// Receipts 是同一答复下不同命令的可见定时任务快照。
		Receipts []eventlog.AutomaticTaskReceipt `json:"automaticTaskReceipts"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Receipts) != 2 || payload.Receipts[0].ReceiptID != "create" ||
		payload.Receipts[1].ReceiptID != "update" || ReadingKind(*message) != "answer" {
		t.Fatalf("receipts = %+v, readable = %q", payload.Receipts, ReadingKind(*message))
	}
}
