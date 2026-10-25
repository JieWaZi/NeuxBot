package workspacefiles

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitFixture(t *testing.T) (*Manager, Scope, string) {
	t.Helper()
	m, s, r := fixture(t)
	gitRun(t, r, "init", "-b", "main")
	gitRun(t, r, "config", "user.email", "test@example.invalid")
	gitRun(t, r, "config", "user.name", "Workspace Test")
	gitRun(t, r, "add", ".")
	gitRun(t, r, "commit", "-m", "initial")
	return m, s, r
}
func gitRun(t *testing.T, r string, args ...string) string {
	t.Helper()
	b, e := exec.Command("git", append([]string{"-C", r}, args...)...).CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func TestGitScopeStatusStageCommitAndDiscard(t *testing.T) {
	m, s, r := gitFixture(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(r, "note.txt"), []byte("modified\n"), 0600)
	gitRun(t, r, "config", "color.diff", "always")
	status, e := m.Status(ctx, "web:one", s.ID, "", "")
	if e != nil || !status.IsRepo || len(status.Entries) != 1 {
		t.Fatalf("%+v %v", status, e)
	}
	if _, e = m.Status(ctx, "web:two", s.ID, "", ""); e != status_error.WorkspaceScopeInvalid {
		t.Fatal(e)
	}
	if _, e = m.Status(ctx, "web:one", s.ID, "", t.TempDir()); e != status_error.WorkspacePathInvalid {
		t.Fatal(e)
	}
	diff, e := m.Diff(ctx, "web:one", s.ID, "", "", "note.txt", false)
	if e != nil || !strings.Contains(diff, "modified") || strings.Contains(diff, "\x1b[") {
		t.Fatalf("%s %v", diff, e)
	}
	if e = m.ChangeIndex(ctx, "web:one", s.ID, "", "", "note.txt", "stage"); e != nil {
		t.Fatal(e)
	}
	if e = m.Commit(ctx, "web:one", s.ID, "", "", "change"); e != nil {
		t.Fatal(e)
	}
	logs, e := m.Log(ctx, "web:one", s.ID, "", "", 10, 0)
	if e != nil || len(logs) != 2 {
		t.Fatalf("%+v %v", logs, e)
	}
	shown, e := m.Show(ctx, "web:one", s.ID, "", "", "HEAD", "note.txt")
	if e != nil || shown != "modified\n" {
		t.Fatalf("%q %v", shown, e)
	}
	os.WriteFile(filepath.Join(r, "note.txt"), []byte("discard\n"), 0600)
	if e = m.ChangeIndex(ctx, "web:one", s.ID, "", "", "note.txt", "discard"); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(r, "note.txt"))
	if string(b) != "modified\n" {
		t.Fatal(string(b))
	}
	os.WriteFile(filepath.Join(r, "*.txt"), []byte("literal\n"), 0600)
	os.WriteFile(filepath.Join(r, "other.txt"), []byte("other\n"), 0600)
	if e = m.ChangeIndex(ctx, "web:one", s.ID, "", "", "*.txt", "stage"); e != nil {
		t.Fatal(e)
	}
	if got := gitRun(t, r, "diff", "--cached", "--name-only"); got != "*.txt" {
		t.Fatal(got)
	}
}
func TestGitCheckoutRefusesConflictingChanges(t *testing.T) {
	m, s, r := gitFixture(t)
	ctx := context.Background()
	gitRun(t, r, "switch", "-c", "other")
	os.WriteFile(filepath.Join(r, "note.txt"), []byte("other\n"), 0600)
	gitRun(t, r, "commit", "-am", "other")
	gitRun(t, r, "switch", "main")
	os.WriteFile(filepath.Join(r, "note.txt"), []byte("local\n"), 0600)
	preview, e := m.PreviewCheckout(ctx, "web:one", s.ID, "", "", "other")
	if e != nil || preview.CanCheckout || len(preview.Conflicts) != 1 {
		t.Fatalf("%+v %v", preview, e)
	}
	if _, e = m.Checkout(ctx, "web:one", s.ID, "", "", "other", ""); e != status_error.WorkspaceResourceConflict {
		t.Fatal(e)
	}
	if got := gitRun(t, r, "branch", "--show-current"); got != "main" {
		t.Fatal(got)
	}
}

func TestGitDiscoversFrozenProjectRoots(t *testing.T) {
	_, _, project := gitFixture(t)
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "projects"), 0700)
	os.Symlink(project, filepath.Join(root, "projects", "repo"))
	m := New()
	scope, err := m.Open("desktop", root, []string{project})
	if err != nil {
		t.Fatal(err)
	}
	status, err := m.Status(context.Background(), "desktop", scope.ID, "", project)
	if err != nil || !status.IsRepo || len(status.Repositories) != 1 {
		t.Fatalf("frozen project: %+v %v", status, err)
	}
	foreign := t.TempDir()
	os.Symlink(foreign, filepath.Join(root, "projects", "foreign"))
	if _, err = m.Status(context.Background(), "desktop", scope.ID, "", foreign); err != status_error.WorkspacePathInvalid {
		t.Fatalf("unfrozen: %v", err)
	}
}

func TestGitIgnoresHostRepositoryEnvironment(t *testing.T) {
	m, s, root := gitFixture(t)
	_, _, outside := gitFixture(t)
	os.WriteFile(filepath.Join(root, "note.txt"), []byte("authorized"), 0600)
	t.Setenv("GIT_DIR", filepath.Join(outside, ".git"))
	t.Setenv("GIT_WORK_TREE", outside)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(outside, "foreign-index"))
	status, err := m.Status(context.Background(), "web:one", s.ID, "", root)
	if err != nil || status.Root != s.Root || len(status.Entries) != 1 {
		t.Fatalf("host git env escaped: %+v %v", status, err)
	}
}

func TestGitUntrackedDiffUsesLiteralScopedPath(t *testing.T) {
	m, s, root := gitFixture(t)
	if err := os.WriteFile(filepath.Join(root, "*.txt"), []byte("new content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	patch, err := m.Diff(context.Background(), "web:one", s.ID, "", "", "*.txt", false)
	if err != nil || !strings.Contains(patch, "+new content") {
		t.Fatalf("%q %v", patch, err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("secret"), 0600)
	os.Symlink(outside, filepath.Join(root, "escape"))
	if _, err = m.Diff(context.Background(), "web:one", s.ID, "", "", "escape", false); err != status_error.WorkspacePathInvalid {
		t.Fatalf("escape: %v", err)
	}
}
