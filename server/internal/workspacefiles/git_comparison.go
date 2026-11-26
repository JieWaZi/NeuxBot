package workspacefiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const (
	gitComparisonMaxEntries = 2000
	gitComparisonAutoBase   = ":auto"
	gitComparisonMaxMeta    = 16 * 1024 * 1024
	gitComparisonMaxPatch   = 512 * 1024
	gitComparisonMaxIndex   = 128 * 1024 * 1024
)

// GitComparisonEntry 是比较中的单个文件。
type GitComparisonEntry struct {
	// 仓库相对路径。
	Path string `json:"path"`
	// 重命名前路径。
	OldPath string `json:"oldPath,omitempty"`
	// Git 变更状态。
	Status string `json:"status"`
	// 新增行数。
	AddedLines int `json:"addedLines"`
	// 删除行数。
	DeletedLines int `json:"deletedLines"`
	// 行数是否未知。
	LineCountUnavailable bool `json:"lineCountUnavailable"`
}

// GitComparison 是一次仓库比较结果。
type GitComparison struct {
	// 当前仓库根目录。
	Root string `json:"root"`
	// 当前分支。
	Branch string `json:"branch"`
	// 实际比较基准。
	BaseRef string `json:"baseRef,omitempty"`
	// 变更文件。
	Entries []GitComparisonEntry `json:"entries"`
	// 单文件补丁。
	Content string `json:"content,omitempty"`
	// 未暂存补丁版本。
	WorktreeVersion string `json:"worktreeVersion,omitempty"`
	// 是否超过输出上限。
	Truncated bool `json:"truncated,omitempty"`
}

type gitComparisonRef struct {
	// full 是带命名空间的完整分支引用。
	full string
	// short 是分支下拉可用的名称。
	short string
	// oid 固定本次比较读取到的提交。
	oid string
	// symbolic 是远程 HEAD 所指向的引用。
	symbolic string
}

type gitComparisonRange struct {
	// branch 是当前分支名，分离头时为 HEAD。
	branch string
	// base 是向用户展示的比较基准。
	base string
	// head 是分支比较使用的固定提交，未产生提交时为空。
	head string
	// args 是本次 Git diff 的比较对象或索引参数。
	args []string
	// untracked 标识需要通过独立索引纳入未跟踪文件。
	untracked bool
}

