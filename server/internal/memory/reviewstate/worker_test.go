package reviewstate

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/skills"
)

func TestCombinedReviewOnlyGeneratesSkillAfterExplicitSave(t *testing.T) {
	for _, mode := range []string{ModeAsk, ModeAutomatic} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "review.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			store, err := Open(db)
			if err != nil {
				t.Fatal(err)
			}
			const partner, chat = "p1", "c1"
			const publishedAt = 1800000000123
			if err = store.SetPartner(ctx, partner, Partner{MemoryEnabled: true, MemoryGeneration: 1, SkillMode: mode, SkillGeneration: 1}); err != nil {
				t.Fatal(err)
			}
			if err = store.Observe(ctx, chat, partner); err != nil {
				t.Fatal(err)
			}
			calls := 0
			var applied int
			var guarded bool
			var worker *Worker
			worker = New(store, Ports{
				Chat:        func(context.Context, string) (ChatAccess, error) { return ChatAccess{WakerID: partner}, nil },
				WakerExists: func(context.Context, string) (bool, error) { return true, nil },
				Resolve: func(_ context.Context, _ string, settings Settings) (Selection, error) {
					if settings.Source == SourceGlobal {
						return Selection{RuntimeID: settings.RuntimeID, Model: settings.Model}, nil
					}
					return Selection{RuntimeID: "claude", Model: "test-model"}, nil
				},
				Turns: func(context.Context, string, int64, int64, Admission, int) ([]Turn, error) {
					now := time.Now().Add(-IdleDelay - time.Minute)
					return []Turn{
						{Sequence: 5, UserID: "memory-only", User: "Only authorized for remembering", At: now, Memory: true},
						{Sequence: 10, UserID: "u1", User: "Call me J", Assistant: "Okay.", AssistantID: "a1", At: now, Memory: true, Skill: true},
						{Sequence: 20, UserID: "u2", User: "Use the template", Assistant: "Done.", AssistantID: "a2", At: now, Memory: true, Skill: true},
						{Sequence: 30, UserID: "u3", User: "Confirmed", Assistant: "Verified.", AssistantID: "a3", At: now, Memory: true, Skill: true},
					}, nil
				},
				Guard:    func(string, string) (func(), error) { guarded = true; return func() { guarded = false }, nil },
				Memories: func(context.Context, string, string) ([]ReviewMemory, error) { return nil, nil },
				Skills:   func(context.Context, string) ([]SkillCandidate, error) { return nil, nil },
				Generate: func(ctx context.Context, selection Selection, _, use, _, input string, authorize func(context.Context) error) (string, error) {
					if err := authorize(ctx); err != nil {
						return "", err
					}
					calls++
					if guarded {
						t.Fatal("a model call was made inside the chat guard")
					}
					if use == reviewUsage {
						if selection.Model != "test-model" {
							t.Fatalf("review model = %q", selection.Model)
						}
						if err := worker.UpdateSettings(ctx, Settings{Source: SourceGlobal, RuntimeID: "codex", Model: "new-model"}); err != nil {
							return "", err
						}
						if err := worker.UpdatePartner(ctx, partner, Partner{SkillMode: ModeOff}); err != nil {
							return "", err
						}
						if !strings.Contains(input, "Call me J") || !strings.Contains(input, "Verified.") {
							t.Fatalf("no combined evidence: %s", input)
						}
						return `{"operations":[{"action":"add","content":"The user's name is J.","evidenceIds":["e2"]}],"skill":{"name":"use-a-template","lesson":"Apply the saved template and check the result.","action":"create","signal":"correction","sourceMessageIds":["e4","e7"]}}`, nil
					}
					if selection.Model != "new-model" {
						t.Fatalf("skill generation model = %q", selection.Model)
					}
					if strings.Contains(input, "Only authorized for remembering") {
						t.Fatal("memory-only turn leaked into skill authoring")
					}
					for _, field := range []string{`"action"`, `"currentSkill"`, `"userRef"`, `"assistantRef"`, `"skill"`} {
						if strings.Contains(input, field) {
							t.Fatalf("skill drafting received unused field %s", field)
						}
					}
					return `{"markdown":"---\nname: use-a-template\ndescription: Prepare using the standard template.\n---\nCheck the result."}`, nil
				},
				Prepare: func(_ context.Context, b MemoryBatch) (MemoryCommit, error) {
					if guarded {
						t.Fatal("memory preparation held the chat guard")
					}
					applied = len(b.Changes)
					return store.CommitOnly, nil
				},
				Published: func(context.Context, Suggestion) (PublishedSkill, bool, error) { return PublishedSkill{}, false, nil },
				Publish: func(_ context.Context, _ Suggestion, _ skills.LearnedDraft, _ []string, guard func() (func(), error)) (PublishedSkill, error) {
					release, err := guard()
					if err != nil {
						return PublishedSkill{}, err
					}
					defer release()
					if !guarded {
						t.Fatal("publishing must hold the chat guard")
					}
					return PublishedSkill{SkillID: "s1", VersionID: "v1", PublishedAt: publishedAt}, nil
				},
			})
			worker.ports.TurnStarts = worker.ports.Turns
			p, ok, err := store.Ready(ctx, time.Now())
			if err != nil || !ok {
				t.Fatal(err)
			}
			if err = worker.review(ctx, &p); err != nil {
				t.Fatal(err)
			}
			suggestions, err := worker.ChatSuggestions(ctx, chat, 0)
			if err != nil || len(suggestions) != 1 || calls != 1 || applied != 1 {
				t.Fatalf("first stage returned %v %v, calls %d, writes %d", suggestions, err, calls, applied)
			}
			if mode == ModeAsk {
				if suggestions[0].Status != StatusPending {
					t.Fatalf("ask mode was auto dispatched: %s", suggestions[0].Status)
				}
				if v, _ := store.Queued(ctx); v.ID != "" {
					t.Fatal("ask mode queued before explicit save")
				}
				if _, err = worker.ResolveSuggestion(ctx, chat, suggestions[0].ID, DecisionSave, ""); err != nil {
					t.Fatal(err)
				}
			}
			queued, err := store.Queued(ctx)
			if err != nil || queued.ID == "" {
				t.Fatalf("no queued suggestion: %v", err)
			}
			worker.save(ctx, queued)
			saved, err := store.Suggestion(ctx, chat, queued.ID)
			if err != nil || saved.Status != StatusSaved || saved.SkillID != "s1" || saved.PublishedAt != publishedAt || calls != 2 {
				t.Fatalf("second stage saved %+v, calls %d, err %v", saved, calls, err)
			}
		})
	}
}

