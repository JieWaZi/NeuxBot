package workspacefiles

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestGitComparisonLeavesRealObjectStoreUntouched(t *testing.T) {
	for _, name := range []string{"missing empty blob", "loose empty blob", "packed empty blob"} {
		t.Run(name, func(t *testing.T) {
			m, scope, repo := gitFixture(t)
			writeComparisonFile(t, repo, "new.txt", "untracked content\n")
			empty := gitRun(t, repo, "hash-object", "--stdin")
			objects := gitRun(t, repo, "rev-parse", "--path-format=absolute", "--git-path", "objects")
			objectFile := filepath.Join(objects, empty[:2], empty[2:])
			originalTime := time.Unix(1000000000, 0)
			switch name {
			case "loose empty blob":
				gitRun(t, repo, "hash-object", "-w", "--stdin")
			case "packed empty blob":
				writeComparisonFile(t, repo, "empty.txt", "")
				gitRun(t, repo, "add", "empty.txt")
				gitRun(t, repo, "commit", "-m", "store empty blob")
				gitRun(t, repo, "gc", "--prune=now")
				packs, err := filepath.Glob(filepath.Join(objects, "pack", "*.pack"))
				if err != nil || len(packs) != 1 {
					t.Fatalf("expected one repository pack: %v %v", packs, err)
				}
				objectFile = packs[0]
			}
			existing := name != "missing empty blob"
			if existing {
				if err := os.Chtimes(objectFile, originalTime, originalTime); err != nil {
					t.Fatal(err)
				}
			}

			for _, path := range []string{"", "new.txt"} {
				result, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, "HEAD", path, false, nil)
				if err != nil || len(result.Entries) != 1 || result.Entries[0].Path != "new.txt" || result.Entries[0].Status != "A" {
					t.Fatalf("comparison %q: %+v %v", path, result, err)
				}
				if path != "" && !strings.Contains(result.Content, "+untracked content") {
					t.Fatalf("untracked patch: %q", result.Content)
				}
				info, statErr := os.Stat(objectFile)
				if existing {
					if statErr != nil || !info.ModTime().Equal(originalTime) {
						t.Fatalf("comparison %q refreshed the repository object file: %v %v", path, info, statErr)
					}
				} else if !os.IsNotExist(statErr) {
					t.Fatalf("comparison %q created the real empty blob: %v", path, statErr)
				}
			}
		})
	}
}

func TestGitComparisonKeepsWorkspaceWatchQuiet(t *testing.T) {
	for _, mode := range []string{"normal index", "split index"} {
		t.Run(mode, func(t *testing.T) {
			m, scope, repo := gitFixture(t)
			gitRun(t, repo, "hash-object", "-w", "--stdin")
			if mode == "split index" {
				gitRun(t, repo, "update-index", "--split-index")
			}
			writeComparisonFile(t, repo, "new.txt", "untracked content\n")
			_, changes, stop, err := m.Watch(t.Context(), scope.subject, scope.ID, "", repo, true)
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			// FSEvents 可能在订阅建立后交付夹具准备阶段已排队的写入事件。
			setupEvents := time.NewTimer(300 * time.Millisecond)
			defer setupEvents.Stop()
		settled:
			for {
				select {
				case event := <-changes:
					if event.State != "changed" {
						t.Fatalf("workspace watch unavailable: %+v", event)
					}
				case <-setupEvents.C:
					break settled
				}
			}
			if result, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, "HEAD", "", false, nil); err != nil || len(result.Entries) != 1 {
				t.Fatalf("comparison: %+v %v", result, err)
			}
			select {
			case event := <-changes:
				t.Fatalf("comparison triggered a workspace change: %+v", event)
			case <-time.After(300 * time.Millisecond):
			}
			gitRun(t, repo, "switch", "-c", "feature")
			select {
			case event := <-changes:
				if event.State != "changed" {
					t.Fatalf("expected a branch change: %+v", event)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("workspace watch missed the real branch change")
			}
		})
	}
}

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

	summary, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "", false, nil)
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
	patch, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "new.txt", false, nil)
	if err != nil || !strings.Contains(patch.Content, "+next line") || strings.Contains(patch.Content, "branch change") {
		t.Fatalf("selected patch: %+v %v", patch, err)
	}
	if got := gitRun(t, root, "ls-files", "--stage"); got != indexBefore {
		t.Fatal("comparison mutated the real index")
	}
	if _, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "main~1", "", false, nil); err != status_error.WorkspacePathInvalid {
		t.Fatalf("non-branch revision: %v", err)
	}
	if _, err := m.Comparison(ctx, "web:two", scope.ID, "", "", "", "", false, nil); err != status_error.WorkspaceScopeInvalid {
		t.Fatalf("foreign scope: %v", err)
	}
}