// Comparison 读取授权仓库的文件元数据，或指定文件的有界补丁。
func (m *Manager) Comparison(ctx context.Context, subject, id, root, selected, base, path string, ignoreWhitespace bool, previousPath *string) (GitComparison, error) {
	if path != "" && !validGitPath(path) {
		return GitComparison{}, status_error.WorkspacePathInvalid
	}
	paths := []string{}
	if path != "" {
		paths = append(paths, path)
	}
	if previousPath != nil {
		if path == "" {
			return GitComparison{}, status_error.WorkspacePathInvalid
		}
		if previous := *previousPath; previous != "" {
			if !validGitPath(previous) || filepath.ToSlash(filepath.Clean(previous)) != previous || previous == path {
				return GitComparison{}, status_error.WorkspacePathInvalid
			}
			if base != ":unstaged" {
				paths = append(paths, previous)
			}
		}
	}
	repo, _, err := m.repository(ctx, subject, id, root, selected)
	if err != nil {
		return GitComparison{}, err
	}
	result := GitComparison{Root: repo, Entries: []GitComparisonEntry{}}
	if repo == "" {
		return result, nil
	}
	span, err := comparisonRange(ctx, repo, base)
	if err != nil {
		return GitComparison{}, err
	}
	result.Branch = span.branch
	result.BaseRef = span.base
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	diffArgs := gitComparisonDiffOptions(base, ignoreWhitespace)
	var options gitCommandOptions
	if span.untracked {
		var cleanup func()
		options, cleanup, result.Truncated, err = prepareComparisonIndex(ctx, repo, paths)
		if err != nil {
			return GitComparison{}, err
		}
		defer cleanup()
	}
	readEntries := func(paths []string, options gitCommandOptions) ([]GitComparisonEntry, bool, error) {
		args := comparisonDiffArgs(diffArgs, []string{"--raw", "--numstat", "-z"}, span.args, paths)
		output, truncated, err := comparisonGit(ctx, repo, options, gitComparisonMaxMeta, false, args...)
		if err != nil {
			return nil, false, err
		}
		return parseComparisonEntries(output, ignoreWhitespace, truncated), truncated, nil
	}
	entries, truncated, err := readEntries(paths, options)
	if err != nil {
		return GitComparison{}, err
	}
	// 文件列表中的旧路径可能已失效。只有当前 Diff 仍确认这对路径时才返回重命名补丁。
	if len(paths) == 2 && (len(entries) != 1 || entries[0].Path != path || entries[0].OldPath != paths[1]) {
		paths = paths[:1]
		entries, truncated, err = readEntries(paths, options)
		if err != nil {
			return GitComparison{}, err
		}
	}
	result.Truncated = result.Truncated || truncated
	if path == "" {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		result.Truncated = result.Truncated || len(entries) > gitComparisonMaxEntries
		result.Entries = entries[:min(len(entries), gitComparisonMaxEntries)]
		return result, nil
	}

	for _, entry := range entries {
		if entry.Path != path {
			continue
		}
		result.Entries = append(result.Entries, entry)
		if base == ":unstaged" && entry.Status != "A" {
			head, _, headErr := comparisonGit(ctx, repo, gitCommandOptions{}, 512, true, "rev-parse", "--verify", "--quiet", "HEAD")
			if headErr != nil {
				return GitComparison{}, headErr
			}
			result.Content, result.WorktreeVersion, truncated, err = worktreeComparisonPatch(ctx, repo, path, span.branch, strings.TrimSpace(head), ignoreWhitespace)
		} else {
			args := comparisonDiffArgs(diffArgs, []string{"--patch"}, span.args, paths)
			result.Content, truncated, err = comparisonGit(ctx, repo, options, gitComparisonMaxPatch, false, args...)
		}
		if err != nil {
			return GitComparison{}, err
		}
		if truncated {
			// 不把缺失后半段的 hunk 交给补丁解析器。
			result.Content = ""
			result.Truncated = true
		}
		break
	}
	return result, nil
}

func gitComparisonDiffOptions(base string, ignoreWhitespace bool) []string {
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-relative", "--src-prefix=a/", "--dst-prefix=b/", "--line-prefix=", "--find-renames", "--ita-invisible-in-index", "--ignore-submodules=dirty", "--submodule=short"}
	if base == ":unstaged" {
		// 未暂存组按实际索引路径展示，新增与删除可以分别暂存或恢复。
		args = append(args, "--no-renames")
	}
	if ignoreWhitespace {
		args = append(args, "--ignore-all-space")
	}
	return args
}

func worktreeComparisonPatch(ctx context.Context, repo, path, branch, head string, ignoreWhitespace bool) (string, string, bool, error) {
	args := comparisonDiffArgs(gitComparisonDiffOptions(":unstaged", ignoreWhitespace), []string{"--patch", "--full-index"}, nil, []string{path})
	output := &comparisonOutput{limit: gitComparisonMaxPatch}
	digest := sha256.New()
	// 完整补丁的对象标识包含二进制和忽略空白时未显示的内容。预览截断不影响版本。
	_, _ = io.WriteString(digest, branch+"\x00"+head+"\x00")
	if err := runComparisonGit(ctx, repo, gitCommandOptions{}, io.MultiWriter(output, digest), false, args...); err != nil {
		return "", "", false, err
	}
	if output.buffer.Len() == 0 {
		return "", "", false, nil
	}
	return output.buffer.String(), hex.EncodeToString(digest.Sum(nil)), output.truncated, nil
}

func comparisonDiffArgs(diff, format, revisions, paths []string) []string {
	args := append(append([]string{}, diff...), format...)
	args = append(args, revisions...)
	args = append(args, "--")
	for _, path := range paths {
		args = append(args, comparisonPathspec(path))
	}
	return append(args, gitExcludeAccountRoot, gitExcludeAccounts)
}

var comparisonPathEscapes = strings.NewReplacer("*", "\\*", "?", "\\?", "[", "\\[")

func comparisonPathspec(path string) string {
	// Git 的 literal 也会匹配目录后代。转义末尾字符使 glob 精确匹配这个路径。
	pattern := comparisonPathEscapes.Replace(path)
	if pattern == path {
		_, size := utf8.DecodeLastRuneInString(path)
		end := len(path) - size
		pattern = path[:end] + "\\" + path[end:]
	}
	return ":(top,glob)" + pattern
}

