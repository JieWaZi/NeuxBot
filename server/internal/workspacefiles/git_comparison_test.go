package workspacefiles

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestGitComparisonCombinesChangesSinceMergeBase(t *testing.T) {
	m, scope, root := gitFixture(t)
	ctx := context.Background()
	gitRun(t, root, "switch", "-c", "feature")
	writeComparisonFile(t, root, "branch.txt", "branch change\n")
	gitRun(t, root, "add", "branch.txt")
	gitRun(t, root, "commit", "-m", "feature")
	gitRun(t, root, "switch", "main")
	writeComparisonFile(t, root, "main.txt", "other branch only\n")
	gitRun(t, root, "add", "main.txt")
	gitRun(t, root, "commit", "-m", "main change")
	gitRun(t, root, "switch", "feature")
	writeComparisonFile(t, root, "new.txt", "untracked\nnext line\n")
	writeComparisonFile(t, root, "binary.dat", "binary\x00content")
	indexBefore := gitRun(t, root, "ls-files", "--stage")

	summary, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "", false)
	if err != nil || summary.BaseRef != "main" || summary.Branch != "feature" || summary.Content != "" {
		t.Fatalf("comparison metadata: %+v %v", summary, err)
	}
	want := []GitComparisonEntry{
		{Path: "binary.dat", Status: "A", LineCountUnavailable: true},
		{Path: "branch.txt", Status: "A", AddedLines: 1},
		{Path: "new.txt", Status: "A", AddedLines: 2},
	}
	if !slices.Equal(summary.Entries, want) {
		t.Fatalf("entries: %+v", summary.Entries)
	}
	patch, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "new.txt", false)
	if err != nil || !strings.Contains(patch.Content, "+next line") || strings.Contains(patch.Content, "branch change") {
		t.Fatalf("selected patch: %+v %v", patch, err)
	}
	if got := gitRun(t, root, "ls-files", "--stage"); got != indexBefore {
		t.Fatal("comparison mutated the real index")
	}
	if _, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "main~1", "", false); err != status_error.WorkspacePathInvalid {
		t.Fatalf("non-branch revision: %v", err)
	}
	if _, err := m.Comparison(ctx, "web:two", scope.ID, "", "", "", "", false); err != status_error.WorkspaceScopeInvalid {
		t.Fatalf("foreign scope: %v", err)
	}
}

func TestGitComparisonPreservesRenameAndDifferentIndexStates(t *testing.T) {
	m, scope, root := gitFixture(t)
	ctx := context.Background()
	gitRun(t, root, "mv", "note.txt", "renamed.txt")
	writeComparisonFile(t, root, "renamed.txt", "work tree\n")

	patch, err := m.Comparison(ctx, "web:one", scope.ID, "", "", ":staged", "renamed.txt", false)
	if err != nil || !slices.Equal(patch.Entries, []GitComparisonEntry{{Path: "renamed.txt", OldPath: "note.txt", Status: "R"}}) ||
		!strings.Contains(patch.Content, "rename from note.txt\nrename to renamed.txt") {
		t.Fatalf("rename patch: %+v %v", patch, err)
	}
	patch, err = m.Comparison(ctx, "web:one", scope.ID, "", "", ":unstaged", "renamed.txt", false)
	if err != nil || !strings.Contains(patch.Content, "+work tree") {
		t.Fatalf("work tree patch: %+v %v", patch, err)
	}
	first := gitRun(t, root, "rev-parse", "HEAD")
	commit, err := m.Comparison(ctx, "web:one", scope.ID, "", "", ":commit:"+first, "note.txt", false)
	if err != nil || !slices.Equal(commit.Entries, []GitComparisonEntry{{Path: "note.txt", Status: "A", AddedLines: 1}}) || !strings.Contains(commit.Content, "--- /dev/null") {
		t.Fatalf("first commit comparison: %+v %v", commit, err)
	}
}

func TestGitComparisonHidesWhitespaceOnlyFiles(t *testing.T) {
	m, scope, root := gitFixture(t)
	ctx := context.Background()
	writeComparisonFile(t, root, "spaces.txt", "one two\n")
	gitRun(t, root, "add", "spaces.txt")
	gitRun(t, root, "commit", "-m", "spaces")
	writeComparisonFile(t, root, "spaces.txt", "one  two\n")

	normal, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "", false)
	if err != nil || normal.BaseRef != "HEAD" || len(normal.Entries) != 1 {
		t.Fatalf("default comparison: %+v %v", normal, err)
	}
	filtered, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "", true)
	if err != nil || len(filtered.Entries) != 0 {
		t.Fatalf("filtered comparison: %+v %v", filtered, err)
	}
}

