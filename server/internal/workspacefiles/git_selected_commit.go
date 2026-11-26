package workspacefiles

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const gitSelectionLimit = 2000

type gitCommitSelection struct {
	// directory 保存本次请求的临时索引和预览对象。
	directory string
	// index 是 Git 报告的工作树实际索引。
	index string
	// paths 是已验证的仓库相对路径。
	paths map[string]bool
	// preview 使用与真实对象库隔离的预览索引。
	preview gitCommandOptions
	// fingerprint 标识实际选中补丁对应的 Git 对象。
	fingerprint string
	// scope 用于提交前再次验证选中路径。
	scope Scope
}

func prepareGitSelection(ctx context.Context, scope Scope, repo, head string, paths []string) (*gitCommitSelection, error) {
	selection, input, err := selectedGitPaths(ctx, scope, repo, paths)
	if err != nil {
		return nil, err
	}
	gitPaths, err := gitCommand(ctx, repo, "rev-parse", "--path-format=absolute", "--git-path", "index", "--git-path", "objects")
	if err != nil {
		return nil, err
	}
	index, objects, _ := strings.Cut(strings.TrimSuffix(gitPaths, "\n"), "\n")
	if !filepath.IsAbs(index) || !filepath.IsAbs(objects) {
		return nil, status_error.WorkspaceGitFailed
	}
	directory, err := os.MkdirTemp("", "neuxbot-git-selection-")
	if err != nil {
		return nil, fileError("commit-preview", err)
	}
	draft := &gitCommitSelection{
		directory: directory, index: index, paths: selection, scope: scope,
		preview: gitCommandOptions{
			index: filepath.Join(directory, "preview-index"), objects: filepath.Join(directory, "objects"),
			alternates: strconv.Quote(objects), input: input,
		},
	}
	if err = draft.preparePreview(ctx, repo, head); err != nil {
		draft.close()
		return nil, err
	}
	return draft, nil
}

func loadSelectedCommitIndex(ctx context.Context, repo, head string, options gitCommandOptions) error {
	if head == "" {
		head = "--empty"
	}
	if _, err := gitSelectionCommand(ctx, repo, options, "read-tree", head); err != nil {
		return err
	}
	return stageGitSelection(ctx, repo, options)
}

func (s *gitCommitSelection) preparePreview(ctx context.Context, repo, head string) error {
	if err := os.Mkdir(s.preview.objects, 0700); err != nil {
		return fileError("commit-preview-objects", err)
	}
	if err := loadSelectedCommitIndex(ctx, repo, head, s.preview); err != nil {
		return err
	}
	selected, err := gitSelectionChanges(ctx, repo, s.preview, s.paths)
	if err != nil {
		return err
	}
	s.fingerprint = selected
	return nil
}

func (s *gitCommitSelection) close() {
	_ = os.RemoveAll(s.directory)
}

func selectedGitPaths(ctx context.Context, scope Scope, repo string, paths []string) (map[string]bool, string, error) {
	if len(paths) == 0 {
		return nil, "", status_error.WorkspacePathInvalid
	}
	if len(paths) > gitSelectionLimit {
		return nil, "", status_error.WorkspaceResourceTooLarge
	}
	selected := make(map[string]bool, len(paths))
	var input strings.Builder
	add := func(path string) error {
		if selected[path] {
			return nil
		}
		if err := validateGitSelectionPath(ctx, scope, repo, path); err != nil {
			return err
		}
		selected[path] = true
		input.WriteString(":(top,literal)" + path + "\x00")
		return nil
	}
	for _, path := range paths {
		if err := add(path); err != nil {
			return nil, "", err
		}
	}
	// 已暂存的重命名在 Git 中只有一个状态项，旧文件不存在时补入其删除。
	porcelain, err := gitCommand(ctx, repo, "status", "--porcelain=v2", "-z", "--untracked-files=no")
	if err != nil {
		return nil, "", err
	}
	for rest := porcelain; rest != ""; {
		var entry string
		entry, rest, _ = strings.Cut(rest, "\x00")
		if !strings.HasPrefix(entry, "2 ") {
			continue
		}
		oldPath, remaining, _ := strings.Cut(rest, "\x00")
		rest = remaining
		xy, path := parseGitStatusEntry(entry)
		if !strings.HasPrefix(xy, "R") || !selected[path] || selected[oldPath] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(oldPath))); os.IsNotExist(err) {
			if err := add(oldPath); err != nil {
				return nil, "", err
			}
		}
	}
	return selected, input.String(), nil
}