func TestReviewSettingsChangeDoesNotCancelAdmittedCall(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetPartner(ctx, "p", Partner{SkillMode: ModeAsk, SkillGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var sent bool
	worker := New(store, Ports{
		Chat:        func(context.Context, string) (ChatAccess, error) { return ChatAccess{WakerID: "p"}, nil },
		Resolve:     func(context.Context, string, Settings) (Selection, error) { return Selection{RuntimeID: "claude"}, nil },
		Guard:       func(string, string) (func(), error) { return func() {}, nil },
		WakerExists: func(context.Context, string) (bool, error) { return true, nil },
		Generate: func(ctx context.Context, _ Selection, _, _, _, _ string, authorize func(context.Context) error) (string, error) {
			close(entered)
			<-release
			if err := authorize(ctx); err != nil {
				return "", err
			}
			sent = true
			return "{}", nil
		},
	})
	selection, err := worker.resolve(ctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := worker.generate(ctx, selection, "p", "c", skillUsage, "rules", "accepted before changing settings")
		done <- err
	}()
	<-entered
	if err := worker.UpdatePartner(ctx, "p", Partner{SkillMode: ModeOff}); err != nil {
		t.Fatal(err)
	}
	if err := worker.UpdateSettings(ctx, Settings{Source: SourceGlobal, RuntimeID: "codex", Model: "new"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil || !sent {
		t.Fatalf("settings change interrupted the admitted call (%v, sent=%v)", err, sent)
	}
}

func TestReviewDefersReadingBodiesUntilTrigger(t *testing.T) {
	ctx := t.Context()
	store := openReviewTestStore(t)
	if err := store.SetPartner(ctx, "p", Partner{SkillMode: ModeAsk, SkillGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(ctx, "c", "p"); err != nil {
		t.Fatal(err)
	}
	ended := time.Now().Add(-time.Minute)
	starts := []Turn{{Sequence: 4, At: ended}, {Sequence: 9, At: ended}, {Sequence: 14, At: ended}}
	bodyReads := 0
	worker := New(store, Ports{
		Chat: func(context.Context, string) (ChatAccess, error) {
			return ChatAccess{WakerID: "p", LatestSequence: 15}, nil
		},
		Resolve: func(context.Context, string, Settings) (Selection, error) { return Selection{RuntimeID: "claude"}, nil },
		TurnStarts: func(context.Context, string, int64, int64, Admission, int) ([]Turn, error) {
			return starts, nil
		},
		Turns: func(context.Context, string, int64, int64, Admission, int) ([]Turn, error) {
			bodyReads++
			return starts, nil
		},
	})
	if err := worker.review(ctx, &Progress{ChatID: "c", WakerID: "p"}); err != nil {
		t.Fatal(err)
	}
	if bodyReads != 0 {
		t.Fatalf("read %d full checkpoints below the idle trigger", bodyReads)
	}
	p, ok, err := store.Ready(ctx, ended.Add(IdleDelay))
	if err != nil || !ok || p.Cursor != 0 || p.DueAt != ended.Add(IdleDelay).UnixMilli() {
		t.Fatalf("idle scheduling = %+v, %t, %v", p, ok, err)
	}
}

func TestReviewReadsWithinBudgetAndKeepsFrozenTail(t *testing.T) {
	ctx := t.Context()
	store := openReviewTestStore(t)
	if err := store.SetPartner(ctx, "p", Partner{SkillMode: ModeAsk, SkillGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(ctx, "c", "p"); err != nil {
		t.Fatal(err)
	}
	starts := make([]Turn, MaxTurns)
	for i := range starts {
		starts[i] = Turn{Sequence: int64(i + 1), At: time.Now(), Skill: true}
	}
	bodyReads := 0
	worker := New(store, Ports{
		Chat: func(context.Context, string) (ChatAccess, error) {
			return ChatAccess{WakerID: "p", Active: true, LatestSequence: MaxTurns}, nil
		},
		Resolve: func(context.Context, string, Settings) (Selection, error) { return Selection{RuntimeID: "claude"}, nil },
		TurnStarts: func(context.Context, string, int64, int64, Admission, int) ([]Turn, error) {
			return starts, nil
		},
		Turns: func(_ context.Context, _ string, after, through int64, _ Admission, limit int) ([]Turn, error) {
			if through != MaxTurns {
				t.Fatalf("unexpected frozen boundary: %d", through)
			}
			page := make([]Turn, 0, limit)
			for _, turn := range starts[after:min(after+int64(limit), through)] {
				bodyReads++
				turn.UserID, turn.User = "u", strings.Repeat("用", 1800)
				turn.AssistantID, turn.Assistant = "a", strings.Repeat("记", 1800)
				page = append(page, turn)
			}
			return page, nil
		},
		Guard:  func(string, string) (func(), error) { return func() {}, nil },
		Skills: func(context.Context, string) ([]SkillCandidate, error) { return nil, nil },
		Generate: func(ctx context.Context, _ Selection, _, _, _, input string, gate func(context.Context) error) (string, error) {
			if len(input) > EvidenceBytes {
				t.Fatalf("review exceeds evidence budget: %d", len(input))
			}
			return `{"skill":null}`, gate(ctx)
		},
	})
	if err := worker.review(ctx, &Progress{ChatID: "c", WakerID: "p"}); err != nil {
		t.Fatal(err)
	}
	p, ok, err := store.Ready(ctx, time.Now())
	if err != nil || !ok || p.Cursor != 1 || p.Through != MaxTurns || bodyReads > 4 {
		t.Fatalf("first slice did not preserve unprocessed turns: %+v, %t, reads=%d, %v", p, ok, bodyReads, err)
	}
	if err := worker.review(ctx, &p); err != nil {
		t.Fatal(err)
	}
	p, ok, err = store.Ready(ctx, time.Now())
	if err != nil || !ok || p.Cursor != 2 || p.Through != MaxTurns {
		t.Fatalf("frozen tail did not continue: %+v, %t, %v", p, ok, err)
	}
}

func TestReviewFrequencyKeepsBatchSizeAndFrozenTail(t *testing.T) {
	ctx := t.Context()
	store := openReviewTestStore(t)
	if _, err := store.db.ExecContext(ctx, "INSERT INTO review_config VALUES('settings', ?, 3)", `{"source":"partner","everyTurns":30}`); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPartner(ctx, "p", Partner{MemoryEnabled: true, MemoryGeneration: 1, SkillMode: ModeOff}); err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(ctx, "c", "p"); err != nil {
		t.Fatal(err)
	}
	collect := func(_ context.Context, _ string, from, through int64, _ Admission, limit int) ([]Turn, error) {
		var turns []Turn
		for seq := from + 1; seq <= through && len(turns) < limit; seq++ {
			turns = append(turns, Turn{Sequence: seq, UserID: strconv.FormatInt(seq, 10), User: "remember this preference", Memory: true})
		}
		return turns, nil
	}
	startsLimit := 0
	worker := New(store, Ports{
		Chat: func(context.Context, string) (ChatAccess, error) {
			return ChatAccess{WakerID: "p", Active: true, LatestSequence: 30}, nil
		},
		Resolve: func(context.Context, string, Settings) (Selection, error) { return Selection{RuntimeID: "claude"}, nil },
		TurnStarts: func(ctx context.Context, chat string, from, through int64, origin Admission, limit int) ([]Turn, error) {
			startsLimit = limit
			return collect(ctx, chat, from, through, origin, limit)
		},
		Turns:    collect,
		Guard:    func(string, string) (func(), error) { return func() {}, nil },
		Memories: func(context.Context, string, string) ([]ReviewMemory, error) { return nil, nil },
		Generate: func(context.Context, Selection, string, string, string, string, func(context.Context) error) (string, error) {
			return `{"operations":[]}`, nil
		},
	})
	for _, expected := range []struct{ cursor, through int64 }{{20, 30}, {30, 0}} {
		p, ok, err := store.Ready(ctx, time.Now())
		if err != nil || !ok {
			t.Fatalf("window = %+v, %v", p, err)
		}
		if err := worker.review(ctx, &p); err != nil {
			t.Fatal(err)
		}
		p, ok, err = store.Ready(ctx, time.Now())
		if err != nil || !ok || p.Cursor != expected.cursor || p.Through != expected.through || startsLimit != 30 {
			t.Fatalf("frequency did not preserve the expected batch edge: %+v (%v), count limit %d", p, err, startsLimit)
		}
	}
}

func TestReviewFrequencyUpdatePreservesAdmittedConfiguration(t *testing.T) {
	ctx := t.Context()
	store := openReviewTestStore(t)
	worker := New(store, Ports{})
	original := Settings{Source: SourceGlobal, RuntimeID: "codex", AccountID: "account", Model: "default"}
	if err := worker.UpdateSettings(ctx, original); err != nil {
		t.Fatal(err)
	}
	before, err := worker.Settings(ctx)
	if err != nil || before.EveryTurns != DefaultEveryTurns {
		t.Fatalf("initial settings = %+v, %v", before, err)
	}
	if err := store.Observe(ctx, "c", "p"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProgress(ctx, Progress{ChatID: "c"}, Progress{ChatID: "c", DueAt: time.Now().Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if err := worker.UpdateSettings(ctx, Settings{EveryTurns: 5}); err != nil {
		t.Fatal(err)
	}
	after, err := worker.Settings(ctx)
	before.EveryTurns = 5
	if err != nil || after != before {
		t.Fatalf("frequency update changed configuration: %+v vs %+v, %v", after, before, err)
	}
	if pending, found, err := store.Ready(ctx, time.Now()); err != nil || !found || pending.ChatID != "c" {
		t.Fatalf("lower frequency threshold did not reconsider waiting chat: %+v, %v", pending, err)
	}
}

func TestRepeatedReviewFailureBacksOffAndCanBeRetried(t *testing.T) {
	ctx := t.Context()
	store := openReviewTestStore(t)
	worker := New(store, Ports{})
	p := Progress{ChatID: "chat", WakerID: "p", Cursor: 5, Through: 12, Failures: 3}
	if err := store.Observe(ctx, p.ChatID, p.WakerID); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProgress(ctx, Progress{ChatID: p.ChatID}, p); err != nil {
		t.Fatal(err)
	}
	if err := worker.deferReview(ctx, p, status_error.RuntimeProtocolIncompatible); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Ready(ctx, time.Now().Add(time.Hour)); err != nil || found {
		t.Fatalf("persistent failure was scheduled for another immediate call: found=%t, %v", found, err)
	}
	if err := store.Retry(ctx, p.WakerID); err != nil {
		t.Fatal(err)
	}
	next, found, err := store.Ready(ctx, time.Now())
	if err != nil || !found || next.Cursor != p.Cursor || next.Through != p.Through || next.Failures != 0 {
		t.Fatalf("manual retry did not preserve the batch: %+v, %t, %v", next, found, err)
	}
}
