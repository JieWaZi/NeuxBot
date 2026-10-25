package workspacefiles

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// GitEntry 保留暂存与未暂存的独立状态。
type GitEntry struct {
	// Path 是仓库相对路径。
	Path string `json:"path"`
	// XY 是 porcelain 双列状态。
	XY string `json:"xy"`
	// StagedAddedLines 是暂存新增行数。
	StagedAddedLines int `json:"stagedAddedLines"`
	// StagedDeletedLines 是暂存删除行数。
	StagedDeletedLines int `json:"stagedDeletedLines"`
	// UnstagedAddedLines 是工作区新增行数。
	UnstagedAddedLines int `json:"unstagedAddedLines"`
	// UnstagedDeletedLines 是工作区删除行数。
	UnstagedDeletedLines int `json:"unstagedDeletedLines"`
	// LineCountUnavailable 表明二进制等条目不具有可靠行数。
	LineCountUnavailable bool `json:"lineCountUnavailable,omitempty"`
}

// GitStatus 是选中仓库的工作树快照。
type GitStatus struct {
	// IsRepo 表明授权目录内存在仓库。
	IsRepo bool `json:"isRepo"`
	// Branch 是当前分支或分离头说明。
	Branch string `json:"branch,omitempty"`
	// Entries 是逐路径变更状态。
	Entries []GitEntry `json:"entries"`
	// Root 是本次选中的仓库根。
	Root string `json:"root,omitempty"`
	// Repositories 是范围内可供选择的仓库根。
	Repositories []string `json:"repositories"`
}

// GitLogEntry 描述一条提交日志。
type GitLogEntry struct {
	// Hash 是提交短标识。
	Hash string `json:"hash"`
	// HashFull 是完整提交标识。
	HashFull string `json:"hashFull"`
	// Subject 是提交首行说明。
	Subject string `json:"subject"`
	// Author 是提交作者。
	Author string `json:"author"`
	// Date 是提交时间。
	Date string `json:"date"`
	// Refs 是指向该提交的引用。
	Refs string `json:"refs"`
}

type boundedGitOutput struct {
	// buffer 保存有界的 Git 输出。
	buffer bytes.Buffer
	// limit 限制单次命令输出字节数。
	limit int
}

func (w *boundedGitOutput) Write(value []byte) (int, error) {
	if w.buffer.Len()+len(value) > w.limit {
		return 0, io.ErrShortBuffer
	}
	return w.buffer.Write(value)
}
func gitCommand(ctx context.Context, root string, args ...string) (string, error) {
	return gitCommandExit(ctx, root, false, args...)
}

func gitCommandExit(ctx context.Context, root string, diffExit bool, args ...string) (string, error) {
	timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(timeout, "git", append([]string{"-C", root, "--no-pager"}, args...)...)
	output := &boundedGitOutput{limit: 16 * 1024 * 1024}
	stderr := &boundedGitOutput{limit: 64 * 1024}
	command.Stdout = output
	command.Stderr = stderr
	// 宿主遗留的 Git 路径变量不能覆盖已校验的仓库与索引。
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if diffExit && errors.As(err, &exit) && exit.ExitCode() == 1 && timeout.Err() == nil {
			return output.buffer.String(), nil
		}
		logx.Errorf("workspace git operation failed: operation=%q error=%v stderr=%q", args[0], err, stderr.buffer.String())
		return "", status_error.WorkspaceGitFailed
	}
	return output.buffer.String(), nil
}