func validateGitSelectionPath(ctx context.Context, scope Scope, repo, path string) error {
	if !validGitPath(path) || filepath.ToSlash(filepath.Clean(path)) != path {
		return status_error.WorkspacePathInvalid
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if strings.EqualFold(part, ".git") {
			return status_error.WorkspacePathInvalid
		}
	}
	relative := repo
	for index, part := range parts {
		relative = filepath.Join(relative, part)
		if !scope.allows(relative) {
			return status_error.WorkspacePathInvalid
		}
		info, err := os.Lstat(relative)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fileError("commit-path", err)
		}
		if index < len(parts)-1 {
			if !info.IsDir() {
				return status_error.WorkspacePathInvalid
			}
			continue
		}
		if info.IsDir() {
			// Gitlink 是单个索引项。普通目录路径会递归选中未确认的文件。
			entries, err := gitCommand(ctx, repo, "ls-files", "--stage", "-z", "--", ":(top,literal)"+path)
			if err != nil {
				return err
			}
			for entry := range strings.SplitSeq(entries, "\x00") {
				info, name, _ := strings.Cut(entry, "\t")
				if name == path && strings.HasPrefix(info, "160000 ") {
					return nil
				}
			}
			return status_error.WorkspacePathInvalid
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return status_error.WorkspacePathInvalid
		}
	}
	return nil
}

func gitSelectionCommand(ctx context.Context, repo string, options gitCommandOptions, args ...string) (string, error) {
	config := []string{"-c", "core.splitIndex=false", "-c", "splitIndex.sharedIndexExpire=never"}
	return gitCommandWithOptions(ctx, repo, options, append(config, args...)...)
}

func stageGitSelection(ctx context.Context, repo string, options gitCommandOptions) error {
	_, err := gitSelectionCommand(ctx, repo, options, "add", "-A", "--pathspec-from-file=-", "--pathspec-file-nul")
	return err
}

func gitSelectionChanges(ctx context.Context, repo string, options gitCommandOptions, paths map[string]bool) (string, error) {
	options.input = ""
	changes, err := gitCommandWithOptions(ctx, repo, options, "diff", "--cached", "--raw", "--no-abbrev", "--no-renames", "--no-ext-diff", "--ignore-submodules=dirty", "--no-relative", "-z", "--")
	if err != nil {
		return "", err
	}
	if changes == "" {
		return "", status_error.WorkspaceResourceConflict
	}
	for rest := changes; rest != ""; {
		_, rest, _ = strings.Cut(rest, "\x00")
		var path string
		path, rest, _ = strings.Cut(rest, "\x00")
		if !paths[path] {
			return "", status_error.WorkspacePathInvalid
		}
	}
	return changes, nil
}

func gitUnselectedIndex(ctx context.Context, repo string, options gitCommandOptions, paths map[string]bool) (string, error) {
	options.input = ""
	index, err := gitCommandWithOptions(ctx, repo, options, "ls-files", "--stage", "-z")
	if err != nil {
		return "", err
	}
	var unselected strings.Builder
	for entry := range strings.SplitSeq(strings.TrimSuffix(index, "\x00"), "\x00") {
		_, path, _ := strings.Cut(entry, "\t")
		if entry != "" && !paths[path] {
			unselected.WriteString(entry)
			unselected.WriteByte(0)
		}
	}
	return unselected.String(), nil
}

