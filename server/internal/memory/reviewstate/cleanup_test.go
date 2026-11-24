package reviewstate

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
)

func TestReviewPreservesStateDuringStagedChatDeletion(t *testing.T) {
	for _, run := range []string{"observe", "review", "skill"} {
		t.Run(run, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			store := openCleanupTestStore(t, root)
			p := Progress{WakerID: "partner", ChatID: "chat"}
			if err := store.Observe(ctx, p.ChatID, p.WakerID); err != nil {
				t.Fatal(err)
			}
			v := Suggestion{ID: "receipt", WakerID: p.WakerID, ChatID: p.ChatID, Status: StatusQueued}
			if err := store.CommitOnly(ctx, func(tx *sql.Tx, _ bool) error {
				_, err := CommitProgress(ctx, tx, p, 1, &v)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			release, err := chatstorage.AcquireOperation(root, p.ChatID)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			w := New(store, Ports{
				Chat: func(context.Context, string) (ChatAccess, error) {
					return ChatAccess{}, status_error.ChatNotFound
				},
				Guard: func(waker, chat string) (func(), error) {
					return chatstorage.AcquireReferences(root, "waker:"+waker, chat)
				},
				Published: func(context.Context, Suggestion) (PublishedSkill, bool, error) {
					return PublishedSkill{}, false, nil
				},
			})
			switch run {
			case "observe":
				if err := w.observe(ctx, p.ChatID); !errors.Is(err, status_error.ChatHasActiveExecution) {
					t.Fatalf("observation must wait for staged deletion: %v", err)
				}
			case "review":
				if err := w.review(ctx, p); !errors.Is(err, status_error.ChatHasActiveExecution) {
					t.Fatalf("staged deletion must wait for commit or rollback: %v", err)
				}
			case "skill":
				w.save(ctx, v)
			}
			actual, err := store.Suggestion(ctx, p.ChatID, v.ID)
			if err != nil || actual.Status != StatusQueued {
				t.Fatalf("staged deletion changed suggestion: %+v, %v", actual, err)
			}
			release()
			if err := w.review(ctx, p); err != nil {
				t.Fatal(err)
			}
			if actual, err = store.Suggestion(ctx, p.ChatID, v.ID); err != nil || actual.ID != "" {
				t.Fatalf("committed deletion left suggestion: %+v, %v", actual, err)
			}
		})
	}
}

func TestReceiptReconciliationKeepsUnacknowledgedPublications(t *testing.T) {
	ctx := context.Background()
	store := openCleanupTestStore(t, t.TempDir())
	receipts := map[string]bool{"queued": true, "running": true, "failed": true, "saved": true, "deleted-source": true}
	for _, status := range []string{StatusQueued, StatusRunning, StatusFailed, StatusSaved} {
		p := Progress{ChatID: "chat-" + status, WakerID: "partner"}
		if err := store.Observe(ctx, p.ChatID, p.WakerID); err != nil {
			t.Fatal(err)
		}
		v := Suggestion{ID: status, WakerID: p.WakerID, ChatID: p.ChatID, Name: status, Status: status}
		if err := store.CommitOnly(ctx, func(tx *sql.Tx, _ bool) error {
			_, err := CommitProgress(ctx, tx, p, 1, &v)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	w := New(store, Ports{
		ListLearningReceiptIDs: func(context.Context, string, int) ([]string, error) {
			return []string{"deleted-source", "failed", "queued", "running", "saved"}, nil
		},
		DeleteLearningReceipt: func(_ context.Context, id string) error {
			delete(receipts, id)
			return nil
		},
	})
	w.reconcileLearningReceipts(ctx, "")
	if len(receipts) != 3 || !receipts["failed"] || !receipts["queued"] || !receipts["running"] {
		t.Fatalf("replay receipts after cleanup = %v", receipts)
	}
}

func TestUpdatePartnerRejectsDeletedOwner(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openCleanupTestStore(t, root)
	w := New(store, Ports{
		WakerExists: func(context.Context, string) (bool, error) { return false, nil },
		Guard: func(waker, chat string) (func(), error) {
			return chatstorage.AcquireReferences(root, "waker:"+waker, chat)
		},
	})
	if err := w.UpdatePartner(ctx, "deleted-partner", Partner{MemoryEnabled: true}); !errors.Is(err, status_error.WakerNotFound) {
		t.Fatalf("settings write for missing owner returned %v", err)
	}
	if ids, err := store.ConfiguredPartnerIDs(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("settings recreated after deletion: %v, %v", ids, err)
	}
}

func openCleanupTestStore(t *testing.T, root string) *Store {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(root, "review.db"))
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
