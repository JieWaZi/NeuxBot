package workspacefiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func fixture(t *testing.T) (*Manager, Scope, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	manager := New()
	scope, err := manager.Open("web:one", root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return manager, scope, root
}

func TestScopeRejectsOtherSubjectTraversalAndAccountFiles(t *testing.T) {
	manager, scope, root := fixture(t)
	if _, _, err := manager.Read("web:two", scope.ID, "", "note.txt"); err != status_error.WorkspaceScopeInvalid {
		t.Fatalf("other subject: %v", err)
	}
	for _, relative := range []string{"../note.txt", "/note.txt", "a\\b", "runtime/accounts/token", "x\x00"} {
		if _, _, err := manager.Read("web:one", scope.ID, "", relative); err != status_error.WorkspacePathInvalid {
			t.Fatalf("unsafe path %q: %v", relative, err)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Read("web:one", scope.ID, "", "escape/secret"); err != status_error.WorkspacePathInvalid {
		t.Fatalf("escape link: %v", err)
	}
	if _, _, err := manager.Read("web:one", scope.ID, outside, "secret"); err != status_error.WorkspacePathInvalid {
		t.Fatalf("foreign root: %v", err)
	}
	manager.Close("web:two", scope.ID)
	if _, _, err := manager.Read("web:one", scope.ID, "", "note.txt"); err != nil {
		t.Fatal(err)
	}
	manager.Close("web:one", scope.ID)
	if _, _, err := manager.Read("web:one", scope.ID, "", "note.txt"); err != status_error.WorkspaceScopeInvalid {
		t.Fatalf("closed scope: %v", err)
	}
}

func TestFrozenProjectLinkAndRetarget(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	other := t.TempDir()
	for _, dir := range []string{project, other} {
		if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("project"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(project, filepath.Join(root, "project")); err != nil {
		t.Fatal(err)
	}
	manager := New()
	scope, err := manager.Open("desktop", root, []string{project})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.Read("desktop", scope.ID, "", "project/note.txt"); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, "project")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(other, filepath.Join(root, "project")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.Read("desktop", scope.ID, "", "project/note.txt"); err != status_error.WorkspacePathInvalid {
		t.Fatalf("retarget: %v", err)
	}
}

func TestVersionedWriteRenameAndDelete(t *testing.T) {
	manager, scope, root := fixture(t)
	original, _, err := manager.Read("web:one", scope.ID, "", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Write("web:one", scope.ID, "", "note.txt", "after", original.Version)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version == original.Version {
		t.Fatal("version was not advanced")
	}
	if _, err = manager.Write("web:one", scope.ID, "", "note.txt", "lost update", original.Version); err != status_error.WorkspaceResourceConflict {
		t.Fatalf("stale write: %v", err)
	}
	bytes, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) != "after" {
		t.Fatalf("overwritten: %q", bytes)
	}
	if err = os.WriteFile(filepath.Join(root, "occupied.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Rename("web:one", scope.ID, "", "note.txt", "occupied.txt", updated.Version); err != status_error.WorkspaceResourceConflict {
		t.Fatalf("overwrite rename: %v", err)
	}
	renamed, err := manager.Rename("web:one", scope.ID, "", "note.txt", "renamed.txt", updated.Version)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.RelativePath != "renamed.txt" {
		t.Fatal(renamed.RelativePath)
	}
	if err = manager.Delete("web:one", scope.ID, "", "renamed.txt", original.Version); err != status_error.WorkspaceResourceConflict {
		t.Fatalf("stale delete: %v", err)
	}
	if err = manager.Delete("web:one", scope.ID, "", "renamed.txt", renamed.Version); err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.Read("web:one", scope.ID, "", "renamed.txt"); err != status_error.WorkspaceResourceNotFound {
		t.Fatalf("deleted file: %v", err)
	}
}

func TestBoundedReadSearchAndCancellation(t *testing.T) {
	manager, scope, root := fixture(t)
	large, err := os.Create(filepath.Join(root, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err = large.Truncate(readLimit + 1); err != nil {
		t.Fatal(err)
	}
	large.Close()
	if _, _, err = manager.Read("web:one", scope.ID, "", "large.bin"); err != status_error.WorkspaceResourceTooLarge {
		t.Fatalf("large read: %v", err)
	}
	found, err := manager.Search(context.Background(), "web:one", scope.ID, "", "note", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].RelativePath != "note.txt" {
		t.Fatalf("search: %+v", found)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = manager.Search(ctx, "web:one", scope.ID, "", "", "", 1); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}