func (s *gitCommitSelection) commit(ctx context.Context, repo string, refs gitRefState, message string) (string, error) {
	lock, err := os.OpenFile(s.index+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if os.IsExist(err) {
		return "", status_error.WorkspaceResourceConflict
	}
	if err != nil {
		return "", fileError("commit-index-lock", err)
	}
	published := false
	defer func() {
		_ = lock.Close()
		if !published {
			_ = os.Remove(lock.Name())
		}
	}()

	// Git 的其他调用遵守同一个索引锁。失败时留下原索引，成功后原子发布部分提交的索引。
	if err := verifyGitCommitState(ctx, repo, refs, refs.index); err != nil {
		return "", err
	}
	for path := range s.paths {
		if err := validateGitSelectionPath(ctx, s.scope, repo, path); err != nil {
			return "", err
		}
	}
	if err := checkSelectedCommitOperation(ctx, repo); err != nil {
		return "", err
	}
	commitIndex := gitCommandOptions{index: filepath.Join(s.directory, "commit-index"), input: s.preview.input}
	if err := loadSelectedCommitIndex(ctx, repo, refs.head, commitIndex); err != nil {
		return "", err
	}
	fingerprint, err := gitSelectionChanges(ctx, repo, commitIndex, s.paths)
	if err != nil {
		return "", err
	}
	if fingerprint != s.fingerprint {
		return "", status_error.WorkspaceResourceConflict
	}
	commitIndex.input = ""
	tree, err := gitSelectionCommand(ctx, repo, commitIndex, "write-tree")
	if err != nil {
		return "", err
	}
	tree = strings.TrimSpace(tree)
	options := gitCommandOptions{index: filepath.Join(s.directory, "write-index"), input: s.preview.input}
	if original, err := os.Lstat(s.index); err == nil {
		if !original.Mode().IsRegular() {
			return "", status_error.WorkspacePathInvalid
		}
		if err = lock.Chmod(original.Mode().Perm()); err != nil {
			return "", fileError("commit-index-mode", err)
		}
	} else if !os.IsNotExist(err) {
		return "", fileError("commit-index-stat", err)
	}
	if err := copyComparisonIndex(s.index, options.index); err != nil {
		return "", err
	}
	unselected, err := gitUnselectedIndex(ctx, repo, options, s.paths)
	if err != nil {
		return "", err
	}
	// 预先用冻结树更新用户索引的副本，避免与提交后工作树中的新编辑交错。
	if _, err := gitSelectionCommand(ctx, repo, options, "reset", "--quiet", tree, "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
		return "", err
	}
	remaining, err := gitUnselectedIndex(ctx, repo, options, s.paths)
	if err != nil {
		return "", err
	}
	if remaining != unselected {
		return "", status_error.WorkspaceResourceConflict
	}
	if err := prepareGitIndexPublication(lock, options.index); err != nil {
		return "", err
	}
	hash, subject, err := createSelectedCommit(ctx, repo, s.directory, refs.head, tree, message, commitIndex)
	if err != nil {
		return "", err
	}
	if err := checkSelectedCommitOperation(ctx, repo); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", status_error.OperationCanceled
	}
	// 引用提交后即使请求断开也要发布已经同步的索引。
	ctx = context.WithoutCancel(ctx)
	if err := publishSelectedCommitRef(ctx, repo, refs, hash, subject); err != nil {
		return "", err
	}
	if err := os.Rename(lock.Name(), s.index); err != nil {
		return hash, fileError("commit-index-publish", err)
	}
	published = true
	options.input = ""
	_, _ = gitSelectionCommand(ctx, repo, options, "hook", "run", "--ignore-missing", "post-commit")
	return hash, nil
}

func prepareGitIndexPublication(lock *os.File, replacement string) error {
	source, err := os.Open(replacement)
	if err != nil {
		return fileError("commit-index-open", err)
	}
	defer source.Close()
	count, err := io.Copy(lock, io.LimitReader(source, gitComparisonMaxIndex+1))
	if err != nil {
		return fileError("commit-index-copy", err)
	}
	if count > gitComparisonMaxIndex {
		return status_error.WorkspaceResourceTooLarge
	}
	if err := lock.Sync(); err != nil {
		return fileError("commit-index-sync", err)
	}
	if err := lock.Close(); err != nil {
		return fileError("commit-index-close", err)
	}
	return nil
}
