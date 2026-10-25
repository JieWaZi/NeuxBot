package chat

import (
	"context"
	"fmt"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

// TestListEventsAfterLatestDoesNotBuildHistoricalSnapshots 验证游标前只恢复状态，分配量不因中间快照按平方增长。
func TestListEventsAfterLatestDoesNotBuildHistoricalSnapshots(t *testing.T) {
	measure := func(count int) float64 {
		manager, chatID, latest := eventCursorService(t, count)
		return testing.AllocsPerRun(1, func() {
			events, err := manager.ListEvents(context.Background(), chatID, latest, 0)
			if err != nil || len(events) != 0 {
				t.Fatalf("ListEvents() = %d events, %v", len(events), err)
			}
		})
	}
	small, large := measure(64), measure(128)
	if large > small*3 {
		t.Fatalf("cursor restoration allocated %.0f -> %.0f (%.2fx), want <= 3x", small, large, large/small)
	}
}

// BenchmarkListEventsAfterLatest 覆盖长执行重连但没有游标后新事件的恢复开销。
func BenchmarkListEventsAfterLatest(b *testing.B) {
	for _, count := range []int{128, 512} {
		b.Run(fmt.Sprintf("tools=%d", count), func(b *testing.B) {
			manager, chatID, latest := eventCursorService(b, count)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := manager.ListEvents(context.Background(), chatID, latest, 0); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func eventCursorService(t testing.TB, count int) (*service, string, int64) {
	t.Helper()
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	encode := func(value any) []byte {
		data, err := eventlog.MarshalData(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	chatID := "78787878787878787878787878787878"
	inputs := []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: encode(eventlog.ChatCreatedData{WorkspaceDirectory: "/workspace"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "active", Data: encode(eventlog.ExecutionCreatedData{})},
	}
	for index := 0; index < count; index++ {
		inputs = append(inputs, eventlog.AppendInput{
			Type: constants.ChatEventTypeAssistantMessage, ExecutionID: "active", InvocationID: "invocation",
			Data: encode(eventlog.RuntimeMessageData{
				MessageRevision: int64(index + 3),
				Content: []eventlog.RuntimeMessageContent{{Type: "tool_use", Data: encode(map[string]any{
					"toolCallId": fmt.Sprintf("tool-%d", index), "name": "read_file", "path": "/workspace/main.go",
				})}},
			}),
		})
	}
	records, err := store.Create(chatID, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return &service{logStore: store, subscriptions: newSubscriptionHub()}, chatID, records[len(records)-1].Sequence
}
