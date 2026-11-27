package mainconversation

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWorkSourceEntryAndDeletion(t *testing.T) {
	service, wakers, _, chats, _ := newTestService(t)
	ctx := context.Background()
	if err := wakers.Create(ctx, models.Waker{ID: "p", Name: "P"}); err != nil {
		t.Fatal(err)
	}
	w, err := service.ReserveWork(ctx, "waker", "p", "p", "managed")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ValidateWorkEntry(ctx, w.WorkChatID, false); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("ordinary send: %v", err)
	}
	if err = service.ValidateWorkEntry(ctx, "other", false); err != nil {
		t.Fatal(err)
	}
	if err = service.ValidateWorkEntry(ctx, "other", true); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("unknown tagged entry: %v", err)
	}
	receipt, _, err := service.PublishWithReceipt(ctx, w.WorkChatID, "execution", "call", "ready", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := chats.Create(ctx, models.Chat{ID: w.WorkChatID, WakerID: "p", HiddenFromWakerHistory: true}); err != nil {
		t.Fatal(err)
	}
	if err := chats.Delete(ctx, w.WorkChatID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Source(ctx, "waker", "p", w.WorkChatID); err == nil {
		t.Fatal("deleted work still accessible")
	}
	kept, err := service.Message(ctx, w.ConversationID, receipt.ID)
	if err != nil || kept.Content != "ready" || kept.SourceChatID != w.WorkChatID {
		t.Fatalf("published history: %#v %v", kept, err)
	}
	next, err := service.ReserveWork(ctx, "waker", "p", "p", "managed")
	if err != nil || next.WorkChatID == w.WorkChatID {
		t.Fatalf("reopened deleted source: %#v %v", next, err)
	}
}

func TestTeamSourceLifetime(t *testing.T) {
	s, wakers, teams, _, _ := newTestService(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	members := []models.ChannelMember{{ChannelID: "t", WakerID: "a"}, {ChannelID: "t", WakerID: "b"}}
	if err := teams.Create(ctx, models.Channel{ID: "t", Name: "Team", LeadWakerID: "a"}, members); err != nil {
		t.Fatal(err)
	}
	w, err := s.ReserveWork(ctx, "team", "t", "a", "A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveWork(ctx, "team", "t", "c", "A"); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("unexpected assignee: %v", err)
	}
	if err = teams.UpdateLead(ctx, "t", "b", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Source(ctx, "team", "t", w.WorkChatID); err != nil {
		t.Fatal("retained historical source", err)
	}
	if _, err = s.ReserveWork(ctx, "team", "t", "a", "A"); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("old leader opened a new assignment: %v", err)
	}
	owned, err := teams.ListMainWorkChatIDs(ctx, "t")
	if err != nil || len(owned) != 1 || owned[0] != w.WorkChatID {
		t.Fatalf("team deletion ownership: %v %v", owned, err)
	}
	posted, _, err := s.PublishWithReceipt(ctx, w.WorkChatID, "e", "sent", "result", "")
	if err != nil {
		t.Fatal(err)
	}
	rows, more, err := s.WorkMessages(ctx, w.WorkChatID, 0, 1)
	if err != nil || more || len(rows) != 1 || rows[0].ID != posted.ID {
		t.Fatalf("member public page: %#v %t %v", rows, more, err)
	}
	if err := teams.RemoveMember(ctx, "t", "a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.WorkMessages(ctx, w.WorkChatID, 0, 1); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("revoked team history: %v", err)
	}
	if _, _, err := s.Published(ctx, w.WorkChatID, "e", "sent"); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("revoked member receipt: %v", err)
	}
	if _, err := s.Source(ctx, "team", "t", w.WorkChatID); err != nil {
		t.Fatal("user could not read historical source", err)
	}
	if _, _, err = s.PublishWithReceipt(ctx, w.WorkChatID, "exec", "call", "late", ""); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("removed team member published: %v", err)
	}
	if err := teams.Delete(ctx, "t"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Source(ctx, "team", "t", w.WorkChatID); err == nil {
		t.Fatal("opened deleted team source")
	}
}
