package execution

import (
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWorktreeChangeRejectsSharedActiveChat(t *testing.T) {
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := &service{journal: &agentTestJournal{chat: models.Chat{
		ID: "other-chat", WorkspaceDirectory: t.TempDir(), AdditionalWorkingDirectories: []string{repository},
	}}}
	release := reserveAgentTestExecution(t, manager, "other-chat", "execution")

	err = manager.WithIdleWorktreeChange(t.Context(), repository, func() error {
		t.Fatal("branch was switched while another Chat occupied the repository")
		return nil
	})
	if err != status_error.ChatHasActiveExecution {
		t.Fatal(err)
	}

	release()
	changed := false
	err = manager.WithIdleWorktreeChange(t.Context(), repository, func() error {
		changed = true
		return nil
	})
	if err != nil || !changed {
		t.Fatalf("idle worktree was blocked: %v", err)
	}
}