func (m *Manager) repository(ctx context.Context, subject, id, root, selected string) (string, []string, error) {
	_, target, err := m.resolve(subject, id, root, "", false)
	if err != nil {
		return "", nil, err
	}
	scope, err := m.scope(subject, id)
	if err != nil {
		return "", nil, err
	}
	searchRoots := []string{target}
	if target == scope.Root {
		searchRoots = append([]string{}, scope.roots...)
	}
	roots := []string{}
	discovered := map[string]bool{}
	visited := 0
	for _, searchRoot := range searchRoots {
		err = filepath.WalkDir(searchRoot, func(name string, entry os.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() {
				return nil
			}
			visited++
			if visited > 10000 {
				return status_error.WorkspaceResourceTooLarge
			}
			relative, _ := filepath.Rel(searchRoot, name)
			if accountPath(name) {
				return filepath.SkipDir
			}
			if relative != "." && (entry.Name() == ".git" || entry.Name() == "node_modules" || strings.Count(relative, string(filepath.Separator)) > 4) {
				return filepath.SkipDir
			}
			if _, e := os.Lstat(filepath.Join(name, ".git")); e == nil {
				real, e := canonicalDirectory(name)
				if e != nil {
					return e
				}
				if within(searchRoot, real) && !discovered[real] {
					roots = append(roots, real)
					discovered[real] = true
				}
			}
			return nil
		})
		if err != nil {
			if err == status_error.WorkspaceResourceTooLarge || ctx.Err() != nil {
				return "", nil, err
			}
			return "", nil, fileError("discover-repositories", err)
		}
	}
	sort.Strings(roots)
	if selected == "" {
		if len(roots) == 0 {
			return "", roots, nil
		}
		selected = roots[0]
	}
	canonical, err := canonicalDirectory(selected)
	if err != nil {
		return "", nil, err
	}
	found := false
	for _, candidate := range roots {
		if candidate == canonical {
			found = true
			break
		}
	}
	if !found {
		return "", nil, status_error.WorkspacePathInvalid
	}
	top, err := gitCommand(ctx, canonical, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil, err
	}
	actual, err := canonicalDirectory(strings.TrimSpace(top))
	if err != nil {
		return "", nil, err
	}
	if actual != canonical {
		return "", nil, status_error.WorkspacePathInvalid
	}
	return canonical, roots, nil
}

type lineChange struct {
	// added 保存新增行数。
	added int
	// deleted 保存删除行数。
	deleted int
	// unavailable 表明行数无法可靠计算。
	unavailable bool
}

func parseNumstat(output string) map[string]lineChange {
	result := map[string]lineChange{}
	parts := strings.Split(output, "\x00")
	for i := 0; i < len(parts); i++ {
		fields := strings.SplitN(parts[i], "\t", 3)
		if len(fields) != 3 {
			continue
		}
		name := fields[2]
		if name == "" && i+2 < len(parts) {
			i += 2
			name = parts[i]
		}
		added, aerr := strconv.Atoi(fields[0])
		deleted, derr := strconv.Atoi(fields[1])
		result[name] = lineChange{added: added, deleted: deleted, unavailable: aerr != nil || derr != nil}
	}
	return result
}