func comparisonRange(ctx context.Context, repo, base string) (gitComparisonRange, error) {
	headInfo, _, err := comparisonGit(ctx, repo, gitCommandOptions{}, 512, true, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return gitComparisonRange{}, err
	}
	span := gitComparisonRange{branch: strings.TrimPrefix(strings.TrimSuffix(headInfo, "\n"), "refs/heads/")}
	if span.branch == "" {
		span.branch = "HEAD"
	}
	switch {
	case base == ":staged":
		span.base, span.args = "HEAD", []string{"--cached"}
	case base == ":unstaged":
		span.base, span.untracked = "index", true
	case strings.HasPrefix(base, ":commit:"):
		hash := strings.TrimPrefix(base, ":commit:")
		if !commitRevision.MatchString(hash) {
			return span, status_error.WorkspacePathInvalid
		}
		parents, err := gitCommand(ctx, repo, "rev-list", "--parents", "-n", "1", hash, "--")
		if err != nil {
			return span, err
		}
		fields := strings.Fields(parents)
		if len(fields) == 0 {
			return span, status_error.WorkspacePathInvalid
		}
		span.base = fields[0]
		if len(fields) > 1 {
			span.args = []string{fields[1], fields[0]}
			return span, nil
		}
		tree, err := gitCommand(ctx, repo, "hash-object", "-t", "tree", "--stdin")
		if err != nil {
			return span, err
		}
		span.args = []string{strings.TrimSpace(tree), fields[0]}
	default:
		span.base, span.untracked = "HEAD", true
		head, _, err := comparisonGit(ctx, repo, gitCommandOptions{}, 512, true, "rev-parse", "--verify", "--quiet", "HEAD")
		if err != nil {
			return span, err
		}
		head = strings.TrimSpace(head)
		span.head = head
		if head == "" {
			if base != "" && base != "HEAD" {
				return span, status_error.WorkspacePathInvalid
			}
			tree, err := gitCommand(ctx, repo, "hash-object", "-t", "tree", "--stdin")
			if err != nil {
				return span, err
			}
			span.args = []string{strings.TrimSpace(tree)}
			return span, nil
		}
		span.args = []string{head}
		if base == "HEAD" {
			return span, nil
		}
		ref, err := chooseComparisonRef(ctx, repo, span.branch, base)
		if err != nil || ref.oid == "" {
			return span, err
		}
		ancestor, _, err := comparisonGit(ctx, repo, gitCommandOptions{}, 512, true, "merge-base", ref.oid, head)
		if err != nil {
			return span, err
		}
		if ancestor = strings.TrimSpace(ancestor); ancestor != "" {
			span.base, span.args = ref.short, []string{ancestor}
		} else if base != "" {
			return span, status_error.WorkspaceResourceConflict
		}
	}
	return span, nil
}

