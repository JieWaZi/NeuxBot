package knowledge

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"testing"
)

func TestChatLibrariesUseActualChatProjectInsteadOfRequestedProject(t *testing.T) {
	s, general := newServiceFixture(t)
	ctx := context.Background()
	s.config.Chats = selectedChat{chat: models.Chat{WakerID: "waker", ProjectID: ""}}
	values, err := s.ChatLibraries(ctx, "actual-chat", "ignored-waker")
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Ref.KnowledgeBaseID != general.ID || !values[0].DirectIngestSupported {
		t.Fatal(values)
	}
}

func TestChatLibrariesRejectsActualChatForeignProject(t *testing.T) {
	s, _ := newServiceFixture(t)
	ctx := context.Background()
	if err := s.config.Projects.Create(ctx, models.Project{ID: "foreign-project", WakerID: "other-waker", Name: "Foreign"}); err != nil {
		t.Fatal(err)
	}
	s.config.Chats = selectedChat{chat: models.Chat{WakerID: "waker", ProjectID: "foreign-project"}}
	if _, err := s.ChatLibraries(ctx, "actual-chat", "ignored"); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("actual Chat project ownership bypassed: %v", err)
	}
}