func TestGitComparisonPreservesRenameAndDifferentIndexStates(t *testing.T) {
	m, scope, root := gitFixture(t)
	ctx := context.Background()
	gitRun(t, root, "mv", "note.txt", "renamed.txt")
	writeComparisonFile(t, root, "renamed.txt", "work tree\n")

	previous := "note.txt"
	patch, err := m.Comparison(ctx, "web:one", scope.ID, "", "", ":staged", "renamed.txt", false, &previous)
	if err != nil || !slices.Equal(patch.Entries, []GitComparisonEntry{{Path: "renamed.txt", OldPath: "note.txt", Status: "R"}}) ||
		!strings.Contains(patch.Content, "rename from note.txt\nrename to renamed.txt") {
		t.Fatalf("rename patch: %+v %v", patch, err)
	}
	patch, err = m.Comparison(ctx, "web:one", scope.ID, "", "", ":unstaged", "renamed.txt", false, nil)
	if err != nil || !strings.Contains(patch.Content, "+work tree") {
		t.Fatalf("work tree patch: %+v %v", patch, err)
	}
	first := gitRun(t, root, "rev-parse", "HEAD")
	commit, err := m.Comparison(ctx, "web:one", scope.ID, "", "", ":commit:"+first, "note.txt", false, nil)
	if err != nil || !slices.Equal(commit.Entries, []GitComparisonEntry{{Path: "note.txt", Status: "A", AddedLines: 1}}) || !strings.Contains(commit.Content, "--- /dev/null") {
		t.Fatalf("first commit comparison: %+v %v", commit, err)
	}
}

func TestGitComparisonUsesObservedRenamePair(t *testing.T) {
	m, scope, repo := gitFixture(t)
	oldPath := "old[1].txt"
	newPath := "new?.txt"
	writeComparisonFile(t, repo, oldPath, "original content\nunchanged\nmore\n")
	writeComparisonFile(t, repo, "other.txt", "unrelated contents\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "paths")
	gitRun(t, repo, "mv", "--", oldPath, newPath)
	gitRun(t, repo, "rm", "--", "other.txt")
	for _, tt := range []struct {
		// 标识子用例。
		name string
		// 文件列表观察到的旧路径。
		previous string
		// 补丁及响应匹配的旧路径。
		wantOld string
		// 补丁中对应 Git 变更状态的提示。
		marker string
	}{
		{name: "rename", previous: oldPath, wantOld: oldPath, marker: "rename from old[1].txt"},
		{name: "known addition", marker: "--- /dev/null"},
		{name: "stale rename", previous: "other.txt", marker: "--- /dev/null"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			patch, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, ":staged", newPath, false, &tt.previous)
			if err != nil || len(patch.Entries) != 1 || patch.Entries[0].Path != newPath || patch.Entries[0].OldPath != tt.wantOld ||
				!strings.Contains(patch.Content, tt.marker) || strings.Contains(patch.Content, "unrelated contents") {
				t.Fatalf("scoped patch: %+v %v", patch, err)
			}
		})
	}
}

func TestGitComparisonFileReplacementOnlyReturnsRequestedPatch(t *testing.T) {
	m, scope, repo := gitFixture(t)
	if err := os.Remove(filepath.Join(repo, "note.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, "note.txt"), 0700); err != nil {
		t.Fatal(err)
	}
	writeComparisonFile(t, repo, "note.txt/child.txt", "unrelated child contents\n")
	gitRun(t, repo, "add", "-A")
	previous := ""
	patch, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, ":staged", "note.txt", false, &previous)
	if err != nil || len(patch.Entries) != 1 || patch.Entries[0].Path != "note.txt" || patch.Entries[0].Status != "D" ||
		!strings.Contains(patch.Content, "+++ /dev/null") || strings.Contains(patch.Content, "unrelated child contents") {
		t.Fatalf("parent-only patch: %+v %v", patch, err)
	}
}

