package chat

import (
	"context"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventstream "github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
)

// TestSubscriptionRestoresRevisionBaselineWithoutPrefixBacklog 验证空积压重连仍拒绝已落后于权威历史的实时快照。
func TestSubscriptionRestoresRevisionBaselineWithoutPrefixBacklog(t *testing.T) {
	manager, chatID, latest := eventCursorService(t, 4)
	manager.subscriptions.publishLive(chatID, eventstream.PublicEvent{
		Transient: true, Message: &messageview.Message{ID: "active", Revision: latest - 1, Content: "过期内容"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	subscription, err := manager.SubscribeEvents(ctx, chatID, latest)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	if len(subscription.Backlog) != 0 {
		t.Fatalf("backlog = %d, want no events before cursor", len(subscription.Backlog))
	}
	data, err := eventlog.MarshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	records, err := manager.logStore.Append(chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "active", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	manager.subscriptions.publishCommitted(chatID, records)
	select {
	case event := <-subscription.Events:
		if event.Transient || event.Type != constants.ChatEventTypeExecutionCompleted || event.Message == nil || len(event.Message.DisplayParts) != 4 {
			t.Fatalf("first subscribed event = %+v, want terminal with restored message", event)
		}
	case <-ctx.Done():
		t.Fatal("subscription did not deliver committed terminal")
	}
}

func TestSubscriptionHubReplaysLatestLiveSnapshotToNewSubscriber(t *testing.T) {
	hub := newSubscriptionHub()
	hub.publishLive("event-1", eventstream.PublicEvent{
		Transient: true,
		Message: &messageview.Message{
			ID:       "assistant-1",
			Revision: 2,
		},
	})

	_, listener := hub.add("event-1", "")
	select {
	case event := <-listener:
		if event.Live == nil || event.Live.Message == nil {
			t.Fatalf("replayed event = %+v, want live message", event)
		}
		if event.Live.Message.ID != "assistant-1" || event.Live.Message.Revision != 2 {
			t.Fatalf("replayed message = %+v", event.Live.Message)
		}
	default:
		t.Fatal("new subscriber did not receive latest live snapshot")
	}
}

func TestSubscriptionHubClearsLatestLiveSnapshotAtExecutionTerminal(t *testing.T) {
	hub := newSubscriptionHub()
	hub.publishLive("event-1", eventstream.PublicEvent{
		Transient: true,
		Message: &messageview.Message{
			ID:       "assistant-1",
			Revision: 2,
		},
	})
	hub.publishCommitted("event-1", []eventlog.Record{
		{Type: constants.ChatEventTypeExecutionCompleted},
	})

	_, listener := hub.add("event-1", "")
	select {
	case event := <-listener:
		t.Fatalf("replayed event after terminal = %+v", event)
	default:
	}
}

func TestExecutionSubscriptionNeverReceivesLiveOrMessageBodies(t *testing.T) {
	manager, chatID, _ := eventCursorService(t, 64)
	indexed, _ := forkFixture(t)
	manager.queryIndexes = indexed.queryIndexes
	manager.queryIndexes.logStore = manager.logStore
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	subscription, err := manager.SubscribeExecutionEvents(ctx, chatID, "active")
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	if len(subscription.Backlog) != 1 || subscription.Backlog[0].ExecutionID != "active" || subscription.Backlog[0].ExecutionStatus != constants.ExecutionStatusPending || subscription.Backlog[0].Message != nil {
		t.Fatalf("execution snapshot expanded messages: %+v", subscription.Backlog)
	}
	for index := 0; index < subscriberBufferSize*2; index++ {
		manager.subscriptions.publishLive(chatID, eventstream.PublicEvent{Transient: true, Message: &messageview.Message{ID: "active", Revision: int64(index + 100), Content: "不要为执行观察复制正文"}})
		manager.subscriptions.publishCommitted(chatID, []eventlog.Record{{ExecutionID: "other", Type: constants.ChatEventTypeExecutionStarted}, {ExecutionID: "active", Type: constants.ChatEventTypeAssistantMessage}})
	}
	data, err := eventlog.MarshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	records, err := manager.logStore.Append(chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "active", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	manager.subscriptions.publishCommitted(chatID, records)
	select {
	case event, open := <-subscription.Events:
		if !open || event.Message != nil || event.ExecutionID != "active" || event.ExecutionStatus != constants.ExecutionStatusCompleted {
			t.Fatalf("lifecycle listener flooded or projected UI: %+v open=%v", event, open)
		}
	case <-ctx.Done():
		t.Fatal("current execution terminal lost")
	}
}

func TestExecutionSubscriptionCompensatesAlreadyCommittedTerminalWithoutPublicReplay(t *testing.T) {
	manager, chatID, _ := eventCursorService(t, 64)
	indexed, _ := forkFixture(t)
	manager.queryIndexes = indexed.queryIndexes
	manager.queryIndexes.logStore = manager.logStore
	data, err := eventlog.MarshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.logStore.Append(chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "active", Data: data}}); err != nil {
		t.Fatal(err)
	}
	subscription, err := manager.SubscribeExecutionEvents(context.Background(), chatID, "active")
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	if len(subscription.Backlog) != 1 || subscription.Backlog[0].Message != nil || subscription.Backlog[0].ExecutionStatus != constants.ExecutionStatusCompleted {
		t.Fatalf("fast terminal compensation=%+v", subscription.Backlog)
	}
}
