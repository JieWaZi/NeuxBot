package controllers

import (
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestMainInputRepositoryLimitsClaimToFIFOHead(t *testing.T) {
	ctx := t.Context()
	db := newTestDB(t)
	store := NewMainConversationInputController(db)
	work := models.MainConversationWork{WorkChatID: "source", ConversationID: "main", WakerID: "worker", ScopeKey: "scope"}
	if err := db.Create(&work).Error; err != nil {
		t.Fatal(err)
	}
	first := insertTestMainInput(t, store, "first", work)
	later := insertTestMainInput(t, store, "later", work)
	if claimed, err := store.Claim(ctx, later.Sequence); err != nil || claimed {
		t.Fatalf("second input took the first seat: %v, %v", claimed, err)
	}
	if claimed, err := store.Claim(ctx, first.Sequence); err != nil || !claimed {
		t.Fatalf("first claim = %v, %v", claimed, err)
	}
	if claimed, err := store.Claim(ctx, first.Sequence); err != nil || claimed {
		t.Fatalf("duplicate claim = %v, %v", claimed, err)
	}
}

func TestMainInputRepositoryDoesNotOvertakeRoutingToItsDefaultSource(t *testing.T) {
	ctx := t.Context()
	db := newTestDB(t)
	store := NewMainConversationInputController(db)
	owner := models.MainConversation{ID: "main", SubjectType: "team", SubjectID: "team"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	defaultSource := models.MainConversationWork{WorkChatID: "default", ConversationID: owner.ID, WakerID: "partner", ScopeKey: models.MainConversationDefaultScope}
	otherPartner := models.MainConversationWork{WorkChatID: "other", ConversationID: owner.ID, WakerID: "other", ScopeKey: models.MainConversationDefaultScope}
	project := models.MainConversationWork{WorkChatID: "project", ConversationID: owner.ID, WakerID: "partner", ScopeKey: "project"}
	if err := db.Create([]models.MainConversationWork{defaultSource, otherPartner, project}).Error; err != nil {
		t.Fatal(err)
	}
	routing, err := store.Insert(ctx, models.MainConversationInput{
		ConversationID: owner.ID, RouterWakerID: defaultSource.WakerID,
		PublicMessageID: "routing", ExecutionID: "routing", RequestHash: "routing", Input: workinput.Input{SubmissionID: "routing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	late := insertTestMainInput(t, store, "late", defaultSource)
	independent := insertTestMainInput(t, store, "independent", project)
	other := insertTestMainInput(t, store, "other", otherPartner)
	for _, row := range []models.MainConversationInput{independent, other} {
		if claimed, err := store.Claim(ctx, row.Sequence); err != nil || !claimed {
			t.Fatalf("unrelated source %s was made to wait: %v, %v", row.WorkChatID, claimed, err)
		}
	}
	if claimed, err := store.Claim(ctx, late.Sequence); err != nil || claimed {
		t.Fatalf("a default source overtook an older input still routing: %v, %v", claimed, err)
	}
	if settled, err := store.Settle(ctx, routing, constants.MainConversationInputAccepted, ""); err != nil || !settled {
		t.Fatalf("settle original routing: %v, %v", settled, err)
	}
	if claimed, err := store.Claim(ctx, late.Sequence); err != nil || !claimed {
		t.Fatalf("default source not released after routing settled: %v, %v", claimed, err)
	}
}

func TestMainInputRepositoryPublishesAndDeletesByOriginalScope(t *testing.T) {
	ctx := t.Context()
	hub := events.New()
	t.Cleanup(hub.Close)
	db := WithEventPublisher(newTestDB(t), hub)
	owner := models.MainConversation{ID: "main", SubjectType: "waker", SubjectID: "worker"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	work := models.MainConversationWork{WorkChatID: "source", ConversationID: owner.ID, WakerID: "worker", ScopeKey: "scope"}
	other := models.MainConversationWork{WorkChatID: "other", ConversationID: owner.ID, WakerID: "worker", ScopeKey: "other"}
	if err := db.Create([]models.MainConversationWork{work, other}).Error; err != nil {
		t.Fatal(err)
	}
	store := NewMainConversationInputController(db)
	sub := hub.Subscribe(constants.MainConversationEventTopic, events.Resource{Kind: owner.SubjectType, ID: owner.SubjectID})
	t.Cleanup(sub.Close)
	expectChange := func() {
		t.Helper()
		select {
		case changed := <-sub.Events:
			if changed.Type != constants.MainConversationEventTopic+".changed" {
				t.Fatalf("wrong event %+v", changed)
			}
		case <-time.After(time.Second):
			t.Fatal("missing committed queue invalidation")
		}
	}
	failed := insertTestMainInput(t, store, "old-failure", work)
	expectChange()
	if ok, err := store.Settle(ctx, failed, constants.MainConversationInputFailed, "old-reject"); err != nil || !ok {
		t.Fatalf("settle = %v, %v", ok, err)
	}
	expectChange()
	queued := insertTestMainInput(t, store, "pending", work)
	expectChange()
	sibling := insertTestMainInput(t, store, "other-pending", other)
	expectChange()
	active, err := store.List(ctx, owner.ID, "", 0, 1, false)
	if err != nil || len(active) != 1 || active[0].Sequence != queued.Sequence {
		t.Fatalf("old closed failure hid the active queue: %+v, %v", active, err)
	}
	history, err := store.List(ctx, owner.ID, "", 0, 1, true)
	if err != nil || len(history) != 1 || history[0].Sequence != failed.Sequence {
		t.Fatalf("failed receipt history = %+v, %v", history, err)
	}
	if err := releaseMainWorkChat(db, work.WorkChatID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(ctx, owner.ID, queued.PublicMessageID); err != nil || found {
		t.Fatalf("source deletion retained a receipt: %v, %v", found, err)
	}
	if _, found, err := store.Get(ctx, owner.ID, sibling.PublicMessageID); err != nil || !found {
		t.Fatalf("source deletion touched an unrelated receipt: %v, %v", found, err)
	}
	if err := deleteMainConversation(db, owner.SubjectType, owner.SubjectID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(ctx, owner.ID, sibling.PublicMessageID); err != nil || found {
		t.Fatalf("conversation deletion retained a receipt: %v, %v", found, err)
	}
}

func insertTestMainInput(t *testing.T, store *MainConversationInputController, key string, work models.MainConversationWork) models.MainConversationInput {
	t.Helper()
	row := models.MainConversationInput{ConversationID: work.ConversationID, WorkChatID: work.WorkChatID,
		PublicMessageID: key, ExecutionID: key, RequestHash: strings.Repeat("a", 64), Input: workinput.Input{SubmissionID: key}}
	got, err := store.Insert(t.Context(), row)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
