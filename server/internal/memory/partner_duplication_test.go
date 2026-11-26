package memory

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestDuplicatePartnerCopiesLiveMemoryIntoIndependentEntries(t *testing.T) {
	ctx := t.Context()
	service := newTestService(t, EmbeddingConfig{}, nil)
	source := newContext("source")
	target := Context{PartnerID: "target"}

	edited := mustWrite(t, service, source, "outdated explicit preference").Memory
	if err := service.Update(ctx, source, edited.ID, "current explicit preference"); err != nil {
		t.Fatal(err)
	}
	forgotten := mustWrite(t, service, source, "obsolete remembered preference").Memory
	if err := service.Forget(ctx, source, forgotten.ID); err != nil {
		t.Fatal(err)
	}
	review, err := service.PrepareReviewChanges(ctx, ReviewMemoryBatch{
		PartnerID: source.PartnerID, ChatID: source.ChatID, Through: 8, EvidenceIDs: []string{"user-message"},
		Changes: []ReviewMemoryChange{
			{Action: "add", Content: "pinned review preference", EvidenceIDs: []string{"user-message"}},
			{Action: "add", Content: "ordinary review preference", EvidenceIDs: []string{"user-message"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CommitReviewChanges(ctx, review, nil); err != nil {
		t.Fatal(err)
	}
	original, err := service.ListMemories(ctx, ListFilter{PartnerID: source.PartnerID})
	if err != nil || original.Total != 3 {
		t.Fatalf("source memories = %+v, %v", original, err)
	}
	originalIDs := make(map[string]string, len(original.Items))
	for _, item := range original.Items {
		originalIDs[item.Data] = item.ID
		if item.Data != "ordinary review preference" {
			if err := service.SetCore(ctx, source.PartnerID, item.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := service.DuplicatePartner(ctx, source.PartnerID, target.PartnerID); err != nil {
		t.Fatal(err)
	}

	copies, err := service.ListMemories(ctx, ListFilter{PartnerID: target.PartnerID})
	if err != nil || copies.Total != 3 {
		t.Fatalf("copied memories = %+v, %v", copies, err)
	}
	copyIDs := make(map[string]string, len(copies.Items))
	for _, item := range copies.Items {
		originalID := originalIDs[item.Data]
		if originalID == "" || item.ID == originalID || item.PartnerID != target.PartnerID || item.SourceRunID != "" || item.Revision != 1 {
			t.Fatalf("invalid copied memory = %+v", item)
		}
		copyIDs[item.Data] = item.ID
	}
	candidates, err := service.RelevantReviewMemories(ctx, target.PartnerID, "preference")
	if err != nil || len(candidates) != 3 {
		t.Fatalf("target review candidates = %+v, %v", candidates, err)
	}
	for _, item := range candidates {
		ordinary := item.Content == "ordinary review preference"
		if item.Core == ordinary || item.ReadOnly == ordinary {
			t.Fatalf("copy changed core selection or review protection: %+v", item)
		}
	}
	snapshot, err := service.CoreSnapshot(ctx, target.PartnerID)
	var core coreMemoryDocument
	if err != nil || snapshot.Revision != 1 || json.Unmarshal(snapshot.JSON, &core) != nil || len(core.Items) != 2 {
		t.Fatalf("target core snapshot = %+v, %v", snapshot, err)
	}
	for _, item := range core.Items {
		if item.ID != copyIDs[item.Text] || item.Text == "ordinary review preference" {
			t.Fatalf("invalid target snapshot item = %+v", item)
		}
	}
	pinnedID := copyIDs["pinned review preference"]
	if err := service.SetCore(ctx, target.PartnerID, pinnedID, false); err != nil {
		t.Fatal(err)
	}
	unpinned, err := getAccessibleMemory(service.store.db, pinnedID, target.PartnerID)
	if err != nil || !unpinned.reviewEditable() {
		t.Fatalf("unpinning the copy did not restore automatic editing: %+v, %v", unpinned, err)
	}

	timeline, err := service.ListTimeline(ctx, ListFilter{PartnerID: target.PartnerID})
	if err != nil || timeline.Total != 0 {
		t.Fatalf("target retained source history: %+v, %v", timeline, err)
	}
	if hits := mustSearch(t, service, target, "preference"); len(hits.Results) != 3 {
		t.Fatalf("copied entries not found by keyword: %+v", hits)
	}

	targetID := copyIDs["current explicit preference"]
	if err := service.Update(ctx, target, targetID, "independently updated preference"); err != nil {
		t.Fatal(err)
	}
	result := mustSearch(t, service, source, "current")
	if !resultIDs(result)[edited.ID] {
		t.Fatalf("target update modified source: %+v", result)
	}
	if err := service.Forget(ctx, source, edited.ID); err != nil {
		t.Fatal(err)
	}
	result = mustSearch(t, service, target, "independently")
	if !resultIDs(result)[targetID] {
		t.Fatalf("source forget removed the target edit: %+v", result)
	}
}

func TestDuplicatePartnerRetainsSemanticCoverageWithoutEmbedding(t *testing.T) {
	ctx := t.Context()
	embedder := newFakeEmbedder(3)
	service := newTestService(t, semanticConfig, embedder)
	source := newContext("source")
	target := newContext("target")
	entry := mustWrite(t, service, source, "vector preference").Memory
	embedder.fail = true
	service.configSource = func(context.Context) (EmbeddingConfig, error) {
		return EmbeddingConfig{}, status_error.MemoryEmbeddingNotConfigured
	}

	if err := service.DuplicatePartner(ctx, source.PartnerID, target.PartnerID); err != nil {
		t.Fatal(err)
	}

	embedder.fail = false
	service.configSource = func(context.Context) (EmbeddingConfig, error) {
		return semanticConfig, nil
	}
	result := mustSearch(t, service, target, "vector preference")
	if result.Mode != constants.MemoryRetrievalModeSemantic || len(result.Results) != 1 {
		t.Fatalf("target lost semantic coverage: %+v", result)
	}
	hit := result.Results[0].Memory
	if hit.ID == entry.ID || hit.PartnerID != target.PartnerID || hit.Data != entry.Data {
		t.Fatalf("semantic result points to the wrong memory: %+v", hit)
	}
}

func TestDuplicatePartnerRollsBackReplacementAndCanRetry(t *testing.T) {
	ctx := t.Context()
	service := newTestService(t, semanticConfig, newFakeEmbedder(3))
	source := newContext("source")
	target := newContext("target")
	copied := mustWrite(t, service, source, "current source").Memory
	staged := mustWrite(t, service, target, "abandoned staging").Memory
	if err := service.SetCore(ctx, source.PartnerID, copied.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := service.SetCore(ctx, target.PartnerID, staged.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.db.Exec(`CREATE TRIGGER reject_duplicate_snapshot
		BEFORE INSERT ON partner_core_memory WHEN NEW.partner_id = 'target'
		BEGIN SELECT RAISE(ABORT, 'snapshot write rejected'); END`); err != nil {
		t.Fatal(err)
	}

	if err := service.DuplicatePartner(ctx, source.PartnerID, target.PartnerID); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("expected late staging failure, got %v", err)
	}
	if results := mustSearch(t, service, target, staged.Data); results.Mode != constants.MemoryRetrievalModeSemantic || !resultIDs(results)[staged.ID] {
		t.Fatalf("failed replacement changed target semantic coverage: %+v", results)
	}
	fts, err := service.keyword.searchFTS(ctx, "abandoned", 10, target.PartnerID)
	if err != nil || len(fts) != 1 || fts[0].Memory.ID != staged.ID {
		t.Fatalf("failed replacement changed target keyword coverage: %+v, %v", fts, err)
	}
	if _, err := service.store.db.Exec("DROP TRIGGER reject_duplicate_snapshot"); err != nil {
		t.Fatal(err)
	}

	if err := service.DuplicatePartner(ctx, source.PartnerID, target.PartnerID); err != nil {
		t.Fatal(err)
	}

	results := mustSearch(t, service, target, copied.Data)
	if results.Mode != constants.MemoryRetrievalModeSemantic || len(results.Results) != 1 {
		t.Fatalf("restaging did not produce exactly one semantic match: %+v", results)
	}
	hit := results.Results[0].Memory
	if hit.Data != copied.Data || hit.ID == copied.ID || hit.ID == staged.ID {
		t.Fatalf("restaging did not replace the orphan: %+v", hit)
	}
	if fts, err = service.keyword.searchFTS(ctx, "abandoned", 10, target.PartnerID); err != nil || len(fts) != 0 {
		t.Fatalf("orphan remained searchable by keyword: %+v, %v", fts, err)
	}
}

func TestDuplicatePartnerRejectsSameOwnerBeforeReplacingData(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	owner := newContext("owner")
	entry := mustWrite(t, service, owner, "preserve original").Memory

	if err := service.DuplicatePartner(t.Context(), owner.PartnerID, owner.PartnerID); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("self-copy error = %v", err)
	}
	if result := mustSearch(t, service, owner, "preserve"); !resultIDs(result)[entry.ID] {
		t.Fatalf("self-copy changed source: %+v", result)
	}
}
