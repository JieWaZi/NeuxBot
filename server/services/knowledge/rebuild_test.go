package knowledge

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"testing"
)

func TestOrganizeRebuildRequiresExplicitSourcesAndResetsOnlyChosenSource(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	input := OrganizeCommand{RequestID: "rebuild", LibraryID: lib.ID, Rebuild: true, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex}}
	if _, _, err := s.Organize(ctx, input); !errors.Is(err, status_error.KnowledgeInvalid) {
		t.Fatal("rebuild without explicit source accepted", err)
	}
	receipt, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "original.md", Bytes: []byte("# original")})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.config.Engine.GetSource(ctx, lib.ID, receipt.ResourceID)
	input.SourceVersionIDs = []string{receipt.ResourceID}
	accepted, _, err := s.Organize(ctx, input)
	if err != nil || accepted.JobID == "" {
		t.Fatal("rebuild was not admitted", accepted, err)
	}
	after, _ := s.config.Engine.GetSource(ctx, lib.ID, receipt.ResourceID)
	if after.RowVersion != before.RowVersion+1 {
		t.Fatal("host dropped explicit rebuild", after)
	}
	replay, _, err := s.Organize(ctx, input)
	if err != nil || replay.JobID != accepted.JobID {
		t.Fatal("host replay changed job", err)
	}
}
