package changes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

func write(t *testing.T, root, path, text string) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(gitEnvironment(), "GIT_AUTHOR_NAME=Snapshot Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Snapshot Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, data)
	}
	return string(data)
}

func begin(t *testing.T, owned, execution string, roots []string, options Options) *Capture {
	t.Helper()
	capture, err := Begin(context.Background(), owned, execution, roots, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := capture.Close(); err != nil {
			t.Error(err)
		}
	})
	if capture.Summary().State != "pending" {
		t.Fatalf("baseline unavailable: %#v", capture.Summary())
	}
	return capture
}

func finish(t *testing.T, capture *Capture) *Summary {
	t.Helper()
	summary, err := capture.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.State != "ready" && summary.State != "partial" {
		t.Fatalf("finish unavailable: %#v", summary)
	}
	return summary
}

func fileMap(summary *Summary) map[string]File {
	values := map[string]File{}
	for _, file := range summary.Files {
		values[file.Path] = file
	}
	return values
}

func TestExactFileCaptureKeepsNetChangesWithoutReadingSiblings(t *testing.T) {
	for _, action := range []string{"unchanged", "modified", "atomic", "deleted", "symlink", "directory", "parent"} {
		t.Run(action, func(t *testing.T) {
			root, owned := t.TempDir(), t.TempDir()
			root, _ = filepath.EvalSymlinks(root)
			selected := filepath.Join(root, "report.txt")
			write(t, root, "report.txt", "original\n")
			write(t, root, "sibling.txt", "unselected\n")
			capture := begin(t, owned, action, nil, Options{FilePaths: []string{selected, selected}, Limits: Limits{MaxEntries: 1}})
			if roots := capture.Summary().Roots; len(roots) != 1 || roots[0].Path != selected || !roots[0].File {
				t.Fatalf("exact authorization became a directory: %+v", roots)
			}
			write(t, root, "sibling.txt", "must not be captured\n")
			switch action {
			case "modified":
				write(t, root, "report.txt", "revised\n")
			case "atomic":
				write(t, root, "replacement", "revised\n")
				if err := os.Rename(filepath.Join(root, "replacement"), selected); err != nil {
					t.Fatal(err)
				}
			case "parent":
				if err := os.Rename(root, root+"-old"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(root + "-old") })
				write(t, root, "report.txt", "replacement parent must not rebind scope\n")
			case "deleted", "symlink", "directory":
				if err := os.Remove(selected); err != nil {
					t.Fatal(err)
				}
				if action == "symlink" {
					outside := filepath.Join(t.TempDir(), "secret.txt")
					write(t, filepath.Dir(outside), "secret.txt", "outside grant\n")
					if err := os.Symlink(outside, selected); err != nil {
						t.Skipf("symlink unavailable: %v", err)
					}
				}
				if action == "directory" {
					write(t, selected, "secret.txt", "must not enumerate replacement directory\n")
				}
			}
			summary := finish(t, capture)
			if action == "unchanged" || action == "symlink" || action == "directory" || action == "parent" {
				if len(summary.Files) != 0 || (action != "unchanged" && summary.Complete) {
					t.Fatalf("no net edit or unsafe replacement produced an edit: %+v", summary)
				}
				return
			}
			if len(summary.Files) != 1 || summary.Files[0].AbsolutePath != selected || summary.Files[0].Path != "report.txt" {
				t.Fatalf("exact edit lost identity or captured siblings: %+v", summary)
			}
			if action == "deleted" && summary.Files[0].Status != "deleted" {
				t.Fatalf("deletion lost: %+v", summary.Files)
			}
			write(t, root, "report.txt", "later execution\n")
			diff, err := ReadDiff(t.Context(), owned, summary.ID, summary.Files[0].RootID, "report.txt")
			data, _ := json.Marshal(diff)
			if err != nil || !strings.Contains(string(data), "original") || strings.Contains(string(data), "later execution") {
				t.Fatalf("immutable exact file diff changed: %+v %v", diff, err)
			}
		})
	}
}

func TestExactFileCaptureKeepsFrozenIdentityWhenReferenceIsReboundBeforeBaseline(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	selected := filepath.Join(root, "report.txt")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	write(t, filepath.Dir(outside), "secret.txt", "outside grant\n")
	if err := os.Symlink(outside, selected); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	capture, err := Begin(t.Context(), owned, "rebound-reference", nil, Options{FilePaths: []string{selected}})
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	summary := capture.Summary()
	if summary.State != "unavailable" || len(summary.Roots) != 1 || !summary.Roots[0].File || summary.Roots[0].Path != selected || len(summary.Files) != 0 {
		t.Fatalf("rebound reference changed the frozen authorization identity: %+v", summary)
	}
}

