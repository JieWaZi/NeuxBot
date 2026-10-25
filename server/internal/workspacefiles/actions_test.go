package workspacefiles

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDirectoryImportAndResolve(t *testing.T) {
	m, s, root := fixture(t)
	os.Mkdir(filepath.Join(root, "folder"), 0700)
	if _, e := m.Import("web:one", s.ID, "", "folder", "a.txt", []byte("new")); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Import("web:one", s.ID, "", "folder", "a.txt", nil); e != status_error.WorkspaceResourceConflict {
		t.Fatal(e)
	}
	if _, e := m.Import("web:one", s.ID, "", "folder", "../bad", nil); e != status_error.WorkspacePathInvalid {
		t.Fatal(e)
	}
	r, e := m.ResolveReference("web:one", s.ID, "folder/a.txt", root)
	if e != nil || r.Status != "resolved" {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = m.RenameDirectory("web:one", s.ID, "", "folder", "renamed"); e != nil {
		t.Fatal(e)
	}
	if e = m.DeleteDirectory("web:one", s.ID, "", ""); e != status_error.WorkspacePathInvalid {
		t.Fatal(e)
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep"), []byte("safe"), 0600)
	os.Symlink(outside, filepath.Join(root, "renamed", "outside"))
	if e = m.DeleteDirectory("web:one", s.ID, "", "renamed"); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(outside, "keep")); e != nil {
		t.Fatal("followed external link", e)
	}
	r, e = m.ResolveReference("web:one", s.ID, filepath.Join(outside, "keep"), root)
	if e != nil || r.Status != "outside_scope" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestChatFilesPreserveProjectsAndRestoreAttachments(t *testing.T) {
	m, s, root := fixture(t)
	list, e := m.ListChatFiles(context.Background(), "web:one", s.ID, "")
	if e != nil || len(list.Generated) != 0 {
		t.Fatalf("%+v %v", list, e)
	}
	os.MkdirAll(filepath.Join(root, "attachments/generated"), 0700)
	os.WriteFile(filepath.Join(root, "attachments/generated/out.txt"), []byte("out"), 0600)
	if e = m.DeleteChatFile("web:one", s.ID, "", "note.txt"); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "note.txt")); e != nil {
		t.Fatal("project removed", e)
	}
	if e = m.DeleteChatFile("web:one", s.ID, "", "attachments/generated/out.txt"); e != nil {
		t.Fatal(e)
	}
	list, e = m.ListChatFiles(context.Background(), "web:one", s.ID, "")
	if e != nil || len(list.RemovedPaths) != 2 || len(list.Generated) != 0 {
		t.Fatalf("%+v %v", list, e)
	}
	os.WriteFile(filepath.Join(root, "attachments/generated/out.txt"), []byte("again"), 0600)
	list, e = m.ListChatFiles(context.Background(), "web:one", s.ID, "")
	if e != nil || len(list.RemovedPaths) != 1 || len(list.Generated) != 1 {
		t.Fatalf("%+v %v", list, e)
	}
	os.Remove(filepath.Join(root, removedManifest))
	os.Symlink(filepath.Join(root, "note.txt"), filepath.Join(root, removedManifest))
	if e = m.DeleteChatFile("web:one", s.ID, "", "note.txt"); e != status_error.WorkspacePathInvalid {
		t.Fatal(e)
	}
}
func TestWatchFileAndRootDirectoryCancellation(t *testing.T) {
	m, s, root := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready, events, closeWatch, e := m.Watch(ctx, "web:one", s.ID, "note.txt", "", false)
	if e != nil {
		t.Fatal(e)
	}
	defer closeWatch()
	if ready.Sequence != 1 || ready.Resource == nil {
		t.Fatal(ready)
	}
	os.WriteFile(filepath.Join(root, "note.txt"), []byte("changed"), 0600)
	select {
	case event := <-events:
		if event.Sequence <= 1 || event.Resource == nil || event.Resource.Version == ready.Resource.Version {
			t.Fatal(event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing file event")
	}
	_, events2, close2, e := m.Watch(ctx, "web:one", s.ID, "", "", true)
	if e != nil {
		t.Fatal(e)
	}
	defer close2()
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("new"), 0600)
	select {
	case event := <-events2:
		if event.State != "changed" && event.State != "recovering" {
			t.Fatal(event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing directory event")
	}
	cancel()
	close2()
	closeWatch()
}

func TestDirectoryMutationCannotRemoveAccountParent(t *testing.T) {
	m, s, root := fixture(t)
	if err := os.MkdirAll(filepath.Join(root, "runtime/accounts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteDirectory("web:one", s.ID, "", "runtime"); err != status_error.WorkspacePathInvalid {
		t.Fatalf("account parent delete: %v", err)
	}
	if _, err := m.RenameDirectory("web:one", s.ID, "", "runtime", "moved"); err != status_error.WorkspacePathInvalid {
		t.Fatalf("account parent rename: %v", err)
	}
}
