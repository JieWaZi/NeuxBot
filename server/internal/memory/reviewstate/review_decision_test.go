package reviewstate

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

func openReviewTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestReviewRetriesMalformedModelDecision(t *testing.T) {
	for _, answer := range []string{`{}`, `{"operations":[]}`, `{"skill":{}}`, `{"skill":{"action":"update"}}`} {
		t.Run(answer, func(t *testing.T) {
			ctx := t.Context()
			store := openReviewTestStore(t)
			cfg := Partner{SkillMode: ModeAsk, SkillGeneration: 1}
			if err := store.SetPartner(ctx, "partner", cfg); err != nil {
				t.Fatal(err)
			}
			if err := store.Observe(ctx, "chat", "partner"); err != nil {
				t.Fatal(err)
			}
			worker := New(store, Ports{
				Chat:    func(context.Context, string) (ChatAccess, error) { return ChatAccess{WakerID: "partner"}, nil },
				Resolve: func(context.Context, string, Settings) (Selection, error) { return Selection{RuntimeID: "claude"}, nil },
				Skills:  func(context.Context, string) ([]SkillCandidate, error) { return nil, nil },
				Guard:   func(string, string) (func(), error) { return func() {}, nil },
				Generate: func(ctx context.Context, _ Selection, _, _, _, _ string, gate func(context.Context) error) (string, error) {
					return answer, gate(ctx)
				},
			})
			selection, err := worker.resolve(ctx, "partner")
			origin := cfg.admission()
			if err != nil {
				t.Fatal(err)
			}
			p := Progress{ChatID: "chat", WakerID: "partner", Through: 10}
			err = worker.evaluate(ctx, selection, cfg, origin, p, []Turn{{Sequence: 10, UserID: "user", User: "a correction", Skill: true}})
			if !util.HasStatusCode(err, status_error.RuntimeProtocolIncompatible) {
				t.Fatalf("malformed decision should return a retryable protocol error, got %v", err)
			}
			updated, ok, err := store.Ready(ctx, time.Now())
			if err != nil || !ok || updated.Cursor != 0 {
				t.Fatalf("malformed decision consumed the input cursor: %+v, %t, %v", updated, ok, err)
			}
		})
	}
}

