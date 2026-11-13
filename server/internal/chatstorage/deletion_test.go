package chatstorage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeletionCoordinatorResumesByCommittedDatabaseFact(t *testing.T) {
	layout, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	coordinator, err := NewDeletionCoordinator(layout)
	if err != nil {
		t.Fatalf("NewDeletionCoordinator() error = %v", err)
	}
	chatID := "11111111111111111111111111111111"
	chatRoot, err := layout.ChatRoot(chatID)
	if err != nil {
		t.Fatalf("ChatRoot() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(chatRoot, "log"), 0o700); err != nil {
		t.Fatalf("create chat aggregate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chatRoot, "log", chatID+".jsonl"), []byte("authority"), 0o600); err != nil {
		t.Fatalf("write authority: %v", err)
	}
	if _, err := coordinator.Stage(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		[]DeletionTarget{{Kind: AggregateKindChat, AggregateID: chatID}},
		[]string{chatID},
	); err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if exists, existsErr := pathExists(chatRoot); existsErr != nil || exists {
		t.Fatal("aggregate remained published after staging")
	}
	if err := coordinator.Resume(func(string, []string) (bool, error) { return false, nil }, nil); err != nil {
		t.Fatalf("Resume(rollback) error = %v", err)
	}
	if exists, existsErr := pathExists(chatRoot); existsErr != nil || !exists {
		t.Fatal("uncommitted aggregate was not restored")
	}
	if _, err := coordinator.Stage(
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		[]DeletionTarget{{Kind: AggregateKindChat, AggregateID: chatID}},
		[]string{chatID},
	); err != nil {
		t.Fatalf("second Stage() error = %v", err)
	}
	otherChatID := "22222222222222222222222222222222"
	otherChatRoot, err := layout.ChatRoot(otherChatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherChatRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Stage("cccccccccccccccccccccccccccccccc", []DeletionTarget{
		{Kind: AggregateKindChat, AggregateID: otherChatID},
	}, []string{otherChatID}); err != nil {
		t.Fatal(err)
	}
	cleanupCalls := 0
	coordinator.SetChatCleanup(func(logs map[string]string) error {
		cleanupCalls++
		data, err := os.ReadFile(logs[chatID])
		if err != nil || string(data) != "authority" {
			t.Fatalf("cleanup could not read staged authority: %s, %v", data, err)
		}
		if _, err := os.Stat(otherChatRoot); err != nil {
			t.Fatalf("uncommitted chat was still staged while checking retained references: %v", err)
		}
		return nil
	})
	if err := coordinator.Resume(func(_ string, ids []string) (bool, error) { return ids[0] == chatID, nil }, nil); err != nil {
		t.Fatalf("Resume(commit) error = %v", err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup called %d times on committed restart", cleanupCalls)
	}
	if exists, existsErr := pathExists(chatRoot); existsErr != nil || exists {
		t.Fatal("committed aggregate was restored")
	}
	intents, err := os.ReadDir(coordinator.intentRoot)
	if err != nil || len(intents) != 0 {
		t.Fatalf("remaining deletion intents = %v, error = %v", intents, err)
	}
}

func TestDeletionCoordinatorRejectsUntrustedManifestStateAndPath(t *testing.T) {
	layout, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout() error = %v", err)
	}
	coordinator, err := NewDeletionCoordinator(layout)
	if err != nil {
		t.Fatalf("NewDeletionCoordinator() error = %v", err)
	}
	intentID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	chatID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	originalPath, err := layout.ChatRoot(chatID)
	if err != nil {
		t.Fatalf("ChatRoot() error = %v", err)
	}
	valid := deletionIntent{
		Version: deletionIntentVersion,
		ID:      intentID,
		State:   "staged",
		ChatIDs: []string{chatID},
		Targets: []deletionIntentTarget{{
			Kind:         AggregateKindChat,
			AggregateID:  chatID,
			OriginalPath: originalPath,
			StagedPath: filepath.Join(
				coordinator.stagingRoot,
				intentID,
				string(AggregateKindChat)+"-"+chatID,
			),
		}},
	}
	invalidState := valid
	invalidState.State = "unknown"
	if err := coordinator.validateIntent(invalidState); err == nil {
		t.Fatal("manifest with unknown state was accepted")
	}
	invalidPath := valid
	invalidPath.Targets = append([]deletionIntentTarget(nil), valid.Targets...)
	invalidPath.Targets[0].StagedPath = filepath.Join(coordinator.stagingRoot, intentID, "other")
	if err := coordinator.validateIntent(invalidPath); err == nil {
		t.Fatal("manifest with non-canonical staging path was accepted")
	}
}

func TestWakerDeletionUsesOwnTargetAndSeparateRecoveryFact(t *testing.T) {
	layout, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewDeletionCoordinator(layout)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := layout.AgentWorkspace("partner-one")
	conversation, _ := layout.ChatRoot("chat-one")
	for _, directory := range []string{root, conversation} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := coordinator.StageWaker("delete-one", "partner-one"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Resume(func(string, []string) (bool, error) { t.Fatal("chat recovery claimed waker target"); return false, nil }, nil); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ResumeWakers(func(id string) (bool, error) {
		if id != "partner-one" {
			t.Fatal(id)
		}
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("live Waker target not restored: %v", err)
	}
	if _, err := coordinator.StageWaker("delete-two", "partner-one"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ResumeWakers(func(string) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("deleted Waker target survived: %v", err)
	}
	if _, err := os.Stat(conversation); err != nil {
		t.Fatalf("Waker target widened to Conversation: %v", err)
	}
}

func TestWakerDeletionRejectsReplacedDirectorySymlink(t *testing.T) {
	layout, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewDeletionCoordinator(layout)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := layout.AgentWorkspace("partner-one")
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.StageWaker("refuse-link", "partner-one"); err == nil {
		t.Fatal("accepted replaced Agents directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("outside file changed: %v", err)
	}
}
