package chat

import (
	"context"
	"encoding/json"
	"strings"
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
		Transient: true, Live: &eventstream.LiveMessageIncrement{Generation: "old", Sequence: 1, MessageID: "active", Revision: latest - 1},
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
		Live:      &eventstream.LiveMessageIncrement{Generation: "test-generation", Sequence: 1, MessageID: "assistant-1", BaseRevision: 1, Revision: 2, Updates: []messageview.MessageIncrement{{BaseRevision: 1, Revision: 2}}},
	})

	_, listener, _ := hub.add(t.Context(), "event-1", "")
	select {
	case event := <-listener:
		if event.Live == nil || event.Live.Live == nil {
			t.Fatalf("replayed event = %+v, want live message", event)
		}
		if event.Live.Live.MessageID != "assistant-1" || event.Live.Live.Revision != 2 {
			t.Fatalf("replayed message = %+v", event.Live.Live)
		}
	default:
		t.Fatal("new subscriber did not receive latest live snapshot")
	}
}

func TestSubscriptionHubClearsLatestLiveSnapshotAtExecutionTerminal(t *testing.T) {
	hub := newSubscriptionHub()
	hub.publishLive("event-1", eventstream.PublicEvent{
		Transient: true,
		Live:      &eventstream.LiveMessageIncrement{Generation: "test-generation", Sequence: 1, MessageID: "assistant-1", BaseRevision: 1, Revision: 2, Updates: []messageview.MessageIncrement{{BaseRevision: 1, Revision: 2}}},
	})
	hub.publishCommitted("event-1", []eventlog.Record{
		{Type: constants.ChatEventTypeExecutionCompleted},
	})

	_, listener, _ := hub.add(t.Context(), "event-1", "")
	select {
	case event := <-listener:
		t.Fatalf("replayed event after terminal = %+v", event)
	default:
	}
}

