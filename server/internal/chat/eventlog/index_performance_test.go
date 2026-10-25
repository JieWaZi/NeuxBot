package eventlog

import (
	"fmt"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// TestWarmIndexReadDoesNotDecodeActiveExecution 验证已校验的热读取不再恢复整个运行中执行。
func TestWarmIndexReadDoesNotDecodeActiveExecution(t *testing.T) {
	measure := func(count int) float64 {
		store, chatID, latest := activeExecutionStore(t, count)
		if _, err := store.ReadAfter(chatID, latest, 1); err != nil {
			t.Fatal(err)
		}
		return testing.AllocsPerRun(1, func() {
			if records, err := store.ReadAfter(chatID, latest, 1); err != nil || len(records) != 0 {
				t.Fatalf("ReadAfter() = %d records, %v", len(records), err)
			}
		})
	}
	small, large := measure(32), measure(256)
	if large > small*1.5 {
		t.Fatalf("unchanged hot read allocated %.0f -> %.0f (%.2fx), want constant work", small, large, large/small)
	}
}

// BenchmarkWarmIndexReadActiveExecution 覆盖当前执行增长时没有新消息的热读取开销。
func BenchmarkWarmIndexReadActiveExecution(b *testing.B) {
	for _, count := range []int{128, 1024} {
		b.Run(fmt.Sprintf("records=%d", count), func(b *testing.B) {
			store, chatID, latest := activeExecutionStore(b, count)
			if _, err := store.ReadAfter(chatID, latest, 1); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := store.ReadAfter(chatID, latest, 1); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func activeExecutionStore(t testing.TB, count int) (*Store, string, int64) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "89898989898989898989898989898989"
	inputs := []AppendInput{{Type: constants.ChatEventTypeCreated}, {Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "active"}}
	for index := 0; index < count; index++ {
		data, err := MarshalData(RuntimeMessageData{MessageRevision: int64(index + 3)})
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, AppendInput{Type: constants.ChatEventTypeAssistantMessage, ExecutionID: "active", InvocationID: "invocation", Data: data})
	}
	records, err := store.Create(chatID, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return store, chatID, records[len(records)-1].Sequence
}
