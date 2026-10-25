package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestExecutionWorkspaceProjectsInstructionsAndDeletion(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("项目约束"), 0o600); err != nil {
		t.Fatalf("write project instruction: %v", err)
	}
	root := filepath.Join(t.TempDir(), "chat-1")
	if err := EnsureExecutionWorkspace(root, "chat-1", "waker-1", []ProjectLink{{EntryName: "ally", SourcePath: project}}); err != nil {
		t.Fatalf("EnsureExecutionWorkspace() error = %v", err)
	}
	generatedFiles := filepath.Join(
		root,
		constants.ExecutionWorkspaceAttachmentsDirectory,
		constants.ExecutionWorkspaceGeneratedFilesDirectory,
	)
	if info, err := os.Stat(generatedFiles); err != nil || !info.IsDir() {
		t.Fatalf("generated files directory = %v, error = %v", info, err)
	}
	linked, err := os.Readlink(filepath.Join(root, "projects", "ally"))
	if err != nil || linked != project {
		t.Fatalf("project link = %q, error = %v", linked, err)
	}
	if err := DeleteExecutionWorkspace(root, "chat-1"); err != nil {
		t.Fatalf("DeleteExecutionWorkspace() error = %v", err)
	}
	if _, err := os.Stat(project); err != nil {
		t.Fatalf("real project was removed: %v", err)
	}
}

func TestThreadExecutionWorkspaceSharedByMultipleChats(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(t.TempDir(), "thread-1")
	if err := EnsureThreadExecutionWorkspace(
		root,
		"thread-1",
		"lead-waker",
		[]ProjectLink{{EntryName: "share", SourcePath: project}},
	); err != nil {
		t.Fatalf("EnsureThreadExecutionWorkspace() error = %v", err)
	}
	if err := EnsureThreadExecutionWorkspace(
		root,
		"thread-1",
		"lead-waker",
		[]ProjectLink{{EntryName: "share", SourcePath: project}},
	); err != nil {
		t.Fatalf("EnsureThreadExecutionWorkspace() idempotent error = %v", err)
	}
	memberRoot := filepath.Join(root, "members", "member-waker")
	if err := EnsureThreadMemberExecutionWorkspace(
		memberRoot,
		"thread-1",
		"member-chat",
		"member-waker",
		filepath.Join(root, constants.ExecutionWorkspaceProjectsDirectory),
	); err != nil {
		t.Fatalf("EnsureThreadMemberExecutionWorkspace() error = %v", err)
	}
	generatedFiles := filepath.Join(
		memberRoot,
		constants.ExecutionWorkspaceAttachmentsDirectory,
		constants.ExecutionWorkspaceGeneratedFilesDirectory,
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
	if err := DeleteThreadExecutionWorkspace(root, "thread-1"); err != nil {
		t.Fatalf("DeleteThreadExecutionWorkspace() error = %v", err)
	}
}
