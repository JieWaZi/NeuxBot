package events

import (
	"context"
	"errors"
	"testing"
)

func TestCommitScopeDiscardsRollbackAndWaitsForOuterCommit(t *testing.T) {
	var delivered []string
	failed := errors.New("rollback")
	err := CommitScope(context.Background(), func(ctx context.Context) error {
		AfterCommit(ctx, func() { delivered = append(delivered, "outer") })
		if err := CommitScope(ctx, func(nested context.Context) error {
			AfterCommit(nested, func() { delivered = append(delivered, "rolled back") })
			return failed
		}); !errors.Is(err, failed) {
			t.Fatal(err)
		}
		if err := CommitScope(ctx, func(nested context.Context) error {
			AfterCommit(nested, func() { delivered = append(delivered, "nested") })
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(delivered) != 0 {
			t.Fatal("published before outer commit")
		}
		return nil
	})
	if err != nil || len(delivered) != 2 || delivered[0] != "outer" || delivered[1] != "nested" {
		t.Fatalf("delivered=%v err=%v", delivered, err)
	}
	if err := CommitScope(context.Background(), func(ctx context.Context) error {
		AfterCommit(ctx, func() { delivered = append(delivered, "failed outer") })
		return failed
	}); !errors.Is(err, failed) || len(delivered) != 2 {
		t.Fatalf("%v %v", delivered, err)
	}
}

func TestHubScopesResourcesAndClosesSlowConsumers(t *testing.T) {
	hub := New()
	defer hub.Close()
	resource := Resource{Kind: "chat", ID: "one"}
	specific := hub.Subscribe("chat", resource)
	defer specific.Close()
	hub.Publish(Event{Topic: "chat", Resource: Resource{Kind: "chat", ID: "other"}})
	if len(specific.Events) != 0 {
		t.Fatal("wrong resource delivered")
	}
	for i := 0; i <= BufferSize; i++ {
		hub.Publish(Event{Topic: "chat", Resource: resource})
	}
	for i := 0; i < BufferSize; i++ {
		if _, open := <-specific.Events; !open {
			t.Fatal("queued frames disappeared")
		}
	}
	if _, open := <-specific.Events; open {
		t.Fatal("slow consumer did not require resync")
	}
	specific.Close()
	other := hub.Subscribe("im", Resource{Kind: "global"})
	hub.Publish(Event{Topic: "im", Resource: Resource{Kind: "connection", ID: "connection"}})
	if len(other.Events) != 1 {
		t.Fatal("global topic subscription missed resource")
	}
	other.Close()
}

func TestReadingInvalidationFailureClosesOnlyAffectedSubscriptions(t *testing.T) {
	hub := New()
	defer hub.Close()
	chat := Resource{Kind: "chat", ID: "one"}
	affected := hub.Subscribe("reading", chat)
	unrelated := hub.Subscribe("reading", Resource{Kind: "chat", ID: "two"})
	hub.Publish(Event{Topic: "reading", Resource: chat, Payload: make([]byte, MaximumFrameBytes+1)})
	select {
	case _, open := <-affected.Events:
		if open {
			t.Fatal("oversized publication should force resync")
		}
	default:
		t.Fatal("subscription left open after a lost invalidation")
	}
	select {
	case <-unrelated.Events:
		t.Fatal("unrelated subscription was closed or changed")
	default:
	}
}