func TestGitComparisonUsesConfiguredRemoteDefaultBranch(t *testing.T) {
	m, scope, root := gitFixture(t)
	ctx := context.Background()
	gitRun(t, root, "remote", "add", "upstream", filepath.Join(t.TempDir(), "remote"))
	gitRun(t, root, "update-ref", "refs/remotes/upstream/stable", "HEAD")
	gitRun(t, root, "symbolic-ref", "refs/remotes/upstream/HEAD", "refs/remotes/upstream/stable")
	gitRun(t, root, "switch", "-c", "feature", "--track", "upstream/stable")
	writeComparisonFile(t, root, "note.txt", "feature change\n")
	gitRun(t, root, "commit", "-am", "feature")

	result, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "", false)
	if err != nil || result.BaseRef != "upstream/stable" || len(result.Entries) != 1 {
		t.Fatalf("remote comparison: %+v %v", result, err)
	}
}

func TestGitComparisonUnbornAndOversizedPatch(t *testing.T) {
	m, scope, root := fixture(t)
	ctx := context.Background()
	gitRun(t, root, "init", "-b", "new")
	writeComparisonFile(t, root, "oversized.txt", strings.Repeat("A", gitComparisonMaxPatch+1)+"\n")
	result, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "oversized.txt", false)
	if err != nil || !result.Truncated || result.Content != "" || result.BaseRef != "HEAD" || result.Branch != "new" ||
		!slices.Equal(result.Entries, []GitComparisonEntry{{Path: "oversized.txt", Status: "A", AddedLines: 1}}) {
		t.Fatalf("unborn oversized patch: %+v %v", result, err)
	}
}

func TestGitComparisonHeadOmitsIndexChangesRevertedInWorkTree(t *testing.T) {
	m, scope, root := gitFixture(t)
	ctx := context.Background()
	original, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	writeComparisonFile(t, root, "note.txt", "staged change\n")
	gitRun(t, root, "add", "note.txt")
	writeComparisonFile(t, root, "note.txt", string(original))

	net, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "HEAD", "", false)
	if err != nil || len(net.Entries) != 0 {
		t.Fatalf("HEAD vs worktree: %+v %v", net, err)
	}
	for _, source := range []string{":staged", ":unstaged"} {
		view, err := m.Comparison(ctx, "web:one", scope.ID, "", "", source, "", false)
		if err != nil || len(view.Entries) != 1 || view.Entries[0].Path != "note.txt" {
			t.Fatalf("%s: %+v %v", source, view, err)
		}
	}
}

func TestGitLogComparisonOnlyShowsCommitsAfterSelectedMergeBase(t *testing.T) {
	m, scope, root := gitFixture(t)
	ctx := context.Background()
	gitRun(t, root, "switch", "-c", "feature")
	gitRun(t, root, "commit", "--allow-empty", "-m", "feature first")
	gitRun(t, root, "branch", "review-base")
	gitRun(t, root, "commit", "--allow-empty", "-m", "feature second")
	gitRun(t, root, "switch", "main")
	gitRun(t, root, "commit", "--allow-empty", "-m", "main only")
	gitRun(t, root, "switch", "feature")

	for _, base := range []string{":auto", "main", "review-base", "HEAD"} {
		entries, err := m.LogComparison(ctx, "web:one", scope.ID, "", "", base, 100, 0)
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		subjects := []string{}
		for _, entry := range entries {
			subjects = append(subjects, entry.Subject)
		}
		switch base {
		case ":auto", "main":
			if !slices.Equal(subjects, []string{"feature second", "feature first"}) {
				t.Fatalf("%s: %+v", base, subjects)
			}
		case "review-base":
			if !slices.Equal(subjects, []string{"feature second"}) {
				t.Fatalf("%s: %+v", base, subjects)
			}
		case "HEAD":
			if len(subjects) != 0 {
				t.Fatalf("%s: %+v", base, subjects)
			}
		}
	}
	if _, err := m.LogComparison(ctx, "web:one", scope.ID, "", "", ":staged", 100, 0); err != status_error.WorkspacePathInvalid {
		t.Fatalf("invalid base: %v", err)
	}
	legacy, err := m.Log(ctx, "web:one", scope.ID, "", "", 100, 0)
	if err != nil || len(legacy) != 3 || legacy[2].Subject != "initial" {
		t.Fatalf("whole history: %+v %v", legacy, err)
	}
}

func writeComparisonFile(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGitComparisonExcludesAccountPathsRegardlessOfCase(t *testing.T) {
	m, scope, repo := gitFixture(t)
	protected := "Runtime/Accounts/private.txt"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, protected)), 0700); err != nil {
		t.Fatal(err)
	}
	writeComparisonFile(t, repo, protected, "protected contents\n")
	gitRun(t, repo, "add", "--", protected)
	writeComparisonFile(t, repo, "visible.txt", "visible contents\n")
	result, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, "", "", false)
	if err != nil || len(result.Entries) != 1 || result.Entries[0].Path != "visible.txt" {
		t.Fatalf("comparison exposed protected paths: %+v %v", result, err)
	}
	if _, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, ":staged", protected, false); err != status_error.WorkspacePathInvalid {
		t.Fatalf("explicit account comparison was accepted: %v", err)
	}
}
