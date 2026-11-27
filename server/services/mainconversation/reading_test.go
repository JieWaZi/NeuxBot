package mainconversation

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestPublicationsAdvanceOnlyTheirOwnReadingFacts(t *testing.T) {
	service, wakers, _, _, hub := newTestService(t)
	ctx := t.Context()
	const reader = constants.LocalReaderID
	for _, id := range []string{"a", "b"} {
		if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	work, err := service.ReserveWork(ctx, "waker", "a", "a", "scope")
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Ensure(ctx, "waker", "b")
	if err != nil {
		t.Fatal(err)
	}
	resource := events.Resource{Kind: "main_conversation", ID: work.ConversationID}
	reading := hub.Subscribe("reading", resource)
	defer reading.Close()
	board := hub.Subscribe("work-board", resource)
	defer board.Close()
	entity := hub.Subscribe("entities", events.Resource{Kind: "waker", ID: "a"})
	defer entity.Close()
	input, err := service.AdmitUser(ctx, "waker", "a", "request", "mine", "", work.WorkChatID, "execution", "", "", "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	assertState := func(conversationID string, count int64, first, latest string) {
		t.Helper()
		state, err := service.store.Reading(ctx, reader, conversationID)
		if err != nil || state.UnreadMessageCount != count || state.FirstUnreadMessageID != first || state.LatestReadableMessageID != latest {
			t.Fatalf("read state = %+v, err = %v", state, err)
		}
	}
	assertState(work.ConversationID, 0, "", "")
	if len(reading.Events)+len(board.Events)+len(entity.Events) != 0 {
		t.Fatal("own public message emitted unread attention")
	}
	if _, _, err := service.PublishWithReceipt(ctx, work.WorkChatID, "execution", "invalid", "not committed", "missing"); err == nil {
		t.Fatal("invalid reply was accepted")
	}
	if len(reading.Events)+len(board.Events)+len(entity.Events) != 0 {
		t.Fatal("rolled back publication emitted unread attention")
	}

	first, replay, err := service.PublishWithReceipt(ctx, work.WorkChatID, "execution", "first", "first report", input.ID)
	if err != nil || replay {
		t.Fatalf("first = %+v replay = %t error = %v", first, replay, err)
	}
	assertState(work.ConversationID, 1, first.ID, first.ID)
	second, replay, err := service.PublishWithReceipt(ctx, work.WorkChatID, "execution", "second", "second report", input.ID)
	if err != nil || replay {
		t.Fatalf("second = %+v replay = %t error = %v", second, replay, err)
	}
	if got, replay, err := service.PublishWithReceipt(ctx, work.WorkChatID, "execution", "first", "first report", input.ID); err != nil || !replay || got.ID != first.ID {
		t.Fatalf("replayed = %+v replay = %t error = %v", got, replay, err)
	}
	assertState(work.ConversationID, 2, first.ID, second.ID)
	assertState(other.ID, 0, "", "")
	if len(reading.Events) != 2 || len(board.Events) != 4 || len(entity.Events) != 2 {
		t.Fatalf("events after two publications + replay: reading=%d board=%d entity=%d", len(reading.Events), len(board.Events), len(entity.Events))
	}
	wantMessages := map[string]bool{first.ID: true, second.ID: true}
	for len(board.Events) > 0 {
		frame := <-board.Events
		if frame.Type != "work-board.live" {
			continue
		}
		var live models.WorkBoardLiveEvent
		if err := json.Unmarshal(frame.Payload, &live); err != nil || live.Operation != "published" ||
			string(live.Work.Kind) != resource.Kind || live.Work.ID != resource.ID || live.ChatID != "" || !wantMessages[live.MessageID] {
			t.Fatalf("incorrect live publication = %+v, err=%v", live, err)
		}
		delete(wantMessages, live.MessageID)
	}
	if len(wantMessages) != 0 {
		t.Fatalf("missing public reminders: %v", wantMessages)
	}

	ok, err := service.store.MarkReadForBoard(ctx, reader, work.ConversationID, first.ID, 0)
	if err != nil || !ok {
		t.Fatalf("mark frozen first result = %t, %v", ok, err)
	}
	assertState(work.ConversationID, 1, second.ID, second.ID)
	if err := service.store.MarkRead(ctx, reader, work.ConversationID, input.ID, 0, constants.ReadingIntentExplicit); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("own input accepted as read boundary: %v", err)
	}
	if err := service.store.MarkRead(ctx, reader, other.ID, second.ID, 0, constants.ReadingIntentExplicit); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("cross-conversation boundary accepted: %v", err)
	}
	if err := service.store.MarkUnread(ctx, reader, work.ConversationID, first.ID, 0); err != nil {
		t.Fatal(err)
	}
	for _, command := range []struct {
		// generation 是该窗口最后观察到的手动未读代次。
		generation int64
		// intent 是窗口当时的阅读动作。
		intent constants.ReadingIntent
	}{{0, constants.ReadingIntentExplicit}, {1, constants.ReadingIntentAutomatic}} {
		if err := service.store.MarkRead(ctx, reader, work.ConversationID, second.ID, command.generation, command.intent); !errors.Is(err, status_error.ReadingIntentConflict) {
			t.Fatalf("stale or held read accepted: %v", err)
		}
	}
	assertState(work.ConversationID, 2, first.ID, second.ID)

	var windows sync.WaitGroup
	for _, boundary := range []string{first.ID, second.ID} {
		windows.Add(1)
		go func() {
			defer windows.Done()
			if err := service.store.MarkRead(ctx, reader, work.ConversationID, boundary, 1, constants.ReadingIntentExplicit); err != nil {
				t.Error(err)
			}
		}()
	}
	windows.Wait()
	assertState(work.ConversationID, 0, "", second.ID)
	final, err := service.store.Reading(ctx, reader, work.ConversationID)
	if err != nil || final.ThroughSequence != second.Sequence || final.UnreadGeneration != 1 || final.HoldUnread {
		t.Fatalf("reading after two windows = %+v, %v", final, err)
	}
}
