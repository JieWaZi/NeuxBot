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

	_, listener := hub.add("event-1")
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

	_, listener := hub.add("event-1")
	select {
	case event := <-listener:
		t.Fatalf("replayed event after terminal = %+v", event)
	default:
	}
}
