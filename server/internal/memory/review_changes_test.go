package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestReviewMemoryMutationsRollBackWithCursor(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	batch := ReviewMemoryBatch{PartnerID: "partner", ChatID: "chat", Through: 12, EvidenceIDs: []string{"user-1"},
		Changes: []ReviewMemoryChange{{Action: "add", Content: "Prefer English release notes", Core: true, EvidenceIDs: []string{"user-1"}}}}
	prepared, err := service.PrepareReviewChanges(t.Context(), batch)
	if err != nil {
		t.Fatal(err)
	}
	cursorError := errors.New("cursor write failed")
	if err := service.CommitReviewChanges(t.Context(), prepared, func(_ *sql.Tx, changed bool) error {
		if !changed {
			t.Fatal("legitimate memory rejected")
		}
		return cursorError
	}); !errors.Is(err, cursorError) {
		t.Fatalf("commit error = %v", err)
	}
	snapshot, err := service.CoreSnapshot(t.Context(), "partner")
	if err != nil || snapshot.Revision != 0 || string(snapshot.JSON) != `{"items":[]}` {
		t.Fatalf("failed cursor write leaked core snapshot %+v, %v", snapshot, err)
	}
	result, err := service.Search(t.Context(), Context{PartnerID: "partner"}, "release notes", 10)
	if err != nil || len(result.Results) != 0 {
		t.Fatalf("failed cursor write leaked searchable memory %+v, %v", result, err)
	}
}

func TestReviewSavesMemoryWhenEmbeddingChangesDuringBatch(t *testing.T) {
	ctx := t.Context()
	service := newTestService(t, semanticConfig, newFakeEmbedder(3))
	batch := ReviewMemoryBatch{PartnerID: "partner", ChatID: "chat", Through: 12, EvidenceIDs: []string{"user"},
		Changes: []ReviewMemoryChange{{Action: "add", Content: "Prefer English release notes", EvidenceIDs: []string{"user"}}}}
	prepared, err := service.PrepareReviewChanges(ctx, batch)
	if err != nil {
		t.Fatal(err)
	}
	config := semanticConfig
	config.Model = "new-model"
	service.configSource = func(context.Context) (EmbeddingConfig, error) { return config, nil }
	if err := service.CommitReviewChanges(ctx, prepared, nil); err != nil {
		t.Fatal(err)
	}
	result, err := service.Search(ctx, Context{PartnerID: "partner"}, "release notes", 10)
	if err != nil || len(result.Results) != 1 {
		t.Fatalf("review did not save searchable memory: %+v, %v", result, err)
	}
	status, err := service.Status(ctx)
	if err != nil || status.VectorCount != 0 || status.Mode != constants.MemoryRetrievalModeKeyword {
		t.Fatalf("review wrote a vector for the previous model: %+v, %v", status, err)
	}
}

