package workspacefiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
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

func TestSpaceListingHidesInternalFilesWithoutFilteringRealWorkingDirectory(t *testing.T) {
	base := t.TempDir()
	cwd, private, project := filepath.Join(base, "Agents", "waker"), filepath.Join(base, "Conversations", "chat"), filepath.Join(base, "project")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{cwd, project} {
		for _, name := range []string{"report.txt", ".claude/settings.json", ".pi/skills/test/SKILL.md", ".grok/skills/test/SKILL.md", ".agents/skills/test/SKILL.md", ".neuxbot-skills.json", "workspace-owner.json"} {
			path := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	m := New()
	if _, _, err := workspace.PrepareProjectEntries(cwd, "chat", []workspace.ProjectLink{{EntryName: "project", SourcePath: project}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "projects", "notes.txt"), []byte("space notes"), 0600); err != nil {
		t.Fatal(err)
	}
	scope, err := m.OpenChat("subject", "chat", cwd, private, []string{project})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := m.List("subject", scope.ID, "", "")
	if err != nil || len(entries) != 2 || entries[0].Name != "projects" || entries[1].Name != "report.txt" {
		t.Fatalf("space listing exposed internals: %+v %v", entries, err)
	}
	matches, err := m.Search(context.Background(), "subject", scope.ID, "", "", "", 100)
	if err != nil || len(matches) != 2 || matches[0].Name != "report.txt" || matches[1].RelativePath != "projects/notes.txt" {
		t.Fatalf("space search exposed internals: %+v %v", matches, err)
	}
	entries, err = m.List("subject", scope.ID, project, "")
	if err != nil || len(entries) != 7 {
		t.Fatalf("real working directory was filtered: %+v %v", entries, err)
	}
}

func TestLogicalAttachmentDoesNotResolveSpaceFileThroughSymlink(t *testing.T) {
	base := t.TempDir()
	cwd, private := filepath.Join(base, "Agents", "waker"), filepath.Join(base, "Conversations", "chat")
	for _, root := range []string{cwd, private} {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	spaceFile := filepath.Join(cwd, "note.txt")
	if err := os.WriteFile(spaceFile, []byte("space"), 0600); err != nil {
		t.Fatal(err)
	}
	m := New()
	scope, err := m.OpenChat("subject", "chat", cwd, private, nil)
	if err != nil {
		t.Fatal(err)
	}
	relative := "attachments/alias.txt"
	if err := os.Symlink(spaceFile, filepath.Join(private, relative)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Read("subject", scope.ID, "", relative); err != status_error.WorkspacePathInvalid {
		t.Fatalf("logical attachment read a space file: %v", err)
	}
	resolution, err := m.ResolveReference("subject", scope.ID, relative, "")
	if err != nil || resolution.Status != "outside_scope" {
		t.Fatalf("logical attachment resolved a space file: %+v %v", resolution, err)
	}
}

func TestSpaceAndChatAttachmentsWithSameRelativePathKeepTheirOwnIdentity(t *testing.T) {
	base := t.TempDir()
	cwd, private := filepath.Join(base, "Agents", "waker"), filepath.Join(base, "Conversations", "chat")
	relative := filepath.Join("attachments", "message", "note.txt")
	for _, root := range []string{cwd, private} {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(root), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := New()
	scope, err := m.OpenChat("subject", "chat", cwd, private, nil)
	if err != nil {
		t.Fatal(err)
	}
	actualSpace, _ := filepath.EvalSymlinks(cwd)
	actualPrivate, _ := filepath.EvalSymlinks(private)
	for _, root := range []string{"", actualSpace} {
		_, content, err := m.Read("subject", scope.ID, root, filepath.ToSlash(relative))
		expected := private
		if root != "" {
			expected = cwd
		}
		if err != nil || string(content) != expected {
			t.Fatalf("root %q read wrong attachment: %q %v", root, content, err)
		}
	}
	logical, err := m.ResolveReference("subject", scope.ID, filepath.ToSlash(relative), "")
	if err != nil || logical.Status != "resolved" || logical.CanonicalPath != filepath.Join(actualPrivate, relative) || logical.WorkspaceRoot != filepath.Join(actualPrivate, "attachments") {
		t.Fatalf("chat attachment identity: %+v %v", logical, err)
	}
	physical, err := m.ResolveReference("subject", scope.ID, filepath.ToSlash(relative), actualSpace)
	if err != nil || physical.Status != "resolved" || physical.CanonicalPath != filepath.Join(actualSpace, relative) {
		t.Fatalf("space attachment identity: %+v %v", physical, err)
	}
	spaceFile := filepath.Join(cwd, relative)
	if err := os.Remove(spaceFile); err != nil {
		t.Fatal(err)
	}
	legacy, err := m.ResolveReference("subject", scope.ID, filepath.ToSlash(relative), actualSpace)
	if err != nil || legacy.Status != "resolved" || legacy.CanonicalPath != logical.CanonicalPath {
		t.Fatalf("legacy message reference lost its Chat attachment: %+v %v", legacy, err)
	}
	if _, _, err := m.Read("subject", scope.ID, actualSpace, filepath.ToSlash(relative)); err != status_error.WorkspaceResourceNotFound {
		t.Fatalf("explicit space read fell back to a Chat attachment: %v", err)
	}
	outside := filepath.Join(base, "unauthorized.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, spaceFile); err != nil {
		t.Fatal(err)
	}
	forbidden, err := m.ResolveReference("subject", scope.ID, filepath.ToSlash(relative), actualSpace)
	if err != nil || forbidden.Status != "outside_scope" {
		t.Fatalf("forbidden space target fell back to a Chat attachment: %+v %v", forbidden, err)
	}
	if err := m.DeleteChatFile("subject", scope.ID, actualSpace, filepath.ToSlash(relative)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cwd, relative)); err != nil {
		t.Fatalf("chat deletion removed space file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(private, relative)); !os.IsNotExist(err) {
		t.Fatalf("chat original survived deletion: %v", err)
	}
}