// Status 返回冻结范围内选中仓库的有界 Git 状态。
func (m *Manager) Status(ctx context.Context, subject, id, root, selected string) (GitStatus, error) {
	repo, repositories, err := m.repository(ctx, subject, id, root, selected)
	if err != nil {
		return GitStatus{}, err
	}
	result := GitStatus{IsRepo: repo != "", Root: repo, Repositories: repositories, Entries: []GitEntry{}}
	if repo == "" {
		return result, nil
	}
	output, err := gitCommand(ctx, repo, "status", "--porcelain=v1", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return GitStatus{}, err
	}
	staged, err := gitCommand(ctx, repo, "diff", "--cached", "--numstat", "-z", "--no-ext-diff")
	if err != nil {
		return GitStatus{}, err
	}
	unstaged, err := gitCommand(ctx, repo, "diff", "--numstat", "-z", "--no-ext-diff")
	if err != nil {
		return GitStatus{}, err
	}
	stagedLines := parseNumstat(staged)
	unstagedLines := parseNumstat(unstaged)
	parts := strings.Split(output, "\x00")
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if strings.HasPrefix(part, "## ") {
			result.Branch = strings.SplitN(strings.TrimPrefix(part, "## "), "...", 2)[0]
			result.Branch = strings.TrimPrefix(strings.TrimPrefix(result.Branch, "No commits yet on "), "Initial commit on ")
			continue
		}
		if len(part) < 4 {
			continue
		}
		xy, name := part[:2], part[3:]
		if strings.ContainsAny(xy, "RC") && i+1 < len(parts) {
			i++
		}
		a, b := stagedLines[name], unstagedLines[name]
		entry := GitEntry{Path: name, XY: xy, StagedAddedLines: a.added, StagedDeletedLines: a.deleted, UnstagedAddedLines: b.added, UnstagedDeletedLines: b.deleted, LineCountUnavailable: a.unavailable || b.unavailable}
		if xy == "??" {
			entry.LineCountUnavailable = true
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func validGitPath(value string) bool {
	return value != "" && safeRelative(value) && value != "." && !accountPath(value)
}

var commitRevision = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// Diff 返回一个已授权路径的暂存或工作区差异。
func (m *Manager) Diff(ctx context.Context, subject, id, root, selected, relative string, staged bool) (string, error) {
	if !validGitPath(relative) {
		return "", status_error.WorkspacePathInvalid
	}
	repo, _, err := m.repository(ctx, subject, id, root, selected)
	if err != nil {
		return "", err
	}
	if repo == "" {
		return "", status_error.WorkspacePathInvalid
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", ":(literal)"+relative)
	patch, err := gitCommand(ctx, repo, args...)
	if err != nil || patch != "" || staged {
		return patch, err
	}
	untracked, err := gitCommand(ctx, repo, "ls-files", "--others", "--exclude-standard", "-z", "--", ":(literal)"+relative)
	if err != nil {
		return "", err
	}
	found := false
	for _, name := range strings.Split(untracked, "\x00") {
		if name == relative {
			found = true
		}
	}
	if !found {
		return patch, nil
	}
	// no-index 会读取工作树文件；先确认符号链接没有越过仓库边界。
	canonical, err := filepath.EvalSymlinks(filepath.Join(repo, filepath.FromSlash(relative)))
	if err != nil || !within(repo, canonical) {
		return "", status_error.WorkspacePathInvalid
	}
	return gitCommandExit(ctx, repo, true, "diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "--", os.DevNull, relative)
}

// Log 返回有界的提交历史。
func (m *Manager) Log(ctx context.Context, subject, id, root, selected string, count, skip int) ([]GitLogEntry, error) {
	if count <= 0 || count > 200 {
		count = 20
	}
	if skip < 0 || skip > 100000 {
		return nil, status_error.WorkspacePathInvalid
	}
	repo, _, err := m.repository(ctx, subject, id, root, selected)
	if err != nil {
		return nil, err
	}
	if repo == "" {
		return []GitLogEntry{}, nil
	}
	// 尚无提交的仓库返回空历史，其他 Git 故障仍作为失败返回。
	refs, err := gitCommand(ctx, repo, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return nil, err
	}
	_ = refs
	heads, err := gitCommand(ctx, repo, "for-each-ref", "--count=1", "--format=%(objectname)")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(heads) == "" {
		return []GitLogEntry{}, nil
	}
	output, err := gitCommand(ctx, repo, "log", "--no-color", "--max-count="+strconv.Itoa(count), "--skip="+strconv.Itoa(skip), "--format=%h%x1f%H%x1f%s%x1f%an%x1f%aI%x1f%D%x1e")
	if err != nil {
		return nil, err
	}
	result := []GitLogEntry{}
	for _, record := range strings.Split(output, "\x1e") {
		fields := strings.Split(strings.TrimLeft(record, "\n"), "\x1f")
		if len(fields) != 6 {
			continue
		}
		result = append(result, GitLogEntry{Hash: fields[0], HashFull: fields[1], Subject: fields[2], Author: fields[3], Date: fields[4], Refs: fields[5]})
	}
	return result, nil
}

// Show 返回指定提交中受限路径的原始文本。
func (m *Manager) Show(ctx context.Context, subject, id, root, selected, revision, relative string) (string, error) {
	if !validGitPath(relative) || (revision != "HEAD" && !commitRevision.MatchString(revision)) {
		return "", status_error.WorkspacePathInvalid
	}
	repo, _, err := m.repository(ctx, subject, id, root, selected)
	if err != nil {
		return "", err
	}
	if repo == "" {
		return "", status_error.WorkspacePathInvalid
	}
	names, err := gitCommand(ctx, repo, "ls-tree", "-r", "-z", "--name-only", revision, "--", ":(literal)"+relative)
	if err != nil {
		return "", err
	}
	if names == "" {
		return "", nil
	}
	return gitCommand(ctx, repo, "show", "--no-ext-diff", revision+":"+relative)
}

// CommitDiff 返回完整提交的差异，不接受任意 revision 表达式。
func (m *Manager) CommitDiff(ctx context.Context, subject, id, root, selected, hash string) (string, error) {
	if !commitRevision.MatchString(hash) {
		return "", status_error.WorkspacePathInvalid
	}
	repo, _, err := m.repository(ctx, subject, id, root, selected)
	if err != nil {
		return "", err
	}
	if repo == "" {
		return "", status_error.WorkspacePathInvalid
	}
	return gitCommand(ctx, repo, "show", "--no-color", "--format=", "-m", "--first-parent", "--no-ext-diff", "--no-textconv", hash, "--")
}

// ChangeIndex 修改暂存区或丢弃一个已跟踪文件的未暂存改动。
func (m *Manager) ChangeIndex(ctx context.Context, subject, id, root, selected, relative, action string) error {
	if !validGitPath(relative) {
		return status_error.WorkspacePathInvalid
	}
	m.mutations.Lock()
	defer m.mutations.Unlock()
	snapshot, err := m.Status(ctx, subject, id, root, selected)
	if err != nil {
		return err
	}
	var entry *GitEntry
	for i := range snapshot.Entries {
		if snapshot.Entries[i].Path == relative {
			entry = &snapshot.Entries[i]
			break
		}
	}
	if entry == nil {
		return status_error.WorkspaceResourceConflict
	}
	staged := entry.XY[0] != ' ' && entry.XY[0] != '?'
	unstaged := entry.XY == "??" || (entry.XY[1] != ' ' && entry.XY[1] != '?')
	var args []string
	switch action {
	case "stage":
		if !unstaged {
			return status_error.WorkspaceResourceConflict
		}
		args = []string{"add", "-A", "--", ":(literal)" + relative}
	case "unstage":
		if !staged {
			return status_error.WorkspaceResourceConflict
		}
		args = []string{"reset", "-q", "--", ":(literal)" + relative}
	case "discard":
		if !unstaged || entry.XY == "??" {
			return status_error.WorkspaceResourceConflict
		}
		args = []string{"restore", "--worktree", "--", ":(literal)" + relative}
	default:
		return status_error.WorkspacePathInvalid
	}
	_, err = gitCommand(ctx, snapshot.Root, args...)
	return err
}

// Commit 只提交已暂存内容，调用方不得自动重试未知结果。
func (m *Manager) Commit(ctx context.Context, subject, id, root, selected, message string) error {
	message = strings.TrimSpace(message)
	if message == "" || len(message) > 4096 {
		return status_error.WorkspacePathInvalid
	}
	m.mutations.Lock()
	defer m.mutations.Unlock()
	snapshot, err := m.Status(ctx, subject, id, root, selected)
	if err != nil {
		return err
	}
	staged := false
	for _, entry := range snapshot.Entries {
		if entry.XY[0] != ' ' && entry.XY[0] != '?' {
			staged = true
			break
		}
	}
	if !staged {
		return status_error.WorkspaceResourceConflict
	}
	_, err = gitCommand(ctx, snapshot.Root, "commit", "-m", message)
	return err
}

// GitBranch 描述本地或远程分支。
type GitBranch struct {
	// Name 是分支短名。
	Name string `json:"name"`
	// IsCurrent 标识当前检出分支。
	IsCurrent bool `json:"isCurrent"`
	// IsRemote 标识远程跟踪分支。
	IsRemote bool `json:"isRemote"`
}

// GitBranches 返回分支选择器所需的仓库信息。
type GitBranches struct {
	// IsGitRepo 表明范围内存在仓库。
	IsGitRepo bool `json:"isGitRepo"`
	// GitRoot 是选中仓库的真实根目录。
	GitRoot string `json:"gitRoot,omitempty"`
	// Branches 是可选择分支。
	Branches []GitBranch `json:"branches"`
}

// CheckoutConflict 描述可能被分支切换覆盖的本地文件。
type CheckoutConflict struct {
	// Path 是仓库相对路径。
	Path string `json:"path"`
	// AddedLines 是本地新增行数。
	AddedLines int `json:"addedLines"`
	// DeletedLines 是本地删除行数。
	DeletedLines int `json:"deletedLines"`
}

// CheckoutPreview 是分支切换前的冲突预检。
type CheckoutPreview struct {
	// CanCheckout 表明当前快照可以直接切换。
	CanCheckout bool `json:"canCheckout"`
	// Conflicts 是阻止直接切换的本地改动。
	Conflicts []CheckoutConflict `json:"conflicts"`
}

// Branches 列出授权仓库的本地与远程跟踪分支。
func (m *Manager) Branches(ctx context.Context, subject, id, root, selected string) (GitBranches, error) {
	repo, _, err := m.repository(ctx, subject, id, root, selected)
	if err != nil {
		return GitBranches{}, err
	}
	result := GitBranches{IsGitRepo: repo != "", GitRoot: repo, Branches: []GitBranch{}}
	if repo == "" {
		return result, nil
	}
	output, err := gitCommand(ctx, repo, "for-each-ref", "--format=%(refname:short)%09%(refname)%09%(HEAD)", "refs/heads", "refs/remotes")
	if err != nil {
		return GitBranches{}, err
	}
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || strings.HasSuffix(fields[0], "/HEAD") {
			continue
		}
		result.Branches = append(result.Branches, GitBranch{Name: fields[0], IsCurrent: fields[2] == "*", IsRemote: strings.HasPrefix(fields[1], "refs/remotes/")})
	}
	sort.Slice(result.Branches, func(i, j int) bool {
		a, b := result.Branches[i], result.Branches[j]
		if a.IsCurrent != b.IsCurrent {
			return a.IsCurrent
		}
		if a.IsRemote != b.IsRemote {
			return !a.IsRemote
		}
		return a.Name < b.Name
	})
	return result, nil
}

// PreviewCheckout 比较目标分支与本地变化，真正切换仍由 Git 再次拒绝覆盖竞态。
func (m *Manager) PreviewCheckout(ctx context.Context, subject, id, root, selected, branch string) (CheckoutPreview, error) {
	branches, err := m.Branches(ctx, subject, id, root, selected)
	if err != nil {
		return CheckoutPreview{}, err
	}
	found := false
	for _, item := range branches.Branches {
		if item.Name == branch {
			found = true
			break
		}
	}
	if !found {
		return CheckoutPreview{}, status_error.WorkspacePathInvalid
	}
	result := CheckoutPreview{CanCheckout: true, Conflicts: []CheckoutConflict{}}
	snapshot, err := m.Status(ctx, subject, id, root, branches.GitRoot)
	if err != nil {
		return CheckoutPreview{}, err
	}
	changed, err := gitCommand(ctx, branches.GitRoot, "diff", "--name-only", "-z", "HEAD", branch, "--")
	if err != nil {
		return CheckoutPreview{}, err
	}
	paths := map[string]bool{}
	for _, name := range strings.Split(changed, "\x00") {
		paths[name] = true
	}
	targetFiles, err := gitCommand(ctx, branches.GitRoot, "ls-tree", "-r", "--name-only", "-z", branch)
	if err != nil {
		return CheckoutPreview{}, err
	}
	target := map[string]bool{}
	for _, name := range strings.Split(targetFiles, "\x00") {
		target[name] = true
	}
	for _, entry := range snapshot.Entries {
		if !paths[entry.Path] && !(entry.XY == "??" && target[entry.Path]) {
			continue
		}
		result.CanCheckout = false
		result.Conflicts = append(result.Conflicts, CheckoutConflict{Path: entry.Path, AddedLines: entry.StagedAddedLines + entry.UnstagedAddedLines, DeletedLines: entry.StagedDeletedLines + entry.UnstagedDeletedLines})
	}
	return result, nil
}

// Checkout 按显式用户选择切换分支，可先提交本地修改；不会强制覆盖工作树。
func (m *Manager) Checkout(ctx context.Context, subject, id, root, selected, branch, message string) (string, error) {
	m.mutations.Lock()
	defer m.mutations.Unlock()
	branches, err := m.Branches(ctx, subject, id, root, selected)
	if err != nil {
		return "", err
	}
	var chosen *GitBranch
	for i := range branches.Branches {
		if branches.Branches[i].Name == branch {
			chosen = &branches.Branches[i]
			break
		}
	}
	if chosen == nil {
		return "", status_error.WorkspacePathInvalid
	}
	message = strings.TrimSpace(message)
	if message != "" {
		if len(message) > 4096 {
			return "", status_error.WorkspacePathInvalid
		}
		if _, err = gitCommand(ctx, branches.GitRoot, "add", "-A"); err != nil {
			return "", err
		}
		if _, err = gitCommand(ctx, branches.GitRoot, "commit", "-m", message); err != nil {
			return "", err
		}
	} else {
		preview, err := m.PreviewCheckout(ctx, subject, id, root, selected, branch)
		if err != nil {
			return "", err
		}
		if !preview.CanCheckout {
			return "", status_error.WorkspaceResourceConflict
		}
	}
	args := []string{"switch", "--quiet"}
	if chosen.IsRemote {
		local := strings.SplitN(branch, "/", 2)
		if len(local) != 2 {
			return "", status_error.WorkspacePathInvalid
		}
		exists := false
		for _, item := range branches.Branches {
			if !item.IsRemote && item.Name == local[1] {
				exists = true
				break
			}
		}
		if exists {
			args = append(args, local[1])
		} else {
			args = append(args, "--track", branch)
		}
	} else {
		args = append(args, branch)
	}
	if _, err = gitCommand(ctx, branches.GitRoot, args...); err != nil {
		return "", err
	}
	current, err := gitCommand(ctx, branches.GitRoot, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(current), err
}
