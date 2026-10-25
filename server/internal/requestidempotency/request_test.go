package requestidempotency

import (
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestIdentityReusesNormalizedContentAndScope(t *testing.T) {
	first, err := New(" request-1 ", map[string]any{"text": "hello", "count": 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	retry, err := New("request-1", map[string]any{"count": 1, "text": "hello"})
	if err != nil {
		t.Fatalf("New() retry error = %v", err)
	}
	if first != retry {
		t.Fatalf("identities differ: first=%+v retry=%+v", first, retry)
	}
	if first.ResourceID("chat", "waker-1") != retry.ResourceID("chat", "waker-1") {
		t.Fatal("same request scope produced different resource IDs")
	}
	if first.ResourceID("chat", "waker-1") == first.ResourceID("chat", "waker-2") {
		t.Fatal("different request scopes produced the same resource ID")
	}
}

func TestIdentityRejectsInvalidOrConflictingReplay(t *testing.T) {
	if _, err := New(strings.Repeat("x", 65), "content"); err != status_error.BadRequest {
		t.Fatalf("oversized request ID error = %v", err)
	}
	identity, err := New("request-1", "content")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := identity.ValidateReplay("request-1", identity.Hash, true); err != nil {
		t.Fatalf("ready replay error = %v", err)
	}
	if err := identity.ValidateReplay("request-1", identity.Hash, false); err != status_error.CreateRequestInProgress {
		t.Fatalf("in-progress replay error = %v", err)
	}
	if err := identity.ValidateReplay("request-1", "different", true); err != status_error.CreateRequestConflict {
		t.Fatalf("conflicting replay error = %v", err)
	}
}
