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
	for _, name := range []string{"outputs", "tmp"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("workspace created an imposed directory %q: %v", name, err)
		}
	}
}

func TestAttachmentOriginalsStayWithConversationWithoutExecutionCopies(t *testing.T) {
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
	conversation, err = filepath.EvalSymlinks(conversation)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(original) != filepath.Join(conversation, "attachments", "message-one") {
		t.Fatalf("native input no longer names managed original: %s", original)
	}
	content, err := os.ReadFile(original)
	if err != nil || string(content) != "exact-input" {
		t.Fatalf("original = %q, %v", content, err)
	}
	if err := prepared.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("conversation input rollback failed: %v", err)
	}
}