func TestSnapshotsCaptureShellNetChangesAndRecoverImmutableDiff(t *testing.T) {
	for _, repository := range []bool{false, true} {
		t.Run(fmt.Sprint(repository), func(t *testing.T) {
			root, owned := t.TempDir(), t.TempDir()
			write(t, root, "existing.txt", "original uncommitted\n")
			write(t, root, "deleted.txt", "remove me\n")
			write(t, root, "renamed.txt", "rename content\n")
			write(t, root, "reverted.txt", "unchanged\n")
			if repository {
				gitRun(t, root, "init", "--quiet")
				gitRun(t, root, "add", ".")
				gitRun(t, root, "commit", "--quiet", "-m", "baseline")
				write(t, root, "existing.txt", "preexisting dirty\n")
			}
			capture := begin(t, owned, "execution-shell", []string{root}, Options{})
			cmd := exec.Command("sh", "-c", "printf 'after shell\\n' > existing.txt; : > empty.txt; printf '\\000binary' > blob.bin; mv renamed.txt moved.txt; rm deleted.txt; exit 7")
			cmd.Dir = root
			if err := cmd.Run(); err == nil {
				t.Fatal("fixture must fail after partial writes")
			}
			write(t, root, "reverted.txt", "intermediate\n")
			write(t, root, "reverted.txt", "unchanged\n")
			summary := finish(t, capture)
			files := fileMap(summary)
			if summary.TotalFiles != 5 || !summary.Complete || len(summary.Roots) != 1 {
				t.Fatalf("incorrect net summary: %#v", summary)
			}
			if _, ok := files["reverted.txt"]; ok {
				t.Fatal("restored file was reported")
			}
			if files["existing.txt"].Status != "modified" || files["deleted.txt"].Status != "deleted" || files["empty.txt"].Status != "added" || files["moved.txt"].Status != "renamed" || files["moved.txt"].OldPath != "renamed.txt" {
				t.Fatalf("wrong statuses: %#v", files)
			}
			if files["blob.bin"].Content != "binary" || files["blob.bin"].Added != nil {
				t.Fatal("binary file got invented line statistics")
			}
			if files["existing.txt"].Added == nil || *files["existing.txt"].Added != 1 || *files["existing.txt"].Deleted != 1 {
				t.Fatalf("wrong net statistics: %#v", files["existing.txt"])
			}
			write(t, root, "existing.txt", "later unrelated edit\n")
			diff, err := ReadDiff(context.Background(), owned, summary.ID, files["existing.txt"].RootID, "existing.txt")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(diff.Added, files["existing.txt"].Added) || len(diff.Hunks) != 1 || diff.Hunks[0].Lines[len(diff.Hunks[0].Lines)-1].Text != "after shell" {
				t.Fatalf("diff did not use immutable summary source: %#v", diff)
			}
			empty, err := ReadDiff(context.Background(), owned, summary.ID, files["empty.txt"].RootID, "empty.txt")
			if err != nil {
				t.Fatal(err)
			}
			if empty.BeforeExists || !empty.AfterExists || empty.Added == nil || *empty.Added != 0 {
				t.Fatalf("empty and absent conflated: %#v", empty)
			}
			loaded, err := ReadSummary(owned, summary.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(summary, loaded) {
				t.Fatal("record changed after restart read")
			}
		})
	}
}

func directoryDigest(t *testing.T, root string) map[string]string {
	t.Helper()
	values := map[string]string{}
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		relative, _ := filepath.Rel(root, path)
		values[relative] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func TestUserGitHooksFiltersLFSAndSplitIndexRemainUntouched(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	gitRun(t, root, "init", "--quiet")
	write(t, root, "tracked.txt", "before\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "--quiet", "-m", "before")
	gitRun(t, root, "update-index", "--split-index")
	marker := filepath.Join(t.TempDir(), "must-not-run")
	write(t, root, ".gitattributes", "*.txt filter=evil\n*.bin filter=lfs\n")
	gitRun(t, root, "config", "filter.evil.clean", "sh -c 'touch \""+marker+"\"; cat'")
	gitRun(t, root, "config", "filter.evil.required", "true")
	gitRun(t, root, "config", "filter.lfs.clean", "sh -c 'touch \""+marker+"\"; cat'")
	gitRun(t, root, "config", "core.fsmonitor", "sh -c 'touch \""+marker+"\"'")
	write(t, root, ".git/hooks/pre-commit", "#!/bin/sh\ntouch \""+marker+"\"\n")
	if err := os.Chmod(filepath.Join(root, ".git/hooks/pre-commit"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, root, ".gitignore", "tracked.txt\nignored.txt\n")
	write(t, root, "ignored.txt", "private ignored\n")
	write(t, root, ".git/info/exclude", "excluded.txt\n")
	write(t, root, "excluded.txt", "excluded\n")
	// 原生 Git 若加载该 include 会立即失败；采集只解析原配置的对象格式。
	invalidInclude := filepath.Join(t.TempDir(), "untrusted-config")
	if err := os.WriteFile(invalidInclude, []byte("this is not valid Git configuration\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configFile, err := os.OpenFile(filepath.Join(root, ".git/config"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(configFile, "\n[include]\n path = %q\n", invalidInclude); err != nil {
		t.Fatal(err)
	}
	configFile.Close()
	before := directoryDigest(t, filepath.Join(root, ".git"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "foreign-index"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.fsmonitor")
	t.Setenv("GIT_CONFIG_VALUE_0", "sh -c 'touch \""+marker+"\"'")
	capture := begin(t, owned, "isolation", []string{root}, Options{})
	write(t, root, "tracked.txt", "after\n")
	write(t, root, "payload.bin", "raw LFS file\n")
	write(t, root, "ignored.txt", "changed ignored\n")
	write(t, root, "excluded.txt", "changed excluded\n")
	summary := finish(t, capture)
	files := fileMap(summary)
	if _, ok := files["tracked.txt"]; !ok {
		t.Fatal("tracked path excluded by ignore pattern")
	}
	if _, ok := files["ignored.txt"]; ok {
		t.Fatal("ignored path captured")
	}
	if _, ok := files["excluded.txt"]; ok {
		t.Fatal("repository exclude was ignored")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("project filter, hook or fsmonitor executed")
	}
	after := directoryDigest(t, filepath.Join(root, ".git"))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("user index/objects/refs/config changed")
	}
}

func TestScopeOverlapNestedRepositoriesAndSymlinks(t *testing.T) {
	parent, owned, outside := t.TempDir(), t.TempDir(), t.TempDir()
	gitRun(t, parent, "init", "--quiet")
	write(t, parent, ".gitignore", "scope/ignored.txt\n")
	root := filepath.Join(parent, "scope")
	write(t, root, "allowed.txt", "before\n")
	write(t, root, "ignored.txt", "ignored\n")
	write(t, parent, "outside.txt", "outside before\n")
	write(t, root, "runtime/accounts/token", "secret\n")
	write(t, root, "log/private.jsonl", "secret log\n")
	nested := filepath.Join(root, "nested")
	write(t, nested, "file.txt", "before nested\n")
	gitRun(t, nested, "init", "--quiet")
	write(t, root, "not-authorized/file.txt", "before forbidden nested\n")
	gitRun(t, filepath.Join(root, "not-authorized"), "init", "--quiet")
	write(t, outside, "secret.txt", "secret outside\n")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	capture := begin(t, owned, "scoped", []string{root, filepath.Join(root, "runtime"), nested, root}, Options{ExcludePaths: []string{filepath.Join(root, "log")}})
	if len(capture.Summary().Roots) != 2 {
		t.Fatalf("overlap roots not deduplicated: %#v", capture.Summary().Roots)
	}
	write(t, root, "allowed.txt", "after\n")
	write(t, nested, "file.txt", "after nested\n")
	write(t, parent, "outside.txt", "outside changed\n")
	write(t, root, "ignored.txt", "ignored changed\n")
	write(t, root, "runtime/accounts/token", "secret changed\n")
	write(t, root, "log/private.jsonl", "log changed\n")
	write(t, root, "not-authorized/file.txt", "forbidden changed\n")
	write(t, outside, "secret.txt", "outside symlink changed\n")
	summary := finish(t, capture)
	if summary.TotalFiles != 2 {
		t.Fatalf("scope escaped: %#v", summary)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range summary.Files {
		if !inside(root, file.AbsolutePath) {
			t.Fatalf("target escaped root: %#v", file)
		}
	}
	if _, err := ReadDiff(context.Background(), owned, summary.ID, summary.Roots[0].ID, "../outside.txt"); err == nil {
		t.Fatal("arbitrary path was accepted")
	}
}

func TestRecoveryCloseAndClonePreserveOnlyCommittedHistoricalContent(t *testing.T) {
	root, owned, target := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, root, "file.txt", "before\n")
	active := begin(t, owned, "interrupted", []string{root}, Options{})
	write(t, root, "file.txt", "after crash\n")
	// 模拟进程退出时操作系统释放文件租约，不提交当前目录内容。
	active.releaseLease()
	active.closed = true
	if err := Recover(owned); err != nil {
		t.Fatal(err)
	}
	interrupted, err := ReadSummary(owned, active.Summary().ID)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.State != "incomplete" || len(interrupted.Files) != 0 {
		t.Fatalf("restart fabricated historical end: %#v", interrupted)
	}
	// 模拟进程消失，避免旧句柄再覆盖恢复事实。
	active.closed = true
	capture := begin(t, owned, "complete", []string{root}, Options{})
	write(t, root, "file.txt", "forked immutable\n")
	summary := finish(t, capture)
	t.Setenv("PATH", t.TempDir())
	if err := CloneRecord(owned, target, summary.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSummary(target, interrupted.ID); err == nil {
		t.Fatal("fork copied an unrelated record")
	}
	if err := os.RemoveAll(owned); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	diff, err := ReadDiff(context.Background(), target, summary.ID, summary.Files[0].RootID, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Hunks) != 1 || diff.Hunks[0].Lines[1].Text != "forked immutable" {
		t.Fatalf("fork depended on deleted source: %#v", diff)
	}
	if err := Recover(target); err != nil {
		t.Fatal(err)
	}
}

func TestFrozenSnapshotComparisonKeepsOriginalFinalizationDeadline(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	write(t, root, "file.txt", "before\n")
	capture := begin(t, owned, "deadline", []string{root}, Options{})
	write(t, root, "file.txt", "after\n")
	if err := capture.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 将同一收尾截止时间置于过去，确定性模拟 Freeze 已经耗尽预算；Finish 不能重开预算。
	capture.finalizationDeadline = time.Now().Add(-time.Second)
	summary, err := capture.Finish(context.Background())
	if err == nil && summary.State == "ready" {
		t.Fatal("comparison reopened a full timeout after the snapshot budget expired")
	}
}

func TestBoundsUnavailableGitCancellationAndLargeFiles(t *testing.T) {
	t.Run("missing-git", func(t *testing.T) {
		capture, err := Begin(context.Background(), t.TempDir(), "missing", []string{t.TempDir()}, Options{GitExecutable: filepath.Join(t.TempDir(), "absent")})
		if err != nil {
			t.Fatal(err)
		}
		if capture.Summary().State != "unavailable" {
			t.Fatal("missing Git was not explicit")
		}
		summary, err := capture.Finish(context.Background())
		if err != nil || summary.State != "unavailable" {
			t.Fatalf("missing Git changed state: %#v %v", summary, err)
		}
	})
	t.Run("entry-budget", func(t *testing.T) {
		root, owned := t.TempDir(), t.TempDir()
		for i := 0; i < 10; i++ {
			write(t, root, fmt.Sprint(i), "before\n")
		}
		capture := begin(t, owned, "entries", []string{root}, Options{Limits: Limits{MaxEntries: 3}})
		for i := 0; i < 10; i++ {
			write(t, root, fmt.Sprint(i), "after\n")
		}
		summary := finish(t, capture)
		if summary.Complete || summary.State != "partial" {
			t.Fatal("bounded enumeration claimed complete")
		}
	})
	t.Run("display-and-text", func(t *testing.T) {
		root, owned := t.TempDir(), t.TempDir()
		write(t, root, "a.txt", "before\n")
		write(t, root, "large.txt", strings.Repeat("a", 40))
		capture := begin(t, owned, "display", []string{root}, Options{Limits: Limits{MaxFiles: 1, MaxTextBytes: 16, MaxDiffLines: 1}})
		write(t, root, "a.txt", "after\n")
		write(t, root, "large.txt", strings.Repeat("b", 40))
		summary := finish(t, capture)
		if !summary.Truncated || summary.TotalFiles != 2 || len(summary.Files) != 1 {
			t.Fatalf("lost complete total: %#v", summary)
		}
		diff, err := ReadDiff(context.Background(), owned, summary.ID, summary.Files[0].RootID, "a.txt")
		if err != nil {
			t.Fatal(err)
		}
		if !diff.Truncated || diff.Added == nil || *diff.Added != 1 {
			t.Fatalf("render budget corrupted counts: %#v", diff)
		}
	})
	t.Run("oversized", func(t *testing.T) {
		root, owned := t.TempDir(), t.TempDir()
		write(t, root, "large.txt", strings.Repeat("a", 40))
		capture := begin(t, owned, "large", []string{root}, Options{Limits: Limits{MaxTextBytes: 16}})
		write(t, root, "large.txt", strings.Repeat("b", 40))
		summary := finish(t, capture)
		file := summary.Files[0]
		if file.Content != "oversized" || file.Added != nil {
			t.Fatalf("oversized got line counts: %#v", file)
		}
		diff, err := ReadDiff(context.Background(), owned, summary.ID, file.RootID, file.Path)
		if err != nil || diff.State != "oversized" || len(diff.Hunks) != 0 {
			t.Fatalf("oversized content was read: %#v %v", diff, err)
		}
	})
	t.Run("chat-budget", func(t *testing.T) {
		root, owned := t.TempDir(), t.TempDir()
		write(t, root, "file.txt", "before too large\n")
		capture := begin(t, owned, "chat-budget", []string{root}, Options{Limits: Limits{MaxChatBytes: 2}})
		write(t, root, "file.txt", "after too large\n")
		summary := finish(t, capture)
		if summary.State != "partial" || summary.Complete || summary.TotalFiles != 0 || len(summary.Files) != 0 {
			t.Fatalf("space exhaustion hidden: %#v", summary)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		root, owned := t.TempDir(), t.TempDir()
		write(t, root, "file.txt", "before\n")
		capture := begin(t, owned, "cancel", []string{root}, Options{})
		write(t, root, "file.txt", "partial write\n")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		summary, err := capture.Finish(ctx)
		if err == nil && summary.State != "unavailable" {
			t.Fatalf("cancel fabricated success: %#v", summary)
		}
		if err := capture.Close(); err != nil {
			t.Fatal(err)
		}
		stored, err := ReadSummary(owned, capture.Summary().ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.State == "ready" || stored.State == "pending" {
			t.Fatalf("cancel was not durable: %#v", stored)
		}
	})
}

func TestConcurrentCapturesKeepIndependentWindows(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	write(t, root, "file.txt", "initial\n")
	first := begin(t, owned, "first", []string{root}, Options{})
	write(t, root, "file.txt", "between\n")
	second := begin(t, owned, "second", []string{root}, Options{})
	write(t, root, "file.txt", "final\n")
	var wg sync.WaitGroup
	results := make([]*Summary, 2)
	errs := make([]error, 2)
	for i, capture := range []*Capture{first, second} {
		wg.Add(1)
		go func(i int, capture *Capture) {
			defer wg.Done()
			results[i], errs[i] = capture.Finish(context.Background())
		}(i, capture)
	}
	wg.Wait()
	for i, summary := range results {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		diff, err := ReadDiff(context.Background(), owned, summary.ID, summary.Files[0].RootID, "file.txt")
		if err != nil {
			t.Fatal(err)
		}
		want := "initial"
		if i == 1 {
			want = "between"
		}
		if diff.Hunks[0].Lines[0].Text != want {
			t.Fatalf("capture windows were merged: %#v", diff)
		}
	}
}

func TestBoundedDiffPlanCountsAndContext(t *testing.T) {
	limits := normalizedLimits(Limits{})
	plan := makeEdits(context.Background(), "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\n", "a\nB\nc\nd\ne\nf\ng\nh\ni\nJ\nk\n", time.Second)
	hunks, truncated := makeHunks(plan.edits, limits)
	if truncated || len(hunks) != 2 || hunks[0].OldStart != 1 || hunks[1].OldStart != 7 {
		t.Fatalf("incorrect context coordinates: %#v", hunks)
	}
	coarse := makeEdits(context.Background(), strings.Repeat("a\n", 2000), strings.Repeat("b\n", 2000), time.Second)
	if !coarse.coarse {
		t.Fatal("quadratic diff was not bounded")
	}
}

func TestStorageBudgetPreservesConfirmedFingerprintChanges(t *testing.T) {
	for _, modify := range []bool{false, true} {
		t.Run(fmt.Sprint(modify), func(t *testing.T) {
			root, owned := t.TempDir(), t.TempDir()
			write(t, root, "large.txt", "before contents\n")
			capture := begin(t, owned, "fingerprint-change", []string{root}, Options{Limits: Limits{MaxTextBytes: 1, MaxChatBytes: 2}})
			want := 0
			if modify {
				write(t, root, "large.txt", "after contents!\n")
				want = 1
			}
			summary := finish(t, capture)
			if summary.State != "partial" || summary.Complete || summary.TotalFiles != want || len(summary.Files) != want {
				t.Fatalf("fingerprint evidence lost under storage budget: %#v", summary)
			}
			if modify {
				file := summary.Files[0]
				if file.Path != "large.txt" || file.Status != "modified" || file.Content != "unreadable" || file.Added != nil || file.Deleted != nil {
					t.Fatalf("confirmed change fabricated readable contents: %#v", file)
				}
			}
		})
	}
}

func TestCaptureBudgetDoesNotTurnUnchangedFilesIntoEdits(t *testing.T) {
	for _, modify := range []bool{false, true} {
		t.Run(fmt.Sprint(modify), func(t *testing.T) {
			root, owned := t.TempDir(), t.TempDir()
			write(t, root, "a.txt", "before\n")
			write(t, root, "installer.dmg", strings.Repeat("x", 64))
			capture := begin(t, owned, "budget", []string{root}, Options{Limits: Limits{MaxCaptureBytes: 16}})
			want := 0
			if modify {
				write(t, root, "a.txt", "after\n")
				want = 1
			}
			summary := finish(t, capture)
			if summary.Complete || summary.State != "partial" || summary.TotalFiles != want || len(summary.Files) != want {
				t.Fatalf("unknown content counted as edits: %#v", summary)
			}
			if want == 1 && summary.Files[0].Path != "a.txt" {
				t.Fatalf("confirmed edit lost: %#v", summary.Files)
			}
		})
	}
}

func TestUnreadableAndReplacedRootsRemainPartial(t *testing.T) {
	t.Run("unreadable", func(t *testing.T) {
		root, owned := t.TempDir(), t.TempDir()
		write(t, root, "file.txt", "secret\n")
		path := filepath.Join(root, "file.txt")
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(path, 0600)
		capture := begin(t, owned, "unreadable", []string{root}, Options{})
		summary := finish(t, capture)
		if summary.Complete || summary.State != "partial" || summary.TotalFiles != 0 || len(summary.Files) != 0 {
			t.Fatalf("unreadable coverage fabricated: %#v", summary)
		}
	})
	t.Run("replaced-root", func(t *testing.T) {
		container, owned := t.TempDir(), t.TempDir()
		root := filepath.Join(container, "project")
		write(t, root, "old.txt", "before\n")
		capture := begin(t, owned, "replaced", []string{root}, Options{})
		if err := os.Rename(root, root+"-moved"); err != nil {
			t.Fatal(err)
		}
		write(t, root, "new.txt", "different directory\n")
		summary := finish(t, capture)
		if summary.Complete || summary.State != "partial" {
			t.Fatalf("root replacement silently rebound: %#v", summary)
		}
		if summary.TotalFiles != 0 || len(summary.Files) != 0 {
			t.Fatalf("replacement directory fabricated changes: %#v", summary)
		}
	})
}

func TestUninitializedSubmoduleRequiresIndependentAuthorization(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	gitRun(t, root, "init", "--quiet")
	write(t, root, "a.txt", "before\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "--quiet", "-m", "initial")
	commit := strings.TrimSpace(gitRun(t, root, "rev-parse", "HEAD"))
	gitRun(t, root, "update-index", "--add", "--cacheinfo", "160000", commit, "submodule")
	write(t, root, ".gitmodules", "[submodule \"local\"]\n path = submodule\n url = https://example.invalid/never-fetch\n")
	submodule := filepath.Join(root, "submodule")
	write(t, submodule, "file.txt", "before submodule\n")
	unauthorized := begin(t, owned, "without-submodule", []string{root}, Options{})
	authorized := begin(t, owned, "with-submodule", []string{root, submodule}, Options{})
	if len(authorized.Summary().Roots) != 2 {
		t.Fatal("explicit submodule root was deduplicated away")
	}
	write(t, submodule, "file.txt", "after submodule\n")
	without, with := finish(t, unauthorized), finish(t, authorized)
	if without.TotalFiles != 0 || with.TotalFiles != 1 || with.Files[0].Path != "file.txt" {
		t.Fatalf("submodule authorization escaped: %#v %#v", without, with)
	}
}

func TestFullSizeLimitControlPathsAndStoredHunkBudget(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	path := "中文 with [brackets]\tand\nnewline.txt"
	write(t, root, path, "before\n")
	write(t, root, "large.txt", strings.Repeat("a", (2<<20)+1))
	capture := begin(t, owned, "real-size", []string{root}, Options{})
	write(t, root, path, "after\n")
	write(t, root, "large.txt", strings.Repeat("b", (2<<20)+1))
	summary := finish(t, capture)
	files := fileMap(summary)
	if files["large.txt"].Content != "oversized" || files["large.txt"].Added != nil {
		t.Fatal("default text limit was not enforced")
	}
	diff, err := ReadDiff(context.Background(), owned, summary.ID, files[path].RootID, path)
	if err != nil || diff.Path != path || diff.Hunks[0].Lines[1].Text != "after" {
		t.Fatalf("control filename identity changed: %#v %v", diff, err)
	}
	if err := Recover(owned); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDiff(context.Background(), owned, summary.ID, files[path].RootID, path); err != nil {
		t.Fatal("recovery removed referenced content:", err)
	}
}

func TestCommandTimeoutAndOutputLimitNeverFallback(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("shell fixture requires Unix")
	}
	for _, kind := range []string{"timeout", "output"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			script := filepath.Join(directory, "git")
			body := "#!/bin/sh\nexec sleep 10\n"
			limits := Limits{Timeout: 100 * time.Millisecond}
			if kind == "output" {
				body = "#!/bin/sh\nprintf '" + strings.Repeat("x", 1024) + "'\n"
				// 预算须容纳冻结根摘要，才能实际覆盖 Git 子进程输出超限。
				limits = Limits{MaxOutputBytes: 512}
			}
			if err := os.WriteFile(script, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			capture, err := Begin(context.Background(), t.TempDir(), kind, []string{t.TempDir()}, Options{GitExecutable: script, Limits: limits})
			if err != nil {
				t.Fatal(err)
			}
			defer capture.Close()
			if capture.Summary().State != "unavailable" {
				t.Fatal("failed Git command was not explicit")
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("Git command exceeded bounded cancellation")
			}
		})
	}
}

func TestMissingAbsoluteScopeHasDurableUnavailableResult(t *testing.T) {
	owned := t.TempDir()
	missing := filepath.Join(t.TempDir(), "removed-project")
	capture, err := Begin(context.Background(), owned, "missing-scope", []string{missing}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	summary := capture.Summary()
	if summary.State != "unavailable" || summary.Reason != "scope_unavailable" || len(summary.Files) != 0 {
		t.Fatalf("missing scope silently lost: %#v", summary)
	}
	loaded, err := ReadSummary(owned, summary.ID)
	if err != nil || loaded.State != "unavailable" {
		t.Fatalf("unavailable marker not durable: %#v %v", loaded, err)
	}
}

func TestRecoveryReclaimsUnreferencedPackedObjectsAndStaging(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	write(t, root, "file.txt", "initial\n")
	keep := begin(t, owned, "kept", []string{root}, Options{})
	write(t, root, "file.txt", "kept\n")
	summary := finish(t, keep)
	write(t, root, "file.txt", "orphan value\n")
	orphan := begin(t, owned, "orphan", []string{root}, Options{})
	orphanID := orphan.Summary().ID
	orphan.closed = true
	orphan.releaseLease()
	if err := os.RemoveAll(filepath.Dir(recordPath(owned, orphanID))); err != nil {
		t.Fatal(err)
	}
	hash := plumbing.ComputeHash(plumbing.BlobObject, []byte("orphan value\n"))
	if err := objectStore(owned).HasEncodedObject(hash); err != nil {
		t.Fatal("orphan fixture missing:", err)
	}
	staging := filepath.Join(filepath.Dir(recordPath(owned, summary.ID)), "pending-interrupted")
	if err := os.WriteFile(staging, []byte("draft"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Recover(owned); err != nil {
		t.Fatal(err)
	}
	if objectStore(owned).HasEncodedObject(hash) == nil {
		t.Fatal("unreferenced packed content was retained")
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("owned staging was not cleaned")
	}
	if _, err := ReadDiff(context.Background(), owned, summary.ID, summary.Files[0].RootID, summary.Files[0].Path); err != nil {
		t.Fatal("recovery removed committed diff:", err)
	}
}

func TestWalkSummariesDoesNotReadCurrentFilesOrObjects(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	write(t, root, "file.txt", "initial\n")
	capture := begin(t, owned, "walk", []string{root}, Options{})
	write(t, root, "file.txt", "after\n")
	summary := finish(t, capture)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repositoryPath(owned)); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := WalkSummaries(owned, func(value Summary) error {
		count++
		if value.ID != summary.ID || value.TotalFiles != 1 {
			t.Fatalf("wrong summary: %#v", value)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("visited %d summaries", count)
	}
}

func TestRecoveryDoesNotTakeOverActiveCaptureBetweenPhases(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	write(t, root, "file.txt", "before\n")
	capture := begin(t, owned, "active-recovery", []string{root}, Options{})
	write(t, root, "file.txt", "after\n")
	if err := Recover(owned); err != nil {
		t.Fatal(err)
	}
	pending, err := ReadSummary(owned, capture.Summary().ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != "pending" || pending.Version != 1 {
		t.Fatalf("active capture was recovered as crashed: %#v", pending)
	}
	summary := finish(t, capture)
	if summary.State != "ready" || summary.Version != 2 || summary.TotalFiles != 1 {
		t.Fatalf("active capture lost its result: %#v", summary)
	}
}

func TestSHA256SourceIndexUsesPrivateMetadataReader(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	gitRun(t, root, "init", "--quiet", "--object-format=sha256")
	write(t, root, "file.txt", "before\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "--quiet", "-m", "sha256 fixture")
	before := directoryDigest(t, filepath.Join(root, ".git"))
	capture := begin(t, owned, "sha256-source", []string{root}, Options{})
	write(t, root, "file.txt", "after\n")
	summary := finish(t, capture)
	if summary.State != "ready" || summary.TotalFiles != 1 {
		t.Fatalf("source format changed capture coverage: %#v", summary)
	}
	if !reflect.DeepEqual(before, directoryDigest(t, filepath.Join(root, ".git"))) {
		t.Fatal("source SHA256 metadata changed")
	}
}

func TestEscapedPathBudgetMarksPartialCoverage(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	for i := 0; i < 20; i++ {
		write(t, root, strings.Repeat("\x01", 40)+fmt.Sprint(i)+".txt", "before\n")
	}
	capture := begin(t, owned, "escaped-budget", []string{root}, Options{Limits: Limits{MaxOutputBytes: 2048}})
	for i := 0; i < 20; i++ {
		write(t, root, strings.Repeat("\x01", 40)+fmt.Sprint(i)+".txt", "after\n")
	}
	summary := finish(t, capture)
	if summary.Complete || summary.State != "partial" {
		t.Fatalf("JSON escaping bypassed manifest budget: %#v", summary)
	}
	if _, err := ReadSummary(owned, summary.ID); err != nil {
		t.Fatal("partial record cannot be recovered:", err)
	}
}

func TestOversizedMetadataWritePreservesPreviousAtomicRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.json")
	if err := writeJSON(path, map[string]string{"state": "previous"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(path, map[string]string{"oversized": strings.Repeat("x", (64<<20)+1)}); err == nil {
		t.Fatal("write accepted metadata beyond read limit")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("over-budget write replaced the last readable record")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("failed metadata write left owned staging")
	}
}

func TestDiscardUnpublishedRecordPreservesOtherActiveCapture(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	write(t, root, "file.txt", "shared baseline\n")
	first := begin(t, owned, "discarded", []string{root}, Options{})
	second := begin(t, owned, "kept-active", []string{root}, Options{})
	firstID, secondID := first.Summary().ID, second.Summary().ID
	if err := first.Discard(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSummary(owned, firstID); err == nil {
		t.Fatal("discard retained an unpublished record")
	}
	active, err := ReadSummary(owned, secondID)
	if err != nil || active.State != "pending" {
		t.Fatalf("discard changed another active record: %#v %v", active, err)
	}
	write(t, root, "file.txt", "after active capture\n")
	summary := finish(t, second)
	diff, err := ReadDiff(context.Background(), owned, summary.ID, summary.Files[0].RootID, "file.txt")
	if err != nil || diff.Hunks[0].Lines[0].Text != "shared baseline" {
		t.Fatalf("discard removed another capture's baseline: %#v %v", diff, err)
	}
	if err := first.Discard(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSummary(owned, summary.ID); err != nil {
		t.Fatal("discard retry removed committed record:", err)
	}
	if err := second.Discard(context.Background()); err == nil {
		t.Fatal("discard accepted a completed record")
	}
}

func TestDiscardRetriesDoNotAccumulateRecordsOrObjects(t *testing.T) {
	root, owned := t.TempDir(), t.TempDir()
	for i := 0; i < 3; i++ {
		write(t, root, "file.txt", fmt.Sprintf("unique rejected attempt %d\n", i))
		capture := begin(t, owned, fmt.Sprintf("attempt-%d", i), []string{root}, Options{})
		if err := capture.Discard(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := capture.Discard(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(base(owned), "records"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected attempts retained %d records", len(entries))
	}
	iterator, err := objectStore(owned).IterEncodedObjects(plumbing.AnyObject)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	err = iterator.ForEach(func(plumbing.EncodedObject) error { count++; return nil })
	iterator.Close()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected attempts retained %d objects", count)
	}
}
