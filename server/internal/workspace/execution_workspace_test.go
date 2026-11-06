package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestAgentWorkspaceKeepsProjectOriginalAndRejectsChatDeletion(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("项目约束"), 0o600); err != nil {
		t.Fatalf("write project instruction: %v", err)
	}
	root := filepath.Join(t.TempDir(), "chat-1")
	if err := EnsureAgentWorkspace(root, "waker-1"); err != nil {
		t.Fatalf("EnsureAgentWorkspace() error = %v", err)
	}
	generatedFiles := filepath.Join(
		root,
		constants.TaskOutputsDirectoryName,
	)
	if info, err := os.Stat(generatedFiles); err != nil || !info.IsDir() {
		t.Fatalf("generated files directory = %v, error = %v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "projects")); !os.IsNotExist(err) {
		t.Fatalf("created project alias: %v", err)
	}
	if err := ValidateAgentWorkspaceDeletion(root, "another-waker"); err == nil {
		t.Fatal("foreign partner deletion accepted shared partner directory")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("shared partner directory was removed: %v", err)
	}
	if _, err := os.Stat(project); err != nil {
		t.Fatalf("real project was removed: %v", err)
	}
}

func TestThreadExecutionWorkspaceSharedByMultipleChats(t *testing.T) {
	root := filepath.Join(t.TempDir(), "thread-1")
	if err := EnsureThreadExecutionWorkspace(
		root,
		"thread-1",
		"lead-waker",
	); err != nil {
		t.Fatalf("EnsureThreadExecutionWorkspace() error = %v", err)
	}
	if err := EnsureThreadExecutionWorkspace(
		root,
		"thread-1",
		"lead-waker",
	); err != nil {
		t.Fatalf("EnsureThreadExecutionWorkspace() idempotent error = %v", err)
	}
	memberRoot := filepath.Join(root, "members", "member-waker")
	if err := EnsureThreadMemberExecutionWorkspace(
		memberRoot,
		"thread-1",
		"member-chat",
		"member-waker",
	); err != nil {
		t.Fatalf("EnsureThreadMemberExecutionWorkspace() error = %v", err)
	}
	generatedFiles := filepath.Join(
		memberRoot,
		constants.TaskOutputsDirectoryName,
	)
	if info, err := os.Stat(generatedFiles); err != nil || !info.IsDir() {
		t.Fatalf("member generated files directory = %v, error = %v", info, err)
	}
	if err := os.RemoveAll(generatedFiles); err != nil {
		t.Fatalf("remove generated files directory: %v", err)
	}
	if err := EnsureExistingThreadMemberExecutionWorkspace(
		memberRoot,
		"thread-1",
		"member-chat",
		"member-waker",
	); err != nil {
		t.Fatalf("EnsureExistingThreadMemberExecutionWorkspace() error = %v", err)
	}
	if info, err := os.Stat(generatedFiles); err != nil || !info.IsDir() {
		t.Fatalf("restored generated files directory = %v, error = %v", info, err)
	}
	owner, err := ReadExecutionWorkspaceOwner(root)
	if err != nil {
		t.Fatalf("ReadExecutionWorkspaceOwner() error = %v", err)
	}
	if owner.Kind != constants.ThreadExecutionWorkspaceOwnerKind || owner.ThreadID != "thread-1" {
		t.Fatalf("owner = %+v, want thread-1 marker", owner)
	}
	if err := ValidateThreadExecutionWorkspaceDeletion(root, "thread-1"); err != nil {
		t.Fatalf("ValidateThreadExecutionWorkspaceDeletion() error = %v", err)
	}
}
