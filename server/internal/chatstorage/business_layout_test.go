package chatstorage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBusinessLayoutSeparatesConversationAndExecutionOwners(t *testing.T) {
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := layout.ChatRoot("chat-one")
	if err != nil {
		t.Fatal(err)
	}
	if conversation != filepath.Join(layout.Root(), "Conversations", "chat-one") {
		t.Fatalf("conversation root = %s", conversation)
	}
	team, err := layout.TeamWorkspace("thread-one")
	if err != nil {
		t.Fatal(err)
	}
	if team != filepath.Join(layout.Root(), "Workspaces", "thread-one") {
		t.Fatalf("team root = %s", team)
	}
}

func TestNewLayoutCanonicalizesMissingRootThroughParentAlias(t *testing.T) {
	base := t.TempDir()
	actual, alias := filepath.Join(base, "actual"), filepath.Join(base, "alias")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	layout, err := NewLayout(filepath.Join(alias, "new-managed"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(layout.Root()); !os.IsNotExist(err) {
		t.Fatalf("NewLayout created files: %v", err)
	}
	canonicalActual, err := filepath.EvalSymlinks(actual)
	if err != nil {
		t.Fatal(err)
	}
	if layout.Root() != filepath.Join(canonicalActual, "new-managed") {
		t.Fatalf("missing root kept parent alias: %s", layout.Root())
	}
	root, _ := layout.AgentWorkspace("partner")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewDeletionCoordinator(layout)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := coordinator.StageWaker("parent-alias", "partner")
	if err != nil {
		t.Fatal(err)
	}
	if err := staged.Rollback(); err != nil {
		t.Fatal(err)
	}
}
