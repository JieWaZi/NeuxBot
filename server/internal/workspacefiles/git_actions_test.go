package workspacefiles

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestGitActionGenerateMessageFromStagedChanges(t *testing.T) {
	m, scope, repo := gitFixture(t)
	scope = gitActionChatScope(t, m, scope, repo)
	ctx := context.Background()
	var called bool
	writeGitNote(t, repo, "staged text\n")
	gitRun(t, repo, "add", "note.txt")
	writeGitNote(t, repo, "unstaged text\n")
	accountPath := filepath.Join(repo, "Runtime", "Accounts", "secrets.txt")
	if err := os.MkdirAll(filepath.Dir(accountPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(accountPath, []byte("protected content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "Runtime/Accounts/secrets.txt")
	result, err := m.RunGitAction(ctx, scope.subject, gitActionRequest(t, m, scope, repo, constants.GitActionCommit), func(_ context.Context, chatID string, patch string) (string, bool, error) {
		called = true
		if chatID != "chat" {
			t.Fatalf("generation was not tied to the authorized Chat: %q", chatID)
		}
		if !strings.Contains(patch, "+staged text") || strings.Contains(patch, "+unstaged text") ||
			strings.Contains(patch, "accounts") || strings.Contains(patch, "protected content") {
			t.Fatalf("wrong patch for message generation: %q", patch)
		}
		return "Update note", false, nil
	}, nil)
	if err != nil || !called || result.Commit == "" {
		t.Fatalf("commit was not generated: %+v %v", result, err)
	}
	if got := gitRun(t, repo, "log", "-1", "--format=%s"); got != "Update note" {
		t.Fatal(got)
	}
	if got := gitRun(t, repo, "show", "HEAD:note.txt"); got != "staged text" {
		t.Fatal(got)
	}
}

func TestGitActionRejectsStagedChangesDuringGeneration(t *testing.T) {
	m, scope, repo := gitFixture(t)
	scope = gitActionChatScope(t, m, scope, repo)
	writeGitNote(t, repo, "first staged\n")
	gitRun(t, repo, "add", "note.txt")
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
	result, err := m.RunGitAction(context.Background(), scope.subject, request, func(_ context.Context, _ string, _ string) (string, bool, error) {
		writeGitNote(t, repo, "next staged\n")
		gitRun(t, repo, "add", "note.txt")
		return "Update note", false, nil
	}, nil)
	if err != status_error.WorkspaceResourceConflict || result.Commit != "" {
		t.Fatalf("changed index was committed: %+v %v", result, err)
	}
	if got := gitRun(t, repo, "rev-parse", "HEAD"); got != *request.ExpectedHead {
		t.Fatal(got)
	}
}

func TestGitActionUsesStagedMetadataWhenRuntimeCannotDisableTools(t *testing.T) {
	m, scope, repo := gitFixture(t)
	scope = gitActionChatScope(t, m, scope, repo)
	writeGitNote(t, repo, "staged change\n")
	gitRun(t, repo, "add", "note.txt")
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := m.RunGitAction(context.Background(), scope.subject, gitActionRequest(t, m, scope, repo, constants.GitActionCommit), func(context.Context, string, string) (string, bool, error) {
		return "", true, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := gitRun(t, repo, "log", "-1", "--format=%s"); got != "Update note.txt" {
		t.Fatal(got)
	}
}

func TestGitActionDraftBlankMessageUsesStagedFiles(t *testing.T) {
	m, scope, repo := gitFixture(t)
	writeGitNote(t, repo, "staged change\n")
	gitRun(t, repo, "add", "note.txt")
	_, err := m.RunGitAction(context.Background(), scope.subject, gitActionRequest(t, m, scope, repo, constants.GitActionCommit), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := gitRun(t, repo, "log", "-1", "--format=%s"); got != "Update note.txt" {
		t.Fatal(got)
	}
}

func TestGitActionNonFastForwardReportsCommittedStep(t *testing.T) {
	m, scope, repo := gitFixture(t)
	ctx := context.Background()
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare", "-b", "main")
	gitRun(t, repo, "remote", "add", "origin", remote)
	gitRun(t, repo, "push", "-u", "origin", "main")
	peer := t.TempDir()
	gitRun(t, peer, "clone", remote, ".")
	gitRun(t, peer, "config", "user.email", "peer@example.invalid")
	gitRun(t, peer, "config", "user.name", "Peer")
	writeGitNote(t, peer, "remote update\n")
	gitRun(t, peer, "commit", "-am", "Remote update")
	gitRun(t, peer, "push")
	remoteHead := gitRun(t, remote, "rev-parse", "main")
	writeGitNote(t, repo, "local update\n")
	gitRun(t, repo, "add", "note.txt")
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommitPush)
	request.Message = "Local update"
	request.Remote = "origin"
	result, err := m.RunGitAction(ctx, scope.subject, request, func(context.Context, string, string) (string, bool, error) {
		t.Fatal("manual commit called generation")
		return "", false, nil
	}, nil)
	if err != status_error.WorkspaceGitFailed || result.Commit == "" || result.Pushed || result.FailedStep != "push" {
		t.Fatalf("staged commit outcome was lost: %+v %v", result, err)
	}
	if got := gitRun(t, remote, "rev-parse", "main"); got != remoteHead {
		t.Fatal(got)
	}
}

func TestGitActionRejectsUnconfirmedPushDestination(t *testing.T) {
	m, scope, repo := gitFixture(t)
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare", "-b", "main")
	gitRun(t, repo, "remote", "add", "origin", remote)
	gitRun(t, repo, "push", "-u", "origin", "main:release")
	remoteHead := gitRun(t, remote, "rev-parse", "release")
	gitRun(t, repo, "commit", "--allow-empty", "-m", "local")
	request := gitActionRequest(t, m, scope, repo, constants.GitActionPush)
	expected := "main"
	request.Remote, request.ExpectedRemoteBranch = "origin", &expected
	result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
	if err != status_error.WorkspaceResourceConflict || result.Pushed {
		t.Fatalf("push used an unconfirmed remote branch: %+v %v", result, err)
	}
	if head := gitRun(t, remote, "rev-parse", "release"); head != remoteHead {
		t.Fatal(head)
	}
}

func TestGitActionPushUsesConfirmedCommitAfterBranchMoves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell Git shim")
	}
	m, scope, repo := gitFixture(t)
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare", "-b", "main")
	gitRun(t, repo, "remote", "add", "origin", remote)
	gitRun(t, repo, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/company/*")
	confirmed := gitRun(t, repo, "rev-parse", "HEAD")
	gitRun(t, repo, "commit", "--allow-empty", "-m", "later commit")
	later := gitRun(t, repo, "rev-parse", "HEAD")
	gitRun(t, repo, "reset", "--hard", confirmed)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shimDir := t.TempDir()
	const shim = `#!/bin/sh
if [ "$4" = push ]; then
    "$NEUXBOT_TEST_REAL_GIT" -C "$2" update-ref refs/heads/main "$NEUXBOT_TEST_LATER_HEAD" || exit "$?"
fi
exec "$NEUXBOT_TEST_REAL_GIT" "$@"
`
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(shim), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEUXBOT_TEST_REAL_GIT", git)
	t.Setenv("NEUXBOT_TEST_LATER_HEAD", later)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	request := gitActionRequest(t, m, scope, repo, constants.GitActionPush)
	request.Remote = "origin"
	result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
	if err != nil || !result.Pushed {
		t.Fatalf("first push: %+v %v", result, err)
	}
	if head := gitRun(t, remote, "rev-parse", "refs/heads/main"); head != confirmed {
		t.Fatalf("pushed %s, want confirmed %s", head, confirmed)
	}
	if upstream := gitRun(t, repo, "rev-parse", "--abbrev-ref", "@{upstream}"); upstream != "company/main" {
		t.Fatalf("upstream = %q", upstream)
	}
}

func TestGitActionChecksOutBranchSharingTagName(t *testing.T) {
	m, scope, repo := gitFixture(t)
	gitRun(t, repo, "branch", "topic")
	gitRun(t, repo, "tag", "topic")
	branches, err := m.Branches(t.Context(), scope.subject, scope.ID, "", repo, "topic", nil)
	if err != nil || len(branches.Branches) != 1 {
		t.Fatalf("branches = %+v %v", branches.Branches, err)
	}
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCheckout)
	request.Branch = branches.Branches[0].Name
	result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
	if err != nil || result.Branch != "topic" || gitRun(t, repo, "branch", "--show-current") != "topic" {
		t.Fatalf("checkout %q = %+v %v", request.Branch, result, err)
	}
}

func TestGitActionCreatesAndChecksOutLocalBranch(t *testing.T) {
	m, scope, repo := gitFixture(t)
	ctx := context.Background()
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCreateBranch)
	request.Branch = "work/topic"
	result, err := m.RunGitAction(ctx, scope.subject, request, nil, nil)
	if err != nil || result.Branch != request.Branch || gitRun(t, repo, "branch", "--show-current") != request.Branch {
		t.Fatalf("branch was not created and checked out: %+v %v", result, err)
	}
	if _, err = m.RunGitAction(ctx, scope.subject, request, nil, nil); err != status_error.WorkspaceResourceConflict {
		t.Fatalf("old checkout was accepted: %v", err)
	}
}

func TestGitActionRejectsUnsupportedPRTargetBeforeWriting(t *testing.T) {
	m, scope, repo := gitFixture(t)
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare", "-b", "main")
	gitRun(t, repo, "remote", "add", "origin", remote)
	gitRun(t, repo, "switch", "-c", "topic")
	writeGitNote(t, repo, "staged change\n")
	gitRun(t, repo, "add", "note.txt")
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommitPushPR)
	request.Message, request.Remote, request.Base = "Update note", "origin", "main"
	result, err := m.RunGitAction(context.Background(), scope.subject, request, nil, nil)
	if err != status_error.WorkspaceGitHubUnsupported || result.Pushed || result.Commit != "" {
		t.Fatalf("unsupported PR target wrote to the repository: %+v %v", result, err)
	}
	if head := gitRun(t, repo, "rev-parse", "HEAD"); head != *request.ExpectedHead {
		t.Fatal(head)
	}
}

func TestGitActionReportsPushCompletedWhenPRRequestFails(t *testing.T) {
	m, scope, repo := gitFixture(t)
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare", "-b", "main")
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NEUX_TEST_GH_BARE", remote)
	if err := os.WriteFile(filepath.Join(dir, "test-ssh"), []byte("#!/bin/sh\nexec git-receive-pack \"$NEUX_TEST_GH_BARE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "config", "core.sshCommand", filepath.Join(dir, "test-ssh"))
	gitRun(t, repo, "config", "ssh.variant", "simple")
	gitRun(t, repo, "remote", "add", "origin", "git@github.com:owner/project.git")
	gitRun(t, repo, "switch", "-c", "topic")
	writeGitNote(t, repo, "staged change\n")
	gitRun(t, repo, "add", "note.txt")
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommitPushPR)
	request.Message, request.Remote, request.Base = "Update note", "origin", "main"
	result, err := m.RunGitAction(context.Background(), scope.subject, request, nil, nil)
	if err != status_error.WorkspaceGitHubRequestFailed || !result.Pushed || result.Commit == "" || result.FailedStep != "pr" {
		t.Fatalf("completed steps were lost: %+v %v", result, err)
	}
	if head := gitRun(t, remote, "rev-parse", "topic"); head != result.Commit {
		t.Fatal(head)
	}
}

func TestGitActionCreatesOrReusesExactGitHubPR(t *testing.T) {
	m, scope, repo := gitFixture(t)
	gitRun(t, repo, "remote", "add", "origin", "git@github.com:owner/project.git")
	gitRun(t, repo, "switch", "-c", "topic/note")
	gitRun(t, repo, "commit", "--allow-empty", "-m", "Update note")
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NEUX_TEST_GH_HEAD", gitRun(t, repo, "rev-parse", "HEAD"))
	t.Setenv("NEUX_TEST_GH_MARKER", filepath.Join(dir, "created"))
	script := `#!/bin/sh
set -eu
test "$1 $2 $3 $4" = "api --hostname github.com --method"
case "$5 $6" in
  'GET repos/owner/project')
    printf '{"default_branch":"main"}'
    ;;
  'GET repos/owner/project/pulls')
    test "$7 $8 $9 ${10} ${11} ${12}" = "--raw-field state=open --raw-field head=owner:topic/note --field per_page=2"
    if [ "$#" -gt 12 ]; then
      test "${13} ${14}" = "--raw-field base=main"
    fi
    if [ "${NEUX_TEST_GH_MULTIPLE:-0}" = "1" ]; then
      printf '[{"html_url":"https://github.com/owner/project/pull/123"},{"html_url":"https://github.com/owner/project/pull/124"}]'
    elif [ -f "$NEUX_TEST_GH_MARKER" ]; then
      printf '[{"html_url":"https://github.com/owner/project/pull/123"}]'
    else
      printf '[]'
    fi
    ;;
  'GET repos/owner/project/git/ref/heads/topic/note')
    printf '{"object":{"sha":"%s"}}' "$NEUX_TEST_GH_HEAD"
    ;;
  'POST repos/owner/project/pulls')
    test "$7 $8 $9 ${10} ${11} ${12}" = "--raw-field head=owner:topic/note --raw-field base=main --raw-field title=Update note"
    test ! -f "$NEUX_TEST_GH_MARKER"
    touch "$NEUX_TEST_GH_MARKER"
    printf '{"html_url":"https://github.com/owner/project/pull/123"}'
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	status, err := m.Status(ctx, scope.subject, scope.ID, "", repo)
	if err != nil || !status.GitHub || len(status.Remotes) != 1 || status.Remotes[0] != "origin" {
		t.Fatalf("expected an eligible GitHub remote: %+v %v", status, err)
	}
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCreatePR)
	request.Remote = "origin"
	result, err := m.RunGitAction(ctx, scope.subject, request, nil, nil)
	if err != nil || result.PRURL != "https://github.com/owner/project/pull/123" || result.Base != "main" || result.Pushed {
		t.Fatalf("PR creation did not use confirmed refs: %+v %v", result, err)
	}
	gitRun(t, repo, "commit", "--allow-empty", "-m", "Local work")
	request = gitActionRequest(t, m, scope, repo, constants.GitActionFindPR)
	request.Remote = "origin"
	existing, err := m.RunGitAction(ctx, scope.subject, request, nil, nil)
	if err != nil || existing.PRURL != result.PRURL {
		t.Fatalf("existing PR lookup should work with local ahead commits: %+v %v", existing, err)
	}
	t.Setenv("NEUX_TEST_GH_MULTIPLE", "1")
	ambiguous, err := m.RunGitAction(ctx, scope.subject, request, nil, nil)
	if err != status_error.WorkspaceResourceConflict || ambiguous.PRURL != "" {
		t.Fatalf("ambiguous PR lookup should ask for a base: %+v %v", ambiguous, err)
	}
}

func TestGitStatusReportsUpstreamDivergence(t *testing.T) {
	m, scope, repo := gitFixture(t)
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare", "-b", "main")
	gitRun(t, repo, "remote", "add", "upstream", remote)
	gitRun(t, repo, "push", "-u", "upstream", "main")
	gitRun(t, repo, "remote", "set-head", "upstream", "main")
	gitRun(t, repo, "commit", "--allow-empty", "-m", "local")
	status, err := m.Status(context.Background(), scope.subject, scope.ID, "", repo)
	if err != nil || status.Head != gitRun(t, repo, "rev-parse", "HEAD") ||
		status.Upstream != "upstream/main" || status.Remote != "upstream" || status.DefaultBranch != "main" || status.Ahead != 1 || status.Behind != 0 {
		t.Fatalf("unexpected ref status: %+v %v", status, err)
	}
}

func TestGitCheckoutRejectsLegacyImplicitCommit(t *testing.T) {
	m, scope, repo := gitFixture(t)
	gitRun(t, repo, "branch", "topic")
	writeGitNote(t, repo, "unstaged\n")
	if _, err := m.Checkout(context.Background(), scope.subject, scope.ID, "", repo, "topic", "legacy commit"); err != status_error.WorkspaceResourceConflict {
		t.Fatalf("expected explicit commit workflow: %v", err)
	}
	if branch := gitRun(t, repo, "branch", "--show-current"); branch != "main" {
		t.Fatal(branch)
	}
	if staged := gitRun(t, repo, "diff", "--name-only", "--cached"); staged != "" {
		t.Fatal(staged)
	}
}

func TestGitUnstageRestoresBothPathsOfStagedRename(t *testing.T) {
	m, scope, repo := gitFixture(t)
	gitRun(t, repo, "mv", "note.txt", "renamed.txt")
	if err := m.ChangeIndex(t.Context(), scope.subject, GitIndexRequest{ScopeID: scope.ID, Repository: repo, RelativePath: "renamed.txt"}, "unstage"); err != nil {
		t.Fatal(err)
	}
	if names := gitRun(t, repo, "diff", "--cached", "--name-only"); names != "" {
		t.Fatalf("rename remained staged: %q", names)
	}
	if _, err := os.Stat(filepath.Join(repo, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestGitDiscardRejectsStaleCheckout(t *testing.T) {
	m, scope, repo := gitFixture(t)
	writeGitNote(t, repo, "first worktree change\n")
	preview, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, ":unstaged", "note.txt", false, nil)
	if err != nil || preview.WorktreeVersion == "" {
		t.Fatalf("discard preview = %+v %v", preview, err)
	}
	gitRun(t, repo, "switch", "-c", "topic")
	err = m.ChangeIndex(t.Context(), scope.subject, GitIndexRequest{
		ScopeID: scope.ID, Repository: repo, RelativePath: "note.txt",
		ExpectedWorktreeVersion: preview.WorktreeVersion,
	}, "discard")
	if err != status_error.WorkspaceResourceConflict {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(repo, "note.txt"))
	if err != nil || string(content) != "first worktree change\n" {
		t.Fatalf("discard affected a later checkout: %q %v", content, err)
	}
}

func TestGitDiscardProtectsEditsMadeAfterComparison(t *testing.T) {
	for _, tc := range []struct {
		name             string
		observed         string
		later            string
		ignoreWhitespace bool
	}{
		{name: "text", observed: "first\n", later: "later\n"},
		{name: "hidden whitespace", observed: "first\n", later: " first\n", ignoreWhitespace: true},
		{name: "binary", observed: "\x00first", later: "\x00later"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, scope, repo := gitFixture(t)
			writeGitNote(t, repo, tc.observed)
			preview, err := m.Comparison(t.Context(), scope.subject, scope.ID, "", repo, ":unstaged", "note.txt", tc.ignoreWhitespace, nil)
			if err != nil || preview.WorktreeVersion == "" {
				t.Fatalf("discard preview = %+v %v", preview, err)
			}
			request := GitIndexRequest{ScopeID: scope.ID, Repository: repo, RelativePath: "note.txt", IgnoreWhitespace: tc.ignoreWhitespace}
			if err := m.ChangeIndex(t.Context(), scope.subject, request, "discard"); err != status_error.WorkspaceResourceConflict {
				t.Fatalf("discard without confirmation = %v", err)
			}
			writeGitNote(t, repo, tc.later)
			request.ExpectedWorktreeVersion = preview.WorktreeVersion
			if err := m.ChangeIndex(t.Context(), scope.subject, request, "discard"); err != status_error.WorkspaceResourceConflict {
				t.Fatalf("discard using stale confirmation = %v", err)
			}
			content, err := os.ReadFile(filepath.Join(repo, "note.txt"))
			if err != nil || string(content) != tc.later {
				t.Fatalf("later worktree change = %q %v", content, err)
			}
		})
	}
}

func gitActionRequest(t *testing.T, m *Manager, scope Scope, repo string, action constants.GitActionKind) GitActionRequest {
	t.Helper()
	status, err := m.Status(context.Background(), scope.subject, scope.ID, "", repo)
	if err != nil {
		t.Fatal(err)
	}
	return GitActionRequest{Action: action, ScopeID: scope.ID, Repository: repo, ExpectedBranch: &status.Branch, ExpectedHead: &status.Head, ExpectedIndex: &status.Index}
}

func gitActionChatScope(t *testing.T, m *Manager, scope Scope, repo string) Scope {
	t.Helper()
	private := filepath.Join(t.TempDir(), "chats", "chat")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := m.OpenChat(scope.subject, "chat", repo, private, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func writeGitNote(t *testing.T, repo, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "note.txt"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGitActionCommitSelectedFilesPreservesOtherIndexEntries(t *testing.T) {
	m, scope, repo := gitFixture(t)
	writeGitNote(t, repo, "selected staged\n")
	gitRun(t, repo, "add", "note.txt")
	writeGitNote(t, repo, "selected worktree\n")
	for _, path := range []string{"other.txt", "remove.txt"} {
		if err := os.WriteFile(filepath.Join(repo, path), []byte("original\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, repo, "add", "other.txt", "remove.txt")
	gitRun(t, repo, "commit", "--only", "-m", "Prepare paths", "--", "other.txt", "remove.txt")
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("other staged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "other.txt")
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("other worktree\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "*.txt"), []byte("new literal file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "remove.txt")); err != nil {
		t.Fatal(err)
	}
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
	request.FilePaths = []string{"note.txt", "*.txt", "remove.txt"}
	request.Message = "Commit selected files"
	result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
	if err != nil || result.Commit == "" {
		t.Fatalf("commit selected files: %+v %v", result, err)
	}
	if actual := gitRun(t, repo, "diff-tree", "--no-commit-id", "--name-only", "-r", result.Commit); actual != "*.txt\nnote.txt\nremove.txt" {
		t.Fatalf("unexpected commit files: %q", actual)
	}
	if actual := gitRun(t, repo, "show", "HEAD:note.txt"); actual != "selected worktree" {
		t.Fatal(actual)
	}
	if actual := gitRun(t, repo, "diff", "--cached", "--name-only"); actual != "other.txt" {
		t.Fatalf("other index entries changed: %q", actual)
	}
	if actual := gitRun(t, repo, "show", ":other.txt"); actual != "other staged" {
		t.Fatal(actual)
	}
	if actual, err := os.ReadFile(filepath.Join(repo, "other.txt")); err != nil || string(actual) != "other worktree\n" {
		t.Fatalf("other worktree entry changed: %q %v", actual, err)
	}
}

func TestGitActionSelectedMessageRejectsChangedFile(t *testing.T) {
	m, scope, repo := gitFixture(t)
	scope = gitActionChatScope(t, m, scope, repo)
	writeGitNote(t, repo, "unselected staged\n")
	gitRun(t, repo, "add", "note.txt")
	path := filepath.Join(repo, "new.txt")
	if err := os.WriteFile(path, []byte("selected initial\n"), 0600); err != nil {
		t.Fatal(err)
	}
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
	request.FilePaths = []string{"new.txt"}
	result, err := m.RunGitAction(t.Context(), scope.subject, request, func(_ context.Context, _ string, patch string) (string, bool, error) {
		if !strings.Contains(patch, "+selected initial") || strings.Contains(patch, "unselected staged") {
			t.Fatalf("wrong generation context: %q", patch)
		}
		if err := os.WriteFile(path, []byte("selected changed\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return "Add selected new file", false, nil
	}, nil)
	if err != status_error.WorkspaceResourceConflict || result.Commit != "" {
		t.Fatalf("selected content changed during generation: %+v %v", result, err)
	}
	if actual := gitRun(t, repo, "diff", "--cached", "--name-only"); actual != "note.txt" {
		t.Fatalf("generation changed the real index: %q", actual)
	}
}

func TestGitActionSelectedCommitHookFailurePreservesRealIndex(t *testing.T) {
	m, scope, repo := gitFixture(t)
	writeGitNote(t, repo, "staged original\n")
	gitRun(t, repo, "add", "note.txt")
	writeGitNote(t, repo, "selected worktree\n")
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("selected new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
	request.FilePaths = []string{"note.txt", "new.txt"}
	request.Message = "Commit selected files"
	result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
	if err != status_error.WorkspaceGitFailed || result.Commit != "" {
		t.Fatalf("hook did not abort selected commit: %+v %v", result, err)
	}
	if actual := gitRun(t, repo, "show", ":note.txt"); actual != "staged original" {
		t.Fatalf("failed commit changed the real index: %q", actual)
	}
	if actual := gitRun(t, repo, "diff", "--cached", "--name-only"); actual != "note.txt" {
		t.Fatalf("failed commit changed the real index: %q", actual)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("failed commit left the index locked: %v", err)
	}
}

func TestGitActionRejectsUnsafeOrEmptySelectedPaths(t *testing.T) {
	m, scope, repo := gitFixture(t)
	writeGitNote(t, repo, "staged original\n")
	gitRun(t, repo, "add", "note.txt")
	if err := os.Mkdir(filepath.Join(repo, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(repo, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]string{{}, {"../note.txt"}, {"folder"}, {"escape/unsafe.txt"}, {"runtime/accounts/unsafe.txt"}, {"Runtime/Accounts/unsafe.txt"}, {"./note.txt"}} {
		request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
		request.FilePaths, request.Message = paths, "Commit invalid selection"
		result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
		if err != status_error.WorkspacePathInvalid || result.Commit != "" {
			t.Fatalf("invalid selection %q: %+v %v", paths, result, err)
		}
	}
}

func TestGitActionSelectedCommitRejectsUnselectedStagedDescendant(t *testing.T) {
	for _, addMode := range []string{"-A", "-N"} {
		t.Run(addMode, func(t *testing.T) {
			m, scope, repo := gitFixture(t)
			parent := filepath.Join(repo, "replace")
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			nested := filepath.Join(parent, "unselected.txt")
			if err := os.WriteFile(nested, []byte("unselected staged\n"), 0600); err != nil {
				t.Fatal(err)
			}
			gitRun(t, repo, "add", addMode, "replace/unselected.txt")
			original := gitRun(t, repo, "ls-files", "--stage", "replace/unselected.txt")
			if err := os.RemoveAll(parent); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(parent, []byte("selected replacement\n"), 0600); err != nil {
				t.Fatal(err)
			}
			request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
			request.FilePaths, request.Message = []string{"replace"}, "Commit file only"
			result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
			if err != status_error.WorkspaceResourceConflict || result.Commit != "" {
				t.Fatalf("selected file replaced unselected index entry: %+v %v", result, err)
			}
			if actual := gitRun(t, repo, "ls-files", "--stage", "replace/unselected.txt"); actual != original {
				t.Fatalf("unselected index entry changed: %q", actual)
			}
		})
	}
}

func TestGitActionSelectedCommitIncludesStagedRenameSource(t *testing.T) {
	m, scope, repo := gitFixture(t)
	gitRun(t, repo, "mv", "note.txt", "renamed.txt")
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
	request.FilePaths, request.Message = []string{"renamed.txt"}, "Rename note"
	result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
	if err != nil || result.Commit == "" {
		t.Fatalf("selected rename was not committed: %+v %v", result, err)
	}
	if actual := gitRun(t, repo, "diff-tree", "--no-commit-id", "--name-status", "-r", "-M", result.Commit); actual != "R100\tnote.txt\trenamed.txt" {
		t.Fatalf("rename source was not included: %q", actual)
	}
}

func TestGitActionSelectedCommitRejectsHookIndexExpansion(t *testing.T) {
	m, scope, repo := gitFixture(t)
	writeGitNote(t, repo, "checked change\n")
	if err := os.WriteFile(filepath.Join(repo, "unchecked.txt"), []byte("unchecked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writeGitCommitHook(t, repo, "git add -- unchecked.txt")
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
	request.FilePaths, request.Message = []string{"note.txt"}, "Commit checked change"
	result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
	if err != status_error.WorkspaceResourceConflict || result.Commit != "" {
		t.Fatalf("hook expanded selected commit: %+v %v", result, err)
	}
	if got := gitRun(t, repo, "rev-parse", "HEAD"); got != *request.ExpectedHead {
		t.Fatalf("hook expansion moved HEAD: %q", got)
	}
	if got := gitRun(t, repo, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("hook expansion changed real index: %q", got)
	}
}

func TestGitActionSelectedCommitUsesFrozenTreeAfterHookEditsWorktree(t *testing.T) {
	m, scope, repo := gitFixture(t)
	writeGitNote(t, repo, "confirmed contents\n")
	writeGitCommitHook(t, repo, "printf 'hook worktree edit\\n' > note.txt")
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
	request.FilePaths, request.Message = []string{"note.txt"}, "Commit confirmed contents"
	result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
	if err != nil || result.Commit == "" {
		t.Fatalf("commit selected snapshot: %+v %v", result, err)
	}
	if got := gitRun(t, repo, "show", "HEAD:note.txt"); got != "confirmed contents" {
		t.Fatalf("commit included unconfirmed content: %q", got)
	}
	if actual, err := os.ReadFile(filepath.Join(repo, "note.txt")); err != nil || string(actual) != "hook worktree edit\n" {
		t.Fatalf("hook edit should remain in the worktree: %q %v", actual, err)
	}
}

func writeGitCommitHook(t *testing.T, repo, command string) {
	t.Helper()
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nset -eu\n"+command+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestGitActionSelectedCommitRejectsRefMovedByHook(t *testing.T) {
	m, scope, repo := gitFixture(t)
	writeGitNote(t, repo, "checked change\n")
	writeGitCommitHook(t, repo, `old=$(git rev-parse HEAD)
tree=$(git rev-parse 'HEAD^{tree}')
new=$(git commit-tree "$tree" -p "$old" -m external)
git update-ref HEAD "$new" "$old"`)
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
	request.FilePaths, request.Message = []string{"note.txt"}, "Commit stale selection"
	result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
	if err != status_error.WorkspaceResourceConflict || result.Commit != "" {
		t.Fatalf("commit advanced a stale ref: %+v %v", result, err)
	}
	if got := gitRun(t, repo, "log", "-1", "--format=%s"); got != "external" {
		t.Fatalf("external ref update was overwritten: %q", got)
	}
	if got := gitRun(t, repo, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("stale commit changed the index: %q", got)
	}
}

func TestGitActionSelectedCommitKeepsTrackedGitlink(t *testing.T) {
	m, scope, repo := gitFixture(t)
	module := filepath.Join(repo, "module")
	if err := os.Mkdir(module, 0700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, module, "init", "-b", "main")
	gitRun(t, module, "config", "user.email", "test@example.invalid")
	gitRun(t, module, "config", "user.name", "Test")
	gitRun(t, module, "commit", "--allow-empty", "-m", "Initial module")
	gitRun(t, repo, "add", "module")
	gitRun(t, repo, "commit", "-m", "Track module")
	gitRun(t, module, "commit", "--allow-empty", "-m", "Change gitlink")
	moduleHead := gitRun(t, module, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(module, "dirty"), []byte("untracked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	request := gitActionRequest(t, m, scope, repo, constants.GitActionCommit)
	request.FilePaths, request.Message = []string{"module"}, "Update module"
	result, err := m.RunGitAction(t.Context(), scope.subject, request, nil, nil)
	if err != nil || result.Commit == "" {
		t.Fatalf("tracked gitlink was not committed: %+v %v", result, err)
	}
	if actual := gitRun(t, repo, "ls-tree", "HEAD", "module"); actual != "160000 commit "+moduleHead+"\tmodule" {
		t.Fatalf("wrong gitlink commit: %q", actual)
	}
}