func TestMemoryOnlyReviewOmitsSkillInstructionsAndUnusedPayload(t *testing.T) {
	ctx := t.Context()
	store := openReviewTestStore(t)
	cfg := Partner{MemoryEnabled: true, MemoryGeneration: 1, SkillMode: ModeOff}
	if err := store.SetPartner(ctx, "partner", cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(ctx, "chat", "partner"); err != nil {
		t.Fatal(err)
	}
	worker := New(store, Ports{
		Chat:    func(context.Context, string) (ChatAccess, error) { return ChatAccess{WakerID: "partner"}, nil },
		Resolve: func(context.Context, string, Settings) (Selection, error) { return Selection{RuntimeID: "claude"}, nil },
		Memories: func(context.Context, string, string) ([]ReviewMemory, error) {
			return []ReviewMemory{{ID: "stored", Content: "纸质阅读笔记收在松果册。", Origin: "review", Revision: 1}}, nil
		},
		Guard: func(string, string) (func(), error) { return func() {}, nil },
		Generate: func(ctx context.Context, _ Selection, _, _, instruction, input string, gate func(context.Context) error) (string, error) {
			const want = `{"turns":[{"userRef":"e1","user":"纸质阅读笔记改放在云杉册。"}],"memories":[{"id":"m1","content":"纸质阅读笔记收在松果册。"}]}`
			if input != want || strings.Contains(instruction, `"skill"`) {
				t.Fatalf("memory-only review received unrelated fields or instructions: %s", input)
			}
			return `{"operations":[{"action":"update","memoryId":"m1","content":"纸质阅读笔记改放在云杉册。","evidenceIds":["e1"]},{"action":"forget","memoryId":"stored","evidenceIds":["e1"]},{"action":"forget","memoryId":"m1","evidenceIds":["user"]}]}`, gate(ctx)
		},
		Prepare: func(_ context.Context, batch MemoryBatch) (MemoryCommit, error) {
			if len(batch.Changes) != 1 || batch.Changes[0].Action != "update" || batch.Changes[0].MemoryID != "stored" ||
				len(batch.Changes[0].EvidenceIDs) != 1 || batch.Changes[0].EvidenceIDs[0] != "user" {
				t.Fatalf("correction lost its existing target: %+v", batch.Changes)
			}
			return store.CommitOnly, nil
		},
	})
	selection, err := worker.resolve(ctx, "partner")
	origin := cfg.admission()
	if err != nil {
		t.Fatal(err)
	}
	err = worker.evaluate(ctx, selection, cfg, origin, Progress{ChatID: "chat", WakerID: "partner", Through: 10},
		[]Turn{{Sequence: 10, UserID: "user", User: "纸质阅读笔记改放在云杉册。", Memory: true}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSkillCatalogFailurePreservesAuthorizedMemoryReview(t *testing.T) {
	ctx := t.Context()
	store := openReviewTestStore(t)
	cfg := Partner{MemoryEnabled: true, MemoryGeneration: 1, SkillMode: ModeAsk, SkillGeneration: 2}
	if err := store.SetPartner(ctx, "partner", cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(ctx, "chat", "partner"); err != nil {
		t.Fatal(err)
	}
	var changes []MemoryOperation
	worker := New(store, Ports{
		Chat:    func(context.Context, string) (ChatAccess, error) { return ChatAccess{WakerID: "partner"}, nil },
		Resolve: func(context.Context, string, Settings) (Selection, error) { return Selection{RuntimeID: "claude"}, nil },
		Memories: func(context.Context, string, string) ([]ReviewMemory, error) {
			return []ReviewMemory{{ID: "stored", Origin: "review", Revision: 1, Content: "Release preference token:sk-proj-c7x7p9w5z1q4"}}, nil
		},
		Skills: func(context.Context, string) ([]SkillCandidate, error) { return nil, status_error.DatabaseError },
		Guard:  func(string, string) (func(), error) { return func() {}, nil },
		Generate: func(ctx context.Context, _ Selection, _, _, _, input string, gate func(context.Context) error) (string, error) {
			if !strings.Contains(input, "用户喜欢简明的回复") {
				t.Fatal("authorized memory evidence did not reach the review")
			}
			if strings.Contains(input, "sk-proj-c7x7p9w5z1q4") || !strings.Contains(input, `"readOnly":true`) {
				t.Fatal("a partial redaction must hide credentials and protect the original memory")
			}
			return `{"operations":[{"action":"add","content":"用户喜欢简明的回复","evidenceIds":["e1"]}],"skill":null}`, gate(ctx)
		},
		Prepare: func(_ context.Context, batch MemoryBatch) (MemoryCommit, error) {
			changes = batch.Changes
			return store.CommitOnly, nil
		},
	})
	selection, err := worker.resolve(ctx, "partner")
	origin := cfg.admission()
	if err != nil {
		t.Fatal(err)
	}
	p := Progress{ChatID: "chat", WakerID: "partner", Through: 10}
	err = worker.evaluate(ctx, selection, cfg, origin, p, []Turn{{Sequence: 10, UserID: "user", User: "用户喜欢简明的回复", Skill: true, Memory: true}})
	if err != nil || len(changes) != 1 {
		t.Fatalf("optional catalog lookup stopped memory review: %+v, %v", changes, err)
	}
}

func TestBadSkillProposalDoesNotBlockMemoryChanges(t *testing.T) {
	ctx := t.Context()
	store := openReviewTestStore(t)
	cfg := Partner{MemoryEnabled: true, MemoryGeneration: 1, SkillMode: ModeAsk, SkillGeneration: 1}
	if err := store.SetPartner(ctx, "partner", cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(ctx, "chat", "partner"); err != nil {
		t.Fatal(err)
	}
	var committed bool
	worker := New(store, Ports{
		Chat:    func(context.Context, string) (ChatAccess, error) { return ChatAccess{WakerID: "partner"}, nil },
		Resolve: func(context.Context, string, Settings) (Selection, error) { return Selection{RuntimeID: "claude"}, nil },
		Memories: func(context.Context, string, string) ([]ReviewMemory, error) {
			return []ReviewMemory{{ID: "existing", Content: "Prefer Chinese", Origin: "review", Revision: 1}}, nil
		},
		Skills: func(context.Context, string) ([]SkillCandidate, error) { return nil, nil },
		Guard:  func(string, string) (func(), error) { return func() {}, nil },
		Generate: func(ctx context.Context, _ Selection, _, _, _, _ string, authorize func(context.Context) error) (string, error) {
			return `{"operations":[{"action":"update","memoryId":"m1","content":"Prefer English","evidenceIds":["e1"]}],"skill":{"action":"invalid","lesson":"This must not be saved.","sourceMessageIds":["user"]}}`, authorize(ctx)
		},
		Prepare: func(_ context.Context, batch MemoryBatch) (MemoryCommit, error) {
			if len(batch.Changes) != 1 || batch.Changes[0].MemoryID != "existing" || batch.Changes[0].Action != "update" {
				t.Fatalf("expected authorized update, got %+v", batch.Changes)
			}
			return func(ctx context.Context, apply CommitReviewProgress) error {
				err := store.CommitOnly(ctx, apply)
				committed = err == nil
				return err
			}, nil
		},
	})
	selection, err := worker.resolve(ctx, "partner")
	origin := cfg.admission()
	if err != nil {
		t.Fatal(err)
	}
	p := Progress{ChatID: "chat", WakerID: "partner", Through: 10}
	err = worker.evaluate(ctx, selection, cfg, origin, p, []Turn{{
		Sequence: 10, UserID: "user", User: "Please answer in English from now on.", Memory: true, Skill: true,
	}})
	if err != nil || !committed {
		t.Fatalf("bad skill proposal prevented memory commit: committed=%t, error=%v", committed, err)
	}
	suggestions, err := worker.ChatSuggestions(ctx, "chat", 0)
	if err != nil || len(suggestions) != 0 {
		t.Fatalf("bad proposal was saved: %+v, %v", suggestions, err)
	}
}

func TestSuggestionDuplicateOnlyBlocksSameSkillAuthorization(t *testing.T) {
	store := openReviewTestStore(t)
	ctx := t.Context()
	add := func(v Suggestion) {
		t.Helper()
		if err := store.Observe(ctx, v.ChatID, v.WakerID); err != nil {
			t.Fatal(err)
		}
		err := store.CommitOnly(ctx, func(tx *sql.Tx, _ bool) error {
			_, err := CommitProgress(ctx, tx, Progress{ChatID: v.ChatID, WakerID: v.WakerID, Through: 10}, 10, &v)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	old := Suggestion{ID: "s1", WakerID: "partner", ChatID: "chat1", Action: ActionCreate, Name: "template-use", Lesson: "Apply the release template.", Status: StatusIgnored, Origin: Admission{Skill: 1}}
	add(old)
	fresh := old
	fresh.ID, fresh.ChatID, fresh.Status, fresh.Origin.Skill = "s2", "chat2", StatusPending, 2
	add(fresh)
	if item, err := store.Suggestion(ctx, fresh.ChatID, fresh.ID); err != nil || item.ID != fresh.ID {
		t.Fatalf("an invalid previous authorization suppressed a fresh suggestion: %+v, %v", item, err)
	}
	duplicate := fresh
	duplicate.ID, duplicate.ChatID = "s3", "chat3"
	add(duplicate)
	if item, err := store.Suggestion(ctx, duplicate.ChatID, duplicate.ID); err != nil || item.ID != "" {
		t.Fatalf("an identical lesson in the same generation was stored twice: %+v, %v", item, err)
	}
	history, err := store.RecentSkillSuggestions(ctx, fresh.WakerID, fresh.Origin.Skill)
	if err != nil || len(history) != 1 || history[0].ID != fresh.ID {
		t.Fatalf("prior suggestions crossed the current skill authorization boundary: %+v, %v", history, err)
	}
	fresh.Origin.Skill = 3
	history, err = store.RecentSkillSuggestions(ctx, fresh.WakerID, fresh.Origin.Skill)
	if err != nil || len(history) != 0 {
		t.Fatalf("prior suggestions crossed the skill-mode generation boundary: %+v, %v", history, err)
	}
}

func TestReviewKeepsRevisionSuggestionWhenTargetIsUnavailable(t *testing.T) {
	ctx := t.Context()
	store := openReviewTestStore(t)
	cfg := Partner{SkillMode: ModeAutomatic, SkillGeneration: 1}
	if err := store.SetPartner(ctx, "partner", cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(ctx, "chat", "partner"); err != nil {
		t.Fatal(err)
	}
	worker := New(store, Ports{
		Chat:    func(context.Context, string) (ChatAccess, error) { return ChatAccess{WakerID: "partner"}, nil },
		Resolve: func(context.Context, string, Settings) (Selection, error) { return Selection{RuntimeID: "claude"}, nil },
		Skills: func(context.Context, string) ([]SkillCandidate, error) {
			return []SkillCandidate{{SkillID: "skill", VersionID: "version", Name: "release-steps", Revisable: true}}, nil
		},
		Target: func(context.Context, string, string, string) (SkillTarget, error) {
			return SkillTarget{}, status_error.DatabaseError
		},
		Guard: func(string, string) (func(), error) { return func() {}, nil },
		Generate: func(ctx context.Context, _ Selection, _, _, _, _ string, gate func(context.Context) error) (string, error) {
			return `{"skill":{"action":"update","signal":"technique","skillId":"s1","lesson":"Verify the release after the fallback succeeds.","sourceMessageIds":["e3"]}}`, gate(ctx)
		},
	})
	selection, err := worker.resolve(ctx, "partner")
	origin := cfg.admission()
	if err != nil {
		t.Fatal(err)
	}
	p := Progress{ChatID: "chat", WakerID: "partner", Through: 10}
	err = worker.evaluate(ctx, selection, cfg, origin, p, []Turn{{
		Sequence: 10, UserID: "user", User: "Let's retry the release with the fallback.", Skill: true,
		AssistantID: "assistant", Assistant: "The fallback succeeded.", Steps: []ToolStep{{ID: "tool-step", Name: "terminal command"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := store.Queued(ctx)
	if err != nil || queued.SkillID != "skill" || queued.VersionID != "version" {
		t.Fatalf("transient target lookup dropped the model's authorized proposal: %+v, %v", queued, err)
	}
	if len(queued.EvidenceIDs) != 1 || queued.EvidenceIDs[0] != "tool-step" ||
		len(queued.SourceMessageIDs) != 1 || queued.SourceMessageIDs[0] != "assistant" {
		t.Fatalf("phase-two evidence and public anchors were not preserved: %+v", queued)
	}
}
