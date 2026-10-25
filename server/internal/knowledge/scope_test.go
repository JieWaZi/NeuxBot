package knowledge

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"testing"
)

func TestScopeFreezeRejectsNewAssociationsAndRevocation(t *testing.T) {
	ctx := context.Background()
	a := ResourceRef{Backend: "local", ConnectionID: "local", KnowledgeBaseID: "a"}
	b := a
	b.KnowledgeBaseID = "b"
	current := []ResourceRef{a}
	scope := FreezeScope("project", current)
	current = append(current, b)
	check := func(context.Context, string) ([]ResourceRef, error) { return current, nil }
	if err := scope.Authorize(ctx, a, "read", check); err != nil {
		t.Fatal(err)
	}
	if err := scope.Authorize(ctx, b, "read", check); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("new association granted: %v", err)
	}
	if err := scope.Authorize(ctx, a, "organize", check); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("read became write: %v", err)
	}
	current = nil
	if err := scope.Authorize(ctx, a, "read", check); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("revoked association: %v", err)
	}
}
