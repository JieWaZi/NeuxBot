package messageview

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

// TestReplayMessagesToolUpdatesAllocateLinearly 防止工具更新重放反复解析历史，导致分配量按平方增长。
func TestReplayMessagesToolUpdatesAllocateLinearly(t *testing.T) {
	for name, replay := range map[string]func([]eventlog.Record) ([]Message, error){
		"internal": ReplayMessages,
		"public":   ReplayPublicMessages,
	} {
		t.Run(name, func(t *testing.T) {
			for _, modify := range []bool{false, true} {
				t.Run(fmt.Sprintf("file_modify=%t", modify), func(t *testing.T) {
					measure := func(count int) float64 {
						records := toolUpdateHistory(count, modify)
						return testing.AllocsPerRun(1, func() {
							messages, err := replay(records)
							if err != nil || len(messages) != 1 || len(messages[0].DisplayParts) != 1 {
								t.Fatalf("ReplayMessages() = %v, %v", messages, err)
							}
						})
					}
					small, large := measure(128), measure(256)
					// 比较分配次数而非耗时，避免机器负载影响；线性增长允许固定开销和切片扩容余量。
					if large > small*2.5 {
						t.Fatalf("doubling tool updates allocated %.0f -> %.0f (%.2fx), want <= 2.5x", small, large, large/small)
					}
				})
			}
		})
	}
}

// BenchmarkReplayMessagesToolUpdates 覆盖单个工具大量增量更新与包含文件差异的重放规模。
func BenchmarkReplayMessagesToolUpdates(b *testing.B) {
	benchmarkReplayToolUpdates(b, ReplayMessages)
}

// BenchmarkReplayPublicMessagesToolUpdates 验证公开历史窗口重复读取时的时间与分配增长。
func BenchmarkReplayPublicMessagesToolUpdates(b *testing.B) {
	benchmarkReplayToolUpdates(b, ReplayPublicMessages)
}

// BenchmarkMessageSnapshotsDistinctTools 覆盖多个独立工具的历史读取与逐事件实时快照成本。
func BenchmarkMessageSnapshotsDistinctTools(b *testing.B) {
	for _, live := range []bool{false, true} {
		for _, count := range []int{128, 512, 2048} {
			b.Run(fmt.Sprintf("live=%t/tools=%d", live, count), func(b *testing.B) {
				records := distinctToolHistory(count)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					builder := NewMessageViewBuilder()
					for _, record := range records {
						id, err := builder.Apply(record)
						if err != nil {
							b.Fatal(err)
						}
						if live && id != "" {
							if message := builder.PublicMessage(id); message == nil {
								b.Fatal("missing live snapshot")
							}
						}
					}
					message := builder.PublicMessage("execution-1")
					if message == nil || len(message.DisplayParts) != count {
						b.Fatalf("want %d distinct tool display parts", count)
					}
				}
			})
		}
	}
}

func distinctToolHistory(count int) []eventlog.Record {
	records := toolUpdateHistory(count*2, false)
	for index := 2; index < len(records); index++ {
		var message eventlog.RuntimeMessageData
		_ = json.Unmarshal(records[index].Data, &message)
		var data map[string]any
		_ = json.Unmarshal(message.Content[0].Data, &data)
		data["toolCallId"] = fmt.Sprintf("tool-%d", (index-2)/2)
		if index%2 == 1 {
			message.Content[0].Type = "tool_result"
			data["status"] = "completed"
		}
		message.Content[0].Data, _ = eventlog.MarshalData(data)
		records[index].Data, _ = eventlog.MarshalData(message)
	}
	return records
}

func benchmarkReplayToolUpdates(b *testing.B, replay func([]eventlog.Record) ([]Message, error)) {
	for _, modify := range []bool{false, true} {
		for _, count := range []int{128, 512, 2048} {
			b.Run(fmt.Sprintf("file_modify=%t/updates=%d", modify, count), func(b *testing.B) {
				records := toolUpdateHistory(count, modify)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					messages, err := replay(records)
					if err != nil || len(messages) != 1 {
						b.Fatalf("ReplayMessages() returned %d messages, error %v", len(messages), err)
					}
				}
			})
		}
	}
}

// toolUpdateHistory 构造完整持久消息，而非模拟前端调用或将真实用户日志加入测试。
func toolUpdateHistory(count int, modify bool) []eventlog.Record {
	records := []eventlog.Record{chatCreatedRecord(1, "/workspace"), executionCreatedRecord(2, "execution-1")}
	data := map[string]any{
		"toolCallId": "tool-1", "name": "read_file", "rawInput": strings.Repeat("input", 100),
	}
	if modify {
		data["name"] = "file_modify"
		data["content"] = []any{map[string]any{
			"type": "diff", "path": "/workspace/main.go", "oldText": "old\n", "newText": "new\n",
		}}
	}
	encoded, _ := eventlog.MarshalData(data)
	for index := 0; index < count; index++ {
		sequence := int64(index + 3)
		message, _ := eventlog.MarshalData(eventlog.RuntimeMessageData{
			MessageRevision: sequence,
			Content:         []eventlog.RuntimeMessageContent{{Type: "tool_use", Data: encoded}},
		})
		records = append(records, eventlog.Record{
			ChatID: "chat-1", Sequence: sequence, RecordedAt: sequence * 100,
			Type: constants.ChatEventTypeAssistantMessage, MessageID: "execution-1", ExecutionID: "execution-1",
			Data: message,
		})
	}
	return records
}
