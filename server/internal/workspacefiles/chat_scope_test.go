package workspacefiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestChatScopesSeparatePrivateFilesWithSharedAgentDirectory(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(base, "Agents", "waker")
	project := filepath.Join(base, "project")
	for _, dir := range []string{cwd, project} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, "report.txt"), []byte("project"), 0600); err != nil {
		t.Fatal(err)
	}
	m := New()
	scopes := []Scope{}
	for _, chat := range []string{"a", "b"} {
		private := filepath.Join(base, "Conversations", chat)
		if err := os.MkdirAll(filepath.Join(private, "attachments", "input"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{"log", "credential", "attachments/input/note.txt"} {
			if err := os.WriteFile(filepath.Join(private, p), []byte(chat), 0600); err != nil {
				t.Fatal(err)
			}
		}
		scope, err := m.OpenChat("subject", chat, cwd, private, []string{project})
		if err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, scope)
	}
	if scopes[0].ID == scopes[1].ID {
		t.Fatal("same cwd reused another Chat's private scope")
	}
	if err := m.DeleteChatFile("subject", scopes[0].ID, "", "attachments/input/note.txt"); err != nil {
		t.Fatal(err)
	}
	for i, scope := range scopes {
		files, err := m.ListChatFiles(context.Background(), "subject", scope.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(files.RemovedPaths) != 1-i {
			t.Fatalf("chat %d hidden paths: %+v", i, files)
		}
		if _, _, err := m.Read("subject", scope.ID, project, "report.txt"); err != nil {
			t.Fatal(err)
		}
		resolution, err := m.ResolveReference("subject", scope.ID, filepath.Join(project, "report.txt"), "")
		if err != nil || resolution.Status != "resolved" {
			t.Fatalf("project reference: %+v %v", resolution, err)
		}
		private := filepath.Join(base, "Conversations", []string{"a", "b"}[i])
		for _, p := range []string{"log", "credential", removedManifest} {
			if _, _, err := m.Read("subject", scope.ID, private, p); err != status_error.WorkspacePathInvalid {
				t.Fatalf("private read %s: %v", p, err)
			}
		}
	}
	if _, _, err := m.Read("subject", scopes[0].ID, filepath.Join(base, "Conversations", "b", "attachments"), "input/note.txt"); err != status_error.WorkspacePathInvalid {
		t.Fatalf("foreign attachments: %v", err)
	}
	if _, _, err := m.Read("subject", scopes[1].ID, "", "attachments/input/note.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "Conversations", "b"), filepath.Join(cwd, "private")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Read("subject", scopes[0].ID, "", "private/credential"); err != status_error.WorkspacePathInvalid {
		t.Fatalf("private symlink: %v", err)
	}
}
