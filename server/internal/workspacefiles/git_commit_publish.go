package workspacefiles

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

func checkSelectedCommitOperation(ctx context.Context, repo string) error {
	args := []string{"rev-parse", "--path-format=absolute"}
	for _, state := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "sequencer", "rebase-merge", "rebase-apply"} {
		args = append(args, "--git-path", state)
	}
	paths, err := gitCommand(ctx, repo, args...)
	if err != nil {
		return err
	}
	for path := range strings.SplitSeq(strings.TrimSuffix(paths, "\n"), "\n") {
		if !filepath.IsAbs(path) {
			return status_error.WorkspaceGitFailed
		}
		if _, err := os.Lstat(path); err == nil {
			return status_error.WorkspaceResourceConflict
		} else if !os.IsNotExist(err) {
			return fileError("commit-operation-state", err)
		}
	}
	for _, ref := range []string{"CHERRY_PICK_HEAD", "REVERT_HEAD"} {
		resolved, err := gitCommandExit(ctx, repo, true, "-c", "core.warnAmbiguousRefs=false", "rev-parse", "--verify", "--quiet", "--symbolic-full-name", ref)
		if err != nil {
			return err
		}
		if strings.TrimSpace(resolved) == ref {
			return status_error.WorkspaceResourceConflict
		}
	}
	return nil
}

func createSelectedCommit(ctx context.Context, repo, directory, parent, tree, message string, options gitCommandOptions) (string, string, error) {
	messagePath := filepath.Join(directory, "COMMIT_EDITMSG")
	if err := os.WriteFile(messagePath, []byte(message+"\n"), 0600); err != nil {
		return "", "", fileError("commit-message-write", err)
	}
	for _, hook := range [][]string{{"pre-commit"}, {"prepare-commit-msg", "--", messagePath, "message"}, {"commit-msg", "--", messagePath}} {
		args := append([]string{"hook", "run", "--ignore-missing"}, hook...)
		if _, err := gitSelectionCommand(ctx, repo, options, args...); err != nil {
			return "", "", err
		}
	}
	hookTree, err := gitSelectionCommand(ctx, repo, options, "write-tree")
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(hookTree) != tree {
		return "", "", status_error.WorkspaceResourceConflict
	}
	message, err = readSelectedCommitMessage(ctx, repo, messagePath)
	if err != nil {
		return "", "", err
	}
	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	sign, err := gitCommandExit(ctx, repo, true, "config", "--type=bool", "--get", "commit.gpgSign")
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(sign) == "true" {
		args = append(args, "-S")
	}
	options.input = message
	hash, err := gitCommandWithOptions(ctx, repo, options, append(args, "-F", "-")...)
	subject, _, _ := strings.Cut(message, "\n")
	return strings.TrimSpace(hash), subject, err
}

func readSelectedCommitMessage(ctx context.Context, repo, path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fileError("commit-message-stat", err)
	}
	if !info.Mode().IsRegular() {
		return "", status_error.WorkspaceGitFailed
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fileError("commit-message-open", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, gitCommitMessageLimit+2))
	if err != nil {
		return "", fileError("commit-message-read", err)
	}
	message := string(raw)
	if strings.TrimSpace(message) == "" || len(message) > gitCommitMessageLimit+1 || strings.ContainsRune(message, '\x00') {
		return "", status_error.WorkspaceGitFailed
	}
	cleanup, err := gitCommandExit(ctx, repo, true, "config", "--get", "commit.cleanup")
	if err != nil {
		return "", err
	}
	args := []string{"stripspace"}
	switch strings.TrimSpace(cleanup) {
	case "verbatim":
		return message, nil
	case "strip":
		args = append(args, "--strip-comments")
	case "", "default", "scissors", "whitespace":
	default:
		return "", status_error.WorkspaceGitFailed
	}
	message, err = gitCommandWithOptions(ctx, repo, gitCommandOptions{input: message}, args...)
	if err == nil && message == "" {
		err = status_error.WorkspaceGitFailed
	}
	return message, err
}

func publishSelectedCommitRef(ctx context.Context, repo string, refs gitRefState, hash, subject string) error {
	ref := "refs/heads/" + refs.branch
	args := []string{"update-ref", "-m", "commit: " + subject, "--stdin"}
	if refs.branch == detachedGitBranch {
		ref = "HEAD"
		args = append(args, "--no-deref")
	}
	old := refs.head
	if old == "" {
		old = strings.Repeat("0", len(hash))
	}
	if err := runSelectedRefTransaction(ctx, repo, ref, old, hash, refs, args); err != nil {
		// 提交响应可能在引用已更新后丢失，按本次唯一候选对象对账。
		current, readErr := gitRefs(ctx, repo)
		if readErr == nil && current.branch == refs.branch && current.head == hash {
			return nil
		}
		return err
	}
	return nil
}

func runSelectedRefTransaction(ctx context.Context, repo, ref, old, hash string, refs gitRefState, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := newWorkspaceGitCommand(ctx, repo, gitCommandOptions{}, args...)
	stderr := &boundedGitOutput{limit: 64 * 1024, keepPrefix: true}
	command.Stderr = stderr
	input, err := command.StdinPipe()
	if err != nil {
		return fileError("commit-ref-input", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return fileError("commit-ref-output", err)
	}
	if err = command.Start(); err != nil {
		_ = input.Close()
		return fileError("commit-ref-start", err)
	}
	// 先让 Git 同时保护分支引用和 HEAD，再核验用户确认时的状态。
	_, writeErr := io.WriteString(input, "start\nupdate "+ref+" "+hash+" "+old+"\nprepare\n")
	reader := bufio.NewReader(output)
	start, startErr := reader.ReadString('\n')
	prepared, prepareErr := reader.ReadString('\n')
	if writeErr != nil || startErr != nil || start != "start: ok\n" || prepareErr != nil || prepared != "prepare: ok\n" {
		_ = input.Close()
		_ = command.Wait()
		logx.Warnf("workspace git ref prepare failed: stderr=%q", stderr.buffer.String())
		if verifyErr := verifyGitCommitState(ctx, repo, refs, refs.index); verifyErr != nil {
			return verifyErr
		}
		return status_error.WorkspaceGitFailed
	}
	if err = verifyGitCommitState(ctx, repo, refs, refs.index); err != nil {
		_, _ = io.WriteString(input, "abort\n")
		_ = input.Close()
		_ = command.Wait()
		return err
	}
	_, writeErr = io.WriteString(input, "commit\n")
	_ = input.Close()
	waitErr := command.Wait()
	if writeErr != nil || waitErr != nil {
		logx.Warnf("workspace git ref update failed: write_error=%v wait_error=%v stderr=%q", writeErr, waitErr, stderr.buffer.String())
		return status_error.WorkspaceGitFailed
	}
	return nil
}