func TestReviewChangesProtectsTakenOverAndForgottenMemory(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	mc := Context{PartnerID: "partner"}
	apply := func(snapshot []ReviewMemory, changes ...ReviewMemoryChange) {
		t.Helper()
		prepared, err := service.PrepareReviewChanges(t.Context(), ReviewMemoryBatch{
			PartnerID: mc.PartnerID, ChatID: "chat", Through: 12, EvidenceIDs: []string{"user-1"}, Snapshot: snapshot, Changes: changes,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.CommitReviewChanges(t.Context(), prepared, nil); err != nil {
			t.Fatal(err)
		}
	}
	change := func(action, id, content string, core bool) ReviewMemoryChange {
		return ReviewMemoryChange{Action: action, MemoryID: id, Content: content, Core: core, EvidenceIDs: []string{"user-1"}}
	}
	apply(nil, change("add", "", "primary review fact", true), change("add", "", "secondary review fact", false))
	candidates, err := service.RelevantReviewMemories(t.Context(), mc.PartnerID, "review fact")
	if err != nil || len(candidates) != 2 {
		t.Fatalf("outbound candidates = %+v, %v", candidates, err)
	}
	var primaryID, secondaryID string
	for _, candidate := range candidates {
		if candidate.ReadOnly || candidate.Revision != 1 || candidate.Origin != "review" {
			t.Fatalf("metadata mismatch: %+v", candidate)
		}
		if strings.HasPrefix(candidate.Content, "primary") {
			primaryID = candidate.ID
		} else {
			secondaryID = candidate.ID
		}
	}
	if err := service.SetCore(t.Context(), mc.PartnerID, primaryID, true); err != nil {
		t.Fatal(err)
	}
	if err := service.Forget(t.Context(), mc, secondaryID); err != nil {
		t.Fatal(err)
	}
	apply(candidates, change("forget", primaryID, "", false), change("add", "", "secondary review fact", true), ReviewMemoryChange{
		Action: "add", Content: "ungrounded review fact", EvidenceIDs: []string{"non-user-evidence"},
	})
	remaining, err := service.ListMemories(t.Context(), ListFilter{PartnerID: mc.PartnerID})
	if err != nil || len(remaining.Items) != 1 || remaining.Items[0].ID != primaryID || !remaining.Items[0].Core || remaining.Items[0].reviewEditable() {
		t.Fatalf("review bypassed user choice: %+v, %v", remaining, err)
	}
	history, err := service.listHistoryForTest(primaryID)
	if err != nil || len(history) != 1 || history[0].ReviewChatID != "chat" || len(history[0].ReviewEvidence) != 1 || history[0].ReviewEvidence[0] != "user-1" {
		t.Fatalf("history lacks source evidence: %+v, %v", history, err)
	}
}

func TestReviewMemoryReusesCoreBudgetReleasedByForget(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	apply := func(batch ReviewMemoryBatch) {
		t.Helper()
		batch.PartnerID, batch.ChatID, batch.Through = "partner", "chat", 4
		batch.EvidenceIDs = []string{"user"}
		for i := range batch.Changes {
			batch.Changes[i].EvidenceIDs = []string{"user"}
		}
		prepared, err := service.PrepareReviewChanges(t.Context(), batch)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.CommitReviewChanges(t.Context(), prepared, nil); err != nil {
			t.Fatal(err)
		}
	}
	original := "first-large-fact " + strings.Repeat("中", 830)
	replacement := "second-large-fact " + strings.Repeat("文", 830)
	oversize := "first-large-fact remaining" + strings.Repeat("旁", 1650)
	apply(ReviewMemoryBatch{Changes: []ReviewMemoryChange{
		{Action: "add", Content: original, Core: true},
		{Action: "add", Content: oversize, Core: true},
	}})
	candidates, err := service.RelevantReviewMemories(t.Context(), "partner", "first-large-fact")
	if err != nil || len(candidates) != 1 || candidates[0].Content != original {
		t.Fatalf("oversized entry reached the 4 KiB candidate snapshot: %d candidates, %v", len(candidates), err)
	}
	apply(ReviewMemoryBatch{
		Snapshot: candidates,
		Changes: []ReviewMemoryChange{
			{Action: "add", Content: replacement, Core: true},
			{Action: "forget", MemoryID: candidates[0].ID},
			{Action: "add", Content: original, Core: true},
		},
	})
	pages, err := service.ListMemories(t.Context(), ListFilter{PartnerID: "partner"})
	if err != nil || len(pages.Items) != 2 {
		t.Fatalf("forget/add did not retain exactly two memories: %d items, %v", len(pages.Items), err)
	}
	for _, item := range pages.Items {
		if !item.Core || item.Data != replacement && item.Data != oversize {
			t.Fatalf("review re-added forgotten text or lost released core budget: id=%s core=%v", item.ID, item.Core)
		}
	}
	snapshot, err := service.CoreSnapshot(t.Context(), "partner")
	if err != nil || snapshot.Revision != 2 || !strings.Contains(string(snapshot.JSON), "second-large-fact") {
		t.Fatalf("batch core snapshot did not advance atomically: revision=%d, err=%v", snapshot.Revision, err)
	}
}

func TestReviewSkipsStaleTargetBeforeEmbedding(t *testing.T) {
	embedder := newFakeEmbedder(3)
	service := newTestService(t, semanticConfig, embedder)
	batch := ReviewMemoryBatch{PartnerID: "partner", ChatID: "chat", Through: 12, EvidenceIDs: []string{"user"},
		Changes: []ReviewMemoryChange{{Action: "add", Content: "remember this phrase", EvidenceIDs: []string{"user"}}}}
	prepared, err := service.PrepareReviewChanges(t.Context(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CommitReviewChanges(t.Context(), prepared, nil); err != nil {
		t.Fatal(err)
	}
	batch.Snapshot, err = service.RelevantReviewMemories(t.Context(), "partner", "remember this phrase")
	if err != nil || len(batch.Snapshot) != 1 {
		t.Fatalf("expected one visible target, got %d, %v", len(batch.Snapshot), err)
	}
	id := batch.Snapshot[0].ID
	if err := service.SetCore(t.Context(), batch.PartnerID, id, true); err != nil {
		t.Fatal(err)
	}
	embedder.fail = true
	batch.Changes = []ReviewMemoryChange{{Action: "update", MemoryID: id, Content: "stale proposal", EvidenceIDs: []string{"user"}}}
	prepared, err = service.PrepareReviewChanges(t.Context(), batch)
	if err != nil {
		t.Fatalf("a known stale operation attempted embedding and delayed review progress: %v", err)
	}
	if err := service.CommitReviewChanges(t.Context(), prepared, func(_ *sql.Tx, changed bool) error {
		if changed {
			t.Fatal("stale candidate overrode current user choice")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReviewUpdatesOriginalMemoryAfterReturningItToOrdinary(t *testing.T) {
	ctx := t.Context()
	service := newTestService(t, EmbeddingConfig{}, nil)
	mc := Context{PartnerID: "partner"}
	apply := func(snapshot []ReviewMemory, action, id, content string) {
		t.Helper()
		prepared, err := service.PrepareReviewChanges(ctx, ReviewMemoryBatch{
			PartnerID: mc.PartnerID, ChatID: "chat", Through: 15, EvidenceIDs: []string{"user"}, Snapshot: snapshot,
			Changes: []ReviewMemoryChange{{Action: action, MemoryID: id, Content: content, EvidenceIDs: []string{"user"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.CommitReviewChanges(ctx, prepared, nil); err != nil {
			t.Fatal(err)
		}
	}
	candidates := func() []ReviewMemory {
		t.Helper()
		items, err := service.RelevantReviewMemories(ctx, mc.PartnerID, "纸质阅读笔记换了归档位置")
		if err != nil || len(items) != 1 {
			t.Fatalf("expected one candidate, got %d: %v", len(items), err)
		}
		return items
	}
	apply(nil, "add", "", "纸质阅读笔记收在松果册。")
	id := candidates()[0].ID
	if err := service.SetCore(ctx, mc.PartnerID, id, true); err != nil {
		t.Fatal(err)
	}
	if !candidates()[0].ReadOnly {
		t.Fatal("manually pinned core memory must be protected")
	}
	if err := service.SetCore(ctx, mc.PartnerID, id, false); err != nil {
		t.Fatal(err)
	}
	options := candidates()
	if options[0].ReadOnly {
		t.Fatal("an automatic memory that was returned to ordinary is still read-only")
	}
	apply(options, "update", id, "纸质阅读笔记现在收在云杉册。")
	item := candidates()[0]
	if item.ID != id || item.Content != "纸质阅读笔记现在收在云杉册。" {
		t.Fatalf("the correction did not update the original memory: %+v", item)
	}
	history, err := service.listHistoryForTest(id)
	if err != nil || len(history) != 2 || history[0].Event != "UPDATE" && history[1].Event != "UPDATE" {
		t.Fatalf("expected an ADD and an UPDATE for the same ID: %+v, %v", history, err)
	}
	if err := service.Update(ctx, mc, id, "手工编辑过的云杉册。"); err != nil {
		t.Fatal(err)
	}
	for _, core := range []bool{true, false} {
		if err := service.SetCore(ctx, mc.PartnerID, id, core); err != nil {
			t.Fatal(err)
		}
	}
	if !candidates()[0].ReadOnly {
		t.Fatal("demotion changed the ownership of a manually edited memory")
	}
}

func TestReviewCandidatesIncludeCoreAndRecentWithoutKeywordMatch(t *testing.T) {
	embedder := newFakeEmbedder(3)
	service := newTestService(t, semanticConfig, embedder)
	batch := ReviewMemoryBatch{ChatID: "chat", Through: 12, EvidenceIDs: []string{"user"}}
	add := func(partner, content string, core bool) {
		t.Helper()
		batch.PartnerID = partner
		batch.Changes = []ReviewMemoryChange{{Action: "add", Content: content, Core: core, EvidenceIDs: []string{"user"}}}
		prepared, err := service.PrepareReviewChanges(t.Context(), batch)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.CommitReviewChanges(t.Context(), prepared, nil); err != nil {
			t.Fatal(err)
		}
	}
	add("partner", "用户在成都工作。", true)
	add("partner", "用户习惯在周五发布。", false)
	add("other", "用户住在广州。", true)
	embedder.fail = true

	result, err := service.RelevantReviewMemories(t.Context(), "partner", "现在换到上海了，发布安排也改到了星期三。")
	if err != nil || len(result) != 2 {
		t.Fatalf("expected only the partner's core and ordinary context, got %+v, %v", result, err)
	}
	for _, item := range result {
		if item.ReadOnly || item.Revision != 1 || item.Core && item.Content != "用户在成都工作。" || !item.Core && item.Content != "用户习惯在周五发布。" {
			t.Fatalf("unexpected context: %+v", item)
		}
	}
}

func TestReviewFindsOldChineseCorrectionTarget(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	ctx := t.Context()
	add := func(partner, content string) {
		t.Helper()
		prepared, err := service.PrepareReviewChanges(ctx, ReviewMemoryBatch{
			PartnerID: partner, ChatID: "chat", Through: 12, EvidenceIDs: []string{"user"},
			Changes: []ReviewMemoryChange{{Action: "add", Content: content, EvidenceIDs: []string{"user"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.CommitReviewChanges(ctx, prepared, nil); err != nil {
			t.Fatal(err)
		}
	}
	const original = "用户长期把纸质阅读笔记收在一本叫「松果册」的笔记本里。"
	add("partner", original)
	add("unrelated-partner", "纸质阅读笔记和松果册不对其他伙伴可见。")
	time.Sleep(2 * time.Millisecond)
	for i := range reviewMemoryLimit {
		add("partner", fmt.Sprintf("无关偏好主题编号 %d", i))
	}

	for _, fts := range []bool{true, false} {
		t.Run(fmt.Sprintf("fts=%t", fts), func(t *testing.T) {
			if fts && !service.store.ftsAvailable {
				t.Skip("SQLite FTS5 is unavailable")
			}
			service.store.ftsAvailable = fts
			options, err := service.RelevantReviewMemories(ctx, "partner", `我的纸质阅读笔记以后只归档在「云杉册」。
新名称为云杉册，不再使用松果册。
收到
`)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, item := range options {
				found = found || (item.Content == original && !item.ReadOnly)
				if strings.Contains(item.Content, "其他伙伴") {
					t.Fatal("review recalled another partner's memory")
				}
			}
			if !found {
				t.Fatalf("old editable memory missing from %d options", len(options))
			}
		})
	}
}