func TestGitComparisonRenameIntoDirectoryExcludesSiblingPatches(t *testing.T) {
	m, scope, repo := gitFixture(t)
	oldPath, newPath := "旧文件", "旧文件/new?.txt"
	writeComparisonFile(t, repo, oldPath, "rename contents\n")
	gitRun(t, repo, "add", "--", oldPath)
	gitRun(t, repo, "commit", "-m", "original file")
	if err := os.Remove(filepath.Join(repo, oldPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, oldPath), 0700); err != nil {
		t.Fatal(err)
	}
	writeComparisonFile(t, repo, newPath, "rename contents\n")
	writeComparisonFile(t, repo, oldPath+"/sibling.txt", "unrelated sibling contents\n")
	gitRun(t, repo, "add", "-A")
	patch, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, ":staged", newPath, false, &oldPath)
	if err != nil || len(patch.Entries) != 1 || patch.Entries[0].Path != newPath || patch.Entries[0].OldPath != oldPath ||
		!strings.Contains(patch.Content, "rename from ") || strings.Contains(patch.Content, "unrelated sibling contents") {
		t.Fatalf("rename-only patch: %+v %v", patch, err)
	}
}

func TestGitComparisonRejectsInvalidPreviousPath(t *testing.T) {
	m, scope, repo := gitFixture(t)
	for _, previous := range []string{"note.txt", "../old.txt", "./old.txt", "Runtime/Accounts/private.txt"} {
		if _, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, ":staged", "note.txt", false, &previous); err != status_error.WorkspacePathInvalid {
			t.Fatalf("accepted %q: %v", previous, err)
		}
	}
}

func TestGitComparisonHidesWhitespaceOnlyFiles(t *testing.T) {
	m, scope, root := gitFixture(t)
	ctx := context.Background()
	writeComparisonFile(t, root, "spaces.txt", "one two\n")
	gitRun(t, root, "add", "spaces.txt")
	gitRun(t, root, "commit", "-m", "spaces")
	writeComparisonFile(t, root, "spaces.txt", "one  two\n")

	normal, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "", false, nil)
	if err != nil || normal.BaseRef != "HEAD" || len(normal.Entries) != 1 {
		t.Fatalf("default comparison: %+v %v", normal, err)
	}
	filtered, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "", true, nil)
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

	result, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "", false, nil)
	if err != nil || result.BaseRef != "upstream/stable" || len(result.Entries) != 1 {
		t.Fatalf("remote comparison: %+v %v", result, err)
	}
}

func TestGitComparisonUnbornAndOversizedPatch(t *testing.T) {
	m, scope, root := fixture(t)
	ctx := context.Background()
	gitRun(t, root, "init", "-b", "new")
	writeComparisonFile(t, root, "oversized.txt", strings.Repeat("A", gitComparisonMaxPatch+1)+"\n")
	result, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "", "oversized.txt", false, nil)
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

	net, err := m.Comparison(ctx, "web:one", scope.ID, "", "", "HEAD", "", false, nil)
	if err != nil || len(net.Entries) != 0 {
		t.Fatalf("HEAD vs worktree: %+v %v", net, err)
	}
	for _, source := range []string{":staged", ":unstaged"} {
		view, err := m.Comparison(ctx, "web:one", scope.ID, "", "", source, "", false, nil)
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
	result, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, "", "", false, nil)
	if err != nil || len(result.Entries) != 1 || result.Entries[0].Path != "visible.txt" {
		t.Fatalf("comparison exposed protected paths: %+v %v", result, err)
	}
	if _, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, ":staged", protected, false, nil); err != status_error.WorkspacePathInvalid {
		t.Fatalf("explicit account comparison was accepted: %v", err)
	}
}
