package conversation

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
)

func TestNativeDeletionResumesWhenLogOwnershipCannotBeVerified(t *testing.T) {
	for _, damaged := range []string{"staged", "surviving", "empty_surviving", "truncated_surviving", "native_shard"} {
		t.Run(damaged, func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			store := openTestStore(t, root)
			session := agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "maybe-shared"}
			writeBatch(t, store, "bind", 0, fact(constants.ChatEventTypeCreated, "", ""),
				agent.ConversationChange{Binding: &agent.NativeBinding{Session: session}})
			if _, err := store.CommitNativeFragments(ctx, agent.NativeFragmentBatch{
				Key: agent.NativeResultKey{Session: session, InvocationID: "prompt"}, CommitKey: "fragment", MessageID: "answer",
				Changes: []agent.MessageChange{{Kind: agent.MessageAppend, Text: "possibly shared native body"}},
			}); err != nil {
				t.Fatal(err)
			}
			shardPath := filepath.Join(root, nativeBucketPath(nativeShard(session)))
			damagedPath := shardPath
			var beforeBinding int64
			if damaged != "native_shard" {
				damagedID := chatID
				if damaged != "staged" {
					damagedID = "fedcba9876543210fedcba9876543210"
					if _, err := store.Commit(ctx, agent.ConversationBatch{ChatID: damagedID, Key: "create", Changes: []agent.ConversationChange{
						fact(constants.ChatEventTypeCreated, "", ""),
					}}); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				damagedPath, err = store.LogPath(damagedID)
				if err != nil {
					t.Fatal(err)
				}
				if damaged != "staged" {
					info, err := os.Stat(damagedPath)
					if err != nil {
						t.Fatal(err)
					}
					beforeBinding = info.Size()
					if _, err := store.Commit(ctx, agent.ConversationBatch{ChatID: damagedID, Key: "share", ExpectedVersion: 1, Changes: []agent.ConversationChange{
						{Binding: &agent.NativeBinding{Session: session}},
					}}); err != nil {
						t.Fatal(err)
					}
				}
			}
			switch damaged {
			case "empty_surviving", "truncated_surviving":
				if damaged == "empty_surviving" {
					beforeBinding = 0
				}
				if err := os.Truncate(damagedPath, beforeBinding); err != nil {
					t.Fatal(err)
				}
			default:
				original, err := os.ReadFile(damagedPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(damagedPath, append(original, []byte("corrupt frame\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(shardPath)
			if err != nil {
				t.Fatal(err)
			}
			layout, err := chatstorage.NewLayout(root)
			if err != nil {
				t.Fatal(err)
			}
			coordinator, err := chatstorage.NewDeletionCoordinator(layout)
			if err != nil {
				t.Fatal(err)
			}
			const intent = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			if _, err := coordinator.Stage(intent, []chatstorage.DeletionTarget{{Kind: chatstorage.AggregateKindChat, AggregateID: chatID}}, []string{chatID}); err != nil {
				t.Fatal(err)
			}
			store.Close()
			store = openTestStore(t, root)
			coordinator.SetChatCleanup(func(logs map[string]string) error {
				_, err := store.PurgeDeletedChatNativeRecords(ctx, logs)
				return err
			})
			if err := coordinator.Resume(func(string, []string) (bool, error) { return true, nil }, nil); err != nil {
				t.Fatalf("resume committed chat deletion: %v", err)
			}
			if err := coordinator.Resume(func(string, []string) (bool, error) {
				t.Fatal("committed deletion still needs replay")
				return false, nil
			}, nil); err != nil {
				t.Fatal(err)
			}
			if after, err := os.ReadFile(shardPath); err != nil || !bytes.Equal(before, after) {
				t.Fatalf("shared native records changed without proven ownership: %v", err)
			}
		})
	}
}

func TestNativeDeletionPropagatesLogIOFailure(t *testing.T) {
	store := openTestStore(t, t.TempDir())
	_, err := store.PurgeDeletedChatNativeRecords(t.Context(), map[string]string{chatID: t.TempDir()})
	if !errors.Is(err, status_error.ChatLogError) {
		t.Fatalf("read of staged log directory = %v, want log I/O failure", err)
	}
}