func TestExecutionSubscriptionNeverReceivesLiveOrMessageBodies(t *testing.T) {
	source, chatID, _ := eventCursorService(t, 64)
	_, manager := newRecoveryTestService(t, source.logStore.Root())
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
		manager.subscriptions.publishLive(chatID, eventstream.PublicEvent{Transient: true, Live: &eventstream.LiveMessageIncrement{Generation: "generation", Sequence: int64(index + 1), MessageID: "active", Revision: int64(index + 100)}})
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

func TestSubscriptionHubReplaysUncoveredIncrementSuffixAtomically(t *testing.T) {
	hub := newSubscriptionHub()
	publish := func(sequence, base, revision int64, text string) {
		hub.publishLive("chat", eventstream.PublicEvent{Transient: true, Live: &eventstream.LiveMessageIncrement{Generation: "generation", Sequence: sequence, MessageID: "message", ExecutionID: "message", BaseRevision: base, Revision: revision, Updates: []messageview.MessageIncrement{{BaseRevision: base, Revision: revision, Parts: []messageview.MessagePartIncrement{{Kind: constants.MessageIncrementTextAppend, PartID: "body", PartType: constants.MessageDisplayPartText, Text: text}}}}}})
	}
	publish(1, 3, 4, "a")
	publish(2, 4, 5, "b")
	_, listener, _ := hub.add(t.Context(), "chat", "")
	replay := <-listener
	if replay.Live == nil || replay.Live.Message != nil || replay.Live.Live.Sequence != 2 || len(replay.Live.Live.Updates) != 2 || replay.Live.Live.BaseRevision != 3 {
		t.Fatalf("invalid replay suffix: %+v", replay.Live)
	}
	data, _ := eventlog.MarshalData(eventlog.RuntimeMessageData{MessageRevision: 4})
	hub.publishCommitted("chat", []eventlog.Record{{Type: constants.ChatEventTypeAssistantMessage, MessageID: "message", Data: data}})
	_, listener, _ = hub.add(t.Context(), "chat", "")
	replay = <-listener
	if replay.Live.Live.Sequence != 2 || replay.Live.Live.BaseRevision != 4 || len(replay.Live.Live.Updates) != 1 || replay.Live.Live.Updates[0].Parts[0].Text != "b" {
		t.Fatalf("durable handover kept covered prefix: %+v", replay.Live.Live)
	}
}

func TestSubscriptionHubBoundsMetadataOnlyRecoveryWithoutLosingLatestUsage(t *testing.T) {
	hub := newSubscriptionHub()
	for i := int64(1); i <= 1000; i++ {
		hub.publishLive("chat", eventstream.PublicEvent{Transient: true, Live: &eventstream.LiveMessageIncrement{Generation: "generation", Sequence: i, MessageID: "message", BaseRevision: i + 2, Revision: i + 3, Updates: []messageview.MessageIncrement{{BaseRevision: i + 2, Revision: i + 3, RunMetrics: &messageview.MessageRunMetrics{OutputTokens: i}}}}})
	}
	if len(hub.latestLive["chat"]) != 1 {
		t.Fatalf("metadata recovery retained %d frames", len(hub.latestLive["chat"]))
	}
	_, listener, _ := hub.add(t.Context(), "chat", "")
	replay := <-listener
	if replay.Live.Live.Sequence != 1000 || replay.Live.Live.BaseRevision != 3 || replay.Live.Live.Updates[0].RunMetrics.OutputTokens != 1000 {
		t.Fatalf("metadata recovery lost latest usage: %+v", replay.Live.Live)
	}
}

func TestSubscriptionHubOverflowRequiresCoveredBaselineBeforeReplay(t *testing.T) {
	hub := newSubscriptionHub()
	for i := int64(1); i <= subscriberBufferSize; i++ {
		hub.publishLive("chat", eventstream.PublicEvent{Transient: true, Live: &eventstream.LiveMessageIncrement{Generation: "generation", Sequence: i, MessageID: "message", BaseRevision: i, Revision: i + 1, Updates: []messageview.MessageIncrement{{BaseRevision: i, Revision: i + 1, Parts: []messageview.MessagePartIncrement{{Kind: constants.MessageIncrementTextAppend, PartID: "body", PartType: constants.MessageDisplayPartText, Text: "a"}}}}}})
	}
	_, listener, streamContext := hub.add(t.Context(), "chat", "")
	if _, open := <-listener; open {
		t.Fatal("overflow replay exposed an unprovable suffix")
	}
	if streamContext.Err() == nil || len(hub.subscribers) != 0 {
		t.Fatal("rejected replay retained its subscriber")
	}
	data, _ := eventlog.MarshalData(eventlog.RuntimeMessageData{MessageRevision: subscriberBufferSize + 1})
	hub.publishCommitted("chat", []eventlog.Record{{Type: constants.ChatEventTypeAssistantMessage, MessageID: "message", Data: data}})
	if hub.lostLiveRevision["chat"] != 0 {
		t.Fatal("complete checkpoint did not release recovery barrier")
	}
}

func TestSubscriptionHubEvictionStopsBlockedForwarding(t *testing.T) {
	hub := newSubscriptionHub()
	_, _, streamContext := hub.add(t.Context(), "chat", "")
	forwarded := make(chan bool, 1)
	go func() {
		forwarded <- sendPublicEvent(streamContext, make(chan PublicEvent), PublicEvent{})
	}()
	for version := 1; version <= subscriberBufferSize+1; version++ {
		hub.publishCheckpoint("chat", int64(version))
	}
	select {
	case delivered := <-forwarded:
		if delivered || len(hub.subscribers) != 0 {
			t.Fatal("evicted subscriber remained registered or delivered to a blocked consumer")
		}
	case <-time.After(time.Second):
		t.Fatal("evicted subscriber could not stop blocked forwarding")
	}
}

func TestSubscriptionHubRecoveryFramesStayBoundedAndContinueLiveSequence(t *testing.T) {
	hub := newSubscriptionHub()
	for i := int64(1); i <= 100; i++ {
		hub.publishLive("chat", eventstream.PublicEvent{Transient: true, Live: &eventstream.LiveMessageIncrement{Generation: "generation", Sequence: i, MessageID: "message", BaseRevision: i, Revision: i + 1, Updates: []messageview.MessageIncrement{{BaseRevision: i, Revision: i + 1, Parts: []messageview.MessagePartIncrement{{Kind: constants.MessageIncrementTextAppend, PartID: "body", PartType: constants.MessageDisplayPartText, Text: strings.Repeat("x", 1024)}}}}}})
	}
	_, listener, _ := hub.add(t.Context(), "chat", "")
	previous, bodyBytes := int64(0), 0
	for {
		event := <-listener
		frame := event.Live.Live
		encoded, _ := json.Marshal(event.Live)
		if len(encoded) > 128*1024 {
			t.Fatalf("replay frame bypassed shared WS size bound: %d", len(encoded))
		}
		if previous > 0 && frame.Sequence != previous+1 {
			t.Fatal("recovery frames are not consecutive")
		}
		previous = frame.Sequence
		for _, update := range frame.Updates {
			for _, operation := range update.Parts {
				bodyBytes += len(operation.Text)
			}
		}
		if frame.Sequence == 100 {
			break
		}
	}
	if bodyBytes != 100*1024 {
		t.Fatalf("bounded recovery lost body: %d", bodyBytes)
	}
	hub.publishLive("chat", eventstream.PublicEvent{Transient: true, Live: &eventstream.LiveMessageIncrement{Generation: "generation", Sequence: 101, MessageID: "message", BaseRevision: 101, Revision: 102, Updates: []messageview.MessageIncrement{{BaseRevision: 101, Revision: 102}}}})
	if next := (<-listener).Live.Live; next.Sequence != previous+1 {
		t.Fatalf("live handover gap: %+v", next)
	}
}