func chooseComparisonRef(ctx context.Context, repo, branch, base string) (gitComparisonRef, error) {
	if base == "" && branch == "HEAD" {
		return gitComparisonRef{}, nil
	}
	out, err := gitCommand(ctx, repo, "for-each-ref", "--format=%(refname)%09%(refname:short)%09%(objectname)%09%(symref)%09%(upstream:remotename)%09%(upstream)", "refs/heads", "refs/remotes")
	if err != nil {
		return gitComparisonRef{}, err
	}
	refs := map[string]gitComparisonRef{}
	remoteHeads := map[string]bool{}
	hasOrigin := false
	var trackingRemote, upstream string
	var shortMatch gitComparisonRef
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			continue
		}
		ref := gitComparisonRef{full: fields[0], short: fields[1], oid: fields[2], symbolic: fields[3]}
		refs[ref.full] = ref
		if ref.full == "refs/heads/"+branch {
			trackingRemote, upstream = fields[4], fields[5]
		}
		if relative, remote := strings.CutPrefix(ref.full, "refs/remotes/"); remote {
			hasOrigin = hasOrigin || strings.HasPrefix(relative, "origin/")
			if name, head := strings.CutSuffix(relative, "/HEAD"); head && ref.symbolic != "" {
				remoteHeads[name] = true
			}
		}
		if ref.short == base && ref.symbolic == "" {
			shortMatch = ref
		}
	}
	if base != "" {
		if ref := refs[base]; ref.oid != "" && ref.symbolic == "" {
			return ref, nil
		}
		if shortMatch.oid != "" {
			return shortMatch, nil
		}
		return gitComparisonRef{}, status_error.WorkspacePathInvalid
	}
	// 优先取当前跟踪远端，其次取 origin 或唯一已知的远程默认分支。
	remote := trackingRemote
	if remote == "" || remote == "." {
		remote = ""
		if hasOrigin {
			remote = "origin"
		} else if len(remoteHeads) == 1 {
			for name := range remoteHeads {
				remote = name
			}
		}
	}
	remotePrefix := "refs/remotes/" + remote + "/"
	if ref := refs[refs[remotePrefix+"HEAD"].symbolic]; ref.oid != "" {
		return ref, nil
	}
	for _, name := range gitConventionalMainBranches {
		if name == branch && strings.HasPrefix(upstream, "refs/remotes/") && refs[upstream].oid != "" {
			return refs[upstream], nil
		}
		if ref := refs[remotePrefix+name]; ref.oid != "" {
			return ref, nil
		}
		if ref := refs["refs/heads/"+name]; ref.oid != "" && name != branch {
			return ref, nil
		}
	}
	return gitComparisonRef{}, nil
}

func prepareComparisonIndex(ctx context.Context, repo string, paths []string) (gitCommandOptions, func(), bool, error) {
	var empty gitCommandOptions
	noop := func() {}
	args := []string{"ls-files", "--others", "--exclude-standard", "-z", "--"}
	for _, path := range paths {
		args = append(args, comparisonPathspec(path))
	}
	args = append(args, gitExcludeAccountRoot, gitExcludeAccounts)
	listed, truncated, err := comparisonGit(ctx, repo, empty, gitComparisonMaxMeta, false, args...)
	if err != nil {
		return empty, noop, false, err
	}
	var candidates strings.Builder
	count := 0
	for name := range strings.SplitSeq(listed[:lastCompleteGitRecord(listed)], "\x00") {
		if !validGitPath(name) || (len(paths) > 0 && !slices.Contains(paths, name)) {
			continue
		}
		if count >= gitComparisonMaxEntries {
			truncated = true
			break
		}
		count++
		candidates.WriteString(":(literal)" + name + "\x00")
	}
	if count == 0 {
		return empty, noop, truncated, nil
	}
	indexPath, err := gitCommand(ctx, repo, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return empty, noop, false, err
	}
	objectsPath, err := gitCommand(ctx, repo, "rev-parse", "--path-format=absolute", "--git-path", "objects")
	if err != nil {
		return empty, noop, false, err
	}
	indexPath = strings.TrimSuffix(indexPath, "\n")
	objectsPath = strings.TrimSuffix(objectsPath, "\n")
	if !filepath.IsAbs(indexPath) || !filepath.IsAbs(objectsPath) {
		return empty, noop, false, status_error.WorkspaceGitFailed
	}
	temp, err := os.MkdirTemp("", "neuxbot-git-comparison-")
	if err != nil {
		return empty, noop, false, fileError("comparison-index", err)
	}
	cleanup := func() { _ = os.RemoveAll(temp) }
	options := gitCommandOptions{index: filepath.Join(temp, "index"), objects: filepath.Join(temp, "objects")}
	if err := copyComparisonIndex(indexPath, options.index); err != nil {
		cleanup()
		return empty, noop, false, err
	}
	if err := os.Mkdir(options.objects, 0700); err != nil {
		cleanup()
		return empty, noop, false, fileError("comparison-objects", err)
	}
	// 写入意向条目时 Git 也会尝试刷新备用对象库中空 blob 所在文件的时间戳。
	// 写入阶段只使用临时对象库，完成后再接入真实对象读取补丁。
	options.input = candidates.String()
	_, _, err = comparisonGit(ctx, repo, options, gitComparisonMaxMeta, false,
		"-c", "core.splitIndex=false", "-c", "splitIndex.sharedIndexExpire=never",
		"add", "--intent-to-add", "--pathspec-from-file=-", "--pathspec-file-nul")
	if err != nil {
		cleanup()
		return empty, noop, false, err
	}
	options.alternates = strconv.Quote(objectsPath)
	options.input = ""
	return options, cleanup, truncated, nil
}

