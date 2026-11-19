package agent

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestThreadCleanupCannotCancelSuccessorOwner(t *testing.T) {
	lifecycle := NewThreadLifecycle(nil, nil)
	previous := &threadState{id: "chat", chatID: "chat", closing: true}
	lifecycle.threads[previous.id] = previous

	// Both preparations captured the previous closing owner. The first
	// finishes its cleanup and reserves a successor before the second resumes.
	if err := lifecycle.closeCapturedThread(t.Context(), previous); err != nil {
		t.Fatal(err)
	}
	successor := &threadState{id: "chat", chatID: "chat"}
	turn := newTurn(context.Background(), lifecycle, successor, TurnRequest{}, TurnPorts{})
	turn.phase = turnPrepared
	successor.active = turn
	lifecycle.threads[successor.id] = successor
	t.Cleanup(func() {
		if err := turn.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})

	if err := lifecycle.closeCapturedThread(t.Context(), previous); err != nil {
		t.Fatal(err)
	}
	if turn.ctx.Err() != nil {
		t.Fatalf("a delayed cleanup canceled the successor's input: %v", turn.ctx.Err())
	}
	if lifecycle.threads[successor.id] != successor || successor.active != turn || successor.closing {
		t.Fatal("a delayed cleanup changed the successor's owner or admission")
	}
}

func TestRetentionCloseIdleChatPreservesBusySeat(t *testing.T) {
	lifecycle := NewThreadLifecycle(nil, nil)
	lifecycle.threads["chat"] = &threadState{id: "chat", chatID: "chat", active: &Turn{}}
	if err := lifecycle.CloseIdleChat(t.Context(), "chat"); err != status_error.ChatHasActiveExecution {
		t.Fatalf("busy seat: %v", err)
	}
	if lifecycle.threads["chat"].closing {
		t.Fatal("active chat was withdrawn")
	}
	lifecycle.threads["chat"].active = nil
	if err := lifecycle.CloseIdleChat(t.Context(), "chat"); err != nil {
		t.Fatal(err)
	}
	if lifecycle.threads["chat"] != nil {
		t.Fatal("idle seat remains")
	}
}
