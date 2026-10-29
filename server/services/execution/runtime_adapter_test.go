package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
)

func TestChatRuntimeSinkKeepsConcurrentInvocationIDsWithTheirEvents(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "34343434343434343434343434343434"
	if _, err := store.Create(chatID, []eventlog.AppendInput{
		chatCreatedInput(),
		{
			Type:        constants.ChatEventTypeExecutionCreated,
			ExecutionID: "execution-1",
			Data:        marshalData(eventlog.ExecutionCreatedData{}),
		},
		{
			Type:        constants.ChatEventTypeExecutionStarted,
			ExecutionID: "execution-1",
		},
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	runtimeSink, _ := newRuntimeEventSinkForTest(store, chatID)
	const eventCount = 64
	var waitGroup sync.WaitGroup
	waitGroup.Add(eventCount)
	for index := 0; index < eventCount; index++ {
		index := index
		go func() {
			defer waitGroup.Done()
			invocationID := fmt.Sprintf("invocation-%02d", index)
			sink := &chatRuntimeSink{runtime: runtimeSink}
			started := marshalData(runtimecontract.InvocationStartedData{
				InvocationID: invocationID,
			})
			if acceptErr := sink.Accept(context.Background(), runtimecontract.Event{
				Type: runtimecontract.EventTypeInvocationStarted,
				Data: started,
			}); acceptErr != nil {
				t.Errorf("Accept started (%s) error = %v", invocationID, acceptErr)
				return
			}
			payload := marshalData(struct {
				// Index 将持久化事件关联回预期 Invocation。
				Index int `json:"index"`
			}{Index: index})
			if acceptErr := sink.Accept(context.Background(), runtimecontract.Event{
				Type: runtimecontract.EventTypeToolUse,
				Data: payload,
			}); acceptErr != nil {
				t.Errorf("Accept(%s) error = %v", invocationID, acceptErr)
			}
		}()
	}
	waitGroup.Wait()

	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	observed := 0
	for _, record := range records {
		if record.Type != constants.ChatEventTypeAssistantMessage {
			continue
		}
		var runtimeMessage eventlog.RuntimeMessageData
		if err := json.Unmarshal(record.Data, &runtimeMessage); err != nil {
			t.Fatalf("decode runtime message: %v", err)
		}
		if len(runtimeMessage.Content) != 1 {
			t.Fatalf("runtime message content = %+v", runtimeMessage.Content)
		}
		var payload struct {
			// Index 是测试载荷声明的 Invocation 序号。
			Index int `json:"index"`
		}
		if err := json.Unmarshal(runtimeMessage.Content[0].Data, &payload); err != nil {
			t.Fatalf("decode runtime payload: %v", err)
		}
		wantInvocationID := fmt.Sprintf("invocation-%02d", payload.Index)
		if record.InvocationID != wantInvocationID {
			t.Fatalf(
				"payload %d invocation = %q, want %q",
				payload.Index,
				record.InvocationID,
				wantInvocationID,
			)
		}
		observed++
	}
	if observed != eventCount {
		t.Fatalf("persisted execution messages = %d, want %d", observed, eventCount)
	}
}
