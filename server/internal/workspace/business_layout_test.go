package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionWorkspaceIsOwnedByWakerAcrossChats(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Agents", "waker-one")
	if err := EnsureAgentWorkspace(root, "waker-one"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "kept.txt"), []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAgentWorkspace(root, "waker-one"); err != nil {
		t.Fatalf("same Waker cwd rejected second Chat: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects")); !os.IsNotExist(err) {
		t.Fatalf("obsolete project link namespace exists: %v", err)
	}
	if err := EnsureAgentWorkspace(root, "other-waker"); err == nil {
		t.Fatal("accepted another Waker owner")
	}
}

func TestTaskPathsAndAttachmentCopiesStayInsideExecutionScope(t *testing.T) {
	root := t.TempDir()
	conversation := t.TempDir()
	source := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(source, []byte("exact-input"), 0600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareChatAttachments(conversation, "chat-one", "message-one", []AttachmentInput{{SourcePath: source}})
	if err != nil {
		t.Fatal(err)
	}
	original := prepared.RuntimePaths[0]
	paths, err := EnsureTaskDirectories(root, "execution-one")
	if err != nil {
		t.Fatal(err)
	}
	if err := MaterializeTaskInputs(paths.Temporary, "chat-one", "message-one", &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.RuntimePaths[0] == original {
		t.Fatal("native path still points at private conversation")
	}
	content, err := os.ReadFile(prepared.RuntimePaths[0])
	if err != nil || string(content) != "exact-input" {
		t.Fatalf("copy = %q, %v", content, err)
	}
	other, err := EnsureTaskDirectories(root, "execution-two")
	if err != nil || other.Outputs == paths.Outputs || other.Temporary == paths.Temporary {
		t.Fatalf("task isolation lost: %+v %+v %v", paths, other, err)
	}
	if err := prepared.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("conversation input rollback failed: %v", err)
	}
}
