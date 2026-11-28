package controllers

import (
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestAutomaticEventsCommitBaselineReplayAndRejectStaleObservation(t *testing.T) {
	ctx := t.Context()
	store := NewAutomaticEventController(newTestDB(t))
	base := models.AutomaticEventCursor{TaskID: "rule", Instance: "github/repo", CheckedAt: 100, Outcome: "baseline", Initialized: true}
	old := models.AutomaticEventObservation{Event: fixtureAutomaticEvent("old"), Disposition: "baseline"}
	if won, err := store.Commit(ctx, base, []models.AutomaticEventObservation{old}); err != nil || !won {
		t.Fatalf("baseline commit = %v, %v", won, err)
	}
	cur, err := store.Cursor(ctx, base.TaskID, base.Instance)
	if err != nil || cur.Revision != 1 || !cur.Initialized {
		t.Fatalf("baseline cursor = %+v, %v", cur, err)
	}
	fresh := models.AutomaticEventObservation{Event: fixtureAutomaticEvent("fresh"), Disposition: "pending"}
	cur.CheckedAt, cur.Outcome = 200, "matched"
	if won, err := store.Commit(ctx, cur, []models.AutomaticEventObservation{old, fresh}); err != nil || !won {
		t.Fatalf("event commit = %v, %v", won, err)
	}
	cur.CheckedAt = 300
	if won, err := store.Commit(ctx, cur, []models.AutomaticEventObservation{{Event: fixtureAutomaticEvent("stale"), Disposition: "pending"}}); err != nil || won {
		t.Fatalf("stale commit = %v, %v", won, err)
	}
	cur, err = store.Cursor(ctx, base.TaskID, base.Instance)
	if err != nil || cur.Revision != 2 || cur.CheckedAt != 200 || cur.Matched != 1 {
		t.Fatalf("winning cursor = %+v, %v", cur, err)
	}
	pending, err := store.List(ctx, base.TaskID, base.Instance, "pending", 10)
	if err != nil || len(pending) != 1 || pending[0].Event.ID != "fresh" {
		t.Fatalf("pending observations = %+v, %v", pending, err)
	}
	if err := store.Settle(ctx, pending[0].ID, "admitted", "run", ""); err != nil {
		t.Fatal(err)
	}
	if won, err := store.Commit(ctx, cur, []models.AutomaticEventObservation{fresh}); err != nil || !won {
		t.Fatalf("replay commit = %v, %v", won, err)
	}
	pending, err = store.List(ctx, base.TaskID, base.Instance, "pending", 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("settled event became pending again: %+v, %v", pending, err)
	}
}

func fixtureAutomaticEvent(id string) eventsource.Event {
	return eventsource.Event{ID: id, Topic: "github:owner/repo:pull:42", Kind: eventsource.PullOpened, URL: "https://github.com/owner/repo/pull/42"}
}