func copyComparisonIndex(source, target string) error {
	from, err := os.Open(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fileError("comparison-index", err)
	}
	defer from.Close()
	info, err := from.Stat()
	if err != nil {
		return fileError("comparison-index", err)
	}
	if !info.Mode().IsRegular() || info.Size() > gitComparisonMaxIndex {
		return status_error.WorkspaceResourceTooLarge
	}
	to, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fileError("comparison-index", err)
	}
	count, err := io.Copy(to, io.LimitReader(from, gitComparisonMaxIndex+1))
	closeErr := to.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return fileError("comparison-index", err)
	}
	if count > gitComparisonMaxIndex {
		return status_error.WorkspaceResourceTooLarge
	}
	return nil
}

func lastCompleteGitRecord(output string) int {
	return strings.LastIndexByte(output, 0) + 1
}

func parseComparisonEntries(output string, ignoreWhitespace, truncated bool) []GitComparisonEntry {
	output = output[:lastCompleteGitRecord(output)]
	fields := strings.Split(output, "\x00")
	entries := []GitComparisonEntry{}
	modeChanged := map[string]bool{}
	offset := 0
	for offset < len(fields) && strings.HasPrefix(fields[offset], ":") {
		raw := strings.Fields(fields[offset])
		if len(raw) != 5 || offset+1 >= len(fields) || raw[4] == "" {
			break
		}
		status, path := raw[4][:1], fields[offset+1]
		offset += 2
		oldPath := ""
		if status == "R" || status == "C" {
			if offset >= len(fields) {
				break
			}
			oldPath, path = path, fields[offset]
			offset++
		}
		if !validGitPath(path) || (oldPath != "" && !validGitPath(oldPath)) {
			continue
		}
		modeChanged[path] = strings.TrimPrefix(raw[0], ":") != raw[1]
		entries = append(entries, GitComparisonEntry{Path: path, OldPath: oldPath, Status: status})
	}
	stats := parseNumstat(strings.Join(fields[offset:], "\x00"))
	result := entries[:0]
	for _, entry := range entries {
		line, exists := stats[entry.Path]
		entry.AddedLines, entry.DeletedLines = line.added, line.deleted
		entry.LineCountUnavailable = (!exists && truncated) || line.unavailable
		if ignoreWhitespace && entry.Status == "M" && !modeChanged[entry.Path] && !entry.LineCountUnavailable &&
			entry.AddedLines == 0 && entry.DeletedLines == 0 {
			continue
		}
		result = append(result, entry)
	}
	return result
}

type comparisonOutput struct {
	// buffer 保存有界输出前缀。
	buffer bytes.Buffer
	// limit 是最多保存的字节数。
	limit int
	// truncated 表明命令输出已经超过限制。
	truncated bool
}

func (w *comparisonOutput) Write(p []byte) (int, error) {
	size := min(len(p), w.limit-w.buffer.Len())
	_, _ = w.buffer.Write(p[:size])
	w.truncated = w.truncated || size < len(p)
	return len(p), nil
}

func comparisonGit(ctx context.Context, repo string, options gitCommandOptions, limit int, allowEmpty bool, args ...string) (string, bool, error) {
	output := &comparisonOutput{limit: limit}
	if err := runComparisonGit(ctx, repo, options, output, allowEmpty, args...); err != nil {
		return "", false, err
	}
	return output.buffer.String(), output.truncated, nil
}

func runComparisonGit(ctx context.Context, repo string, options gitCommandOptions, output io.Writer, allowEmpty bool, args ...string) error {
	timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := newWorkspaceGitCommand(timeout, repo, options, args...)
	stderr := &comparisonOutput{limit: 64 * 1024}
	command.Stdout, command.Stderr = output, stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return status_error.OperationCanceled
		}
		var exit *exec.ExitError
		if allowEmpty && timeout.Err() == nil && errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil
		}
		logx.Errorf("workspace git comparison failed: operation=%q error=%v stderr=%q", args[0], err, stderr.buffer.String())
		return status_error.WorkspaceGitFailed
	}
	return nil
}
