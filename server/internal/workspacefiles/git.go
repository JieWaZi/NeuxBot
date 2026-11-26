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
	// oldPath 是已识别重命名的原路径，用于撤销完整的暂存项。
	oldPath string
}

// GitStatus 是选中仓库的工作树快照。
type GitStatus struct {
	// IsRepo 表明授权目录内存在仓库。
	IsRepo bool `json:"isRepo"`
	// Branch 是当前分支或分离头说明。
	Branch string `json:"branch,omitempty"`
	// HEAD 提交标识
	Head string `json:"head"`
	// 上游分支
	Upstream string `json:"upstream"`
	// 推送远端
	Remote string `json:"remote"`
	// 可选远端
	Remotes []string `json:"remotes"`
	// GitHub 推送远端
	GitHub bool `json:"github"`
	// 远端默认分支
	DefaultBranch string `json:"defaultBranch,omitempty"`
	// 领先提交数
	Ahead int `json:"ahead"`
	// 相对远端默认分支的独有提交数
	AheadOfDefault int `json:"aheadOfDefault,omitempty"`
	// 落后提交数
	Behind int `json:"behind"`
	// 暂存版本
	Index string `json:"index"`
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
	// keepPrefix 允许读取有界前缀，剩余内容在命令退出前丢弃。
	keepPrefix bool
	// truncated 标记实际输出超过上限。
	truncated bool
}

func (w *boundedGitOutput) Write(value []byte) (int, error) {
	if w.buffer.Len()+len(value) > w.limit {
		if !w.keepPrefix {
			return 0, io.ErrShortBuffer
		}
		w.truncated = true
		w.buffer.Write(value[:w.limit-w.buffer.Len()])
		return len(value), nil
	}
	return w.buffer.Write(value)
}
func gitCommand(ctx context.Context, root string, args ...string) (string, error) {
	return gitCommandExit(ctx, root, false, args...)
}

func gitCommandExit(ctx context.Context, root string, diffExit bool, args ...string) (string, error) {
	output := &boundedGitOutput{limit: 16 * 1024 * 1024}
	if err := runGitCommand(ctx, root, diffExit, output, args...); err != nil {
		return "", err
	}
	return output.buffer.String(), nil
}

func runGitCommand(ctx context.Context, root string, diffExit bool, output *boundedGitOutput, args ...string) error {
	return runGitWithOptions(ctx, root, diffExit, output, gitCommandOptions{}, args...)
}

type gitCommandOptions struct {
	// index 是由宿主创建的临时索引路径。
	index string
	// objects 指定可选的临时对象库。
	objects string
	// alternates 指定临时对象库能读取的现有对象库。
	alternates string
	// input 是由宿主编码的命令标准输入。
	input string
}

func gitCommandWithOptions(ctx context.Context, root string, options gitCommandOptions, args ...string) (string, error) {
	output := &boundedGitOutput{limit: 16 * 1024 * 1024}
	if err := runGitWithOptions(ctx, root, false, output, options, args...); err != nil {
		return "", err
	}
	return output.buffer.String(), nil
}

func runGitWithOptions(ctx context.Context, root string, diffExit bool, output *boundedGitOutput, options gitCommandOptions, args ...string) error {
	timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := newWorkspaceGitCommand(timeout, root, options, args...)
	stderr := &boundedGitOutput{limit: 64 * 1024}
	command.Stdout = output
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return status_error.OperationCanceled
		}
		var exit *exec.ExitError
		if diffExit && errors.As(err, &exit) && exit.ExitCode() == 1 && timeout.Err() == nil {
			return nil
		}
		if args[0] == "push" {
			// 远端失败可能回显带凭据的 URL。
			logx.Errorf("workspace git operation failed: operation=%q error=%v", args[0], err)
		} else {
			logx.Errorf("workspace git operation failed: operation=%q error=%v stderr=%q", args[0], err, stderr.buffer.String())
		}
		return status_error.WorkspaceGitFailed
	}
	return nil
}

func newWorkspaceGitCommand(ctx context.Context, root string, options gitCommandOptions, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root, "--no-pager"}, args...)...)
	command.WaitDelay = time.Second
	// 宿主遗留的 Git 路径变量不能覆盖已校验的仓库与索引。
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	if options.index != "" {
		command.Env = append(command.Env, "GIT_INDEX_FILE="+options.index)
	}
	if options.objects != "" {
		command.Env = append(command.Env, "GIT_OBJECT_DIRECTORY="+options.objects, "GIT_ALTERNATE_OBJECT_DIRECTORIES="+options.alternates)
	}
	if options.input != "" {
		command.Stdin = strings.NewReader(options.input)
	}
	return command
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
			if !scope.allows(name) {
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
				containsPrivate := scope.chatRoot != "" && within(real, filepath.Dir(scope.chatRoot))
				if within(searchRoot, real) && !containsPrivate && !discovered[real] {
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
	result := GitStatus{IsRepo: repo != "", Root: repo, Repositories: repositories, Entries: []GitEntry{}, Remotes: []string{}}
	if repo == "" {
		return result, nil
	}
	output, err := gitCommand(ctx, repo, "status", "--porcelain=v2", "-z", "--branch", "--ahead-behind", "--untracked-files=all")
	if err != nil {
		return GitStatus{}, err
	}
	refs := parseGitRefs(output)
	result.Branch, result.Head, result.Upstream = refs.branch, refs.head, refs.upstream
	result.Ahead, result.Behind = refs.ahead, refs.behind
	result.Index = refs.index
	remotes, err := gitRemoteNames(ctx, repo)
	if err != nil {
		return GitStatus{}, err
	}
	result.Remotes = append(result.Remotes, remotes...)
	result.Remote, err = preferredGitRemote(ctx, repo, refs, remotes)
	if err != nil {
		return GitStatus{}, err
	}
	if result.Remote != "" {
		_, githubErr := githubPushRepository(ctx, repo, result.Remote)
		result.GitHub = githubErr == nil
		result.DefaultBranch, err = gitKnownDefaultBranch(ctx, repo, result.Remote)
		if err != nil {
			return GitStatus{}, err
		}
		if refs.head != "" && result.DefaultBranch != "" && refs.branch != result.DefaultBranch {
			base := "refs/remotes/" + result.Remote + "/" + result.DefaultBranch
			count, err := gitCommand(ctx, repo, "rev-list", "--count", base+".."+refs.head, "--")
			if err != nil {
				return GitStatus{}, err
			}
			result.AheadOfDefault, _ = strconv.Atoi(strings.TrimSpace(count))
		}
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
		xy, name := parseGitStatusEntry(part)
		if xy == "" {
			continue
		}
		var oldPath string
		if strings.HasPrefix(part, "2 ") && i+1 < len(parts) {
			i++
			oldPath = parts[i]
		}
		a, b := stagedLines[name], unstagedLines[name]
		entry := GitEntry{Path: name, XY: xy, StagedAddedLines: a.added, StagedDeletedLines: a.deleted, UnstagedAddedLines: b.added, UnstagedDeletedLines: b.deleted, LineCountUnavailable: a.unavailable || b.unavailable, oldPath: oldPath}
		if xy == "??" {
			entry.LineCountUnavailable = true
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func parseGitStatusEntry(line string) (string, string) {
	if strings.HasPrefix(line, "? ") {
		return "??", line[2:]
	}
	var count int
	switch {
	case strings.HasPrefix(line, "1 "):
		count = 9
	case strings.HasPrefix(line, "2 "):
		count = 10
	case strings.HasPrefix(line, "u "):
		count = 11
	default:
		return "", ""
	}
	fields := strings.SplitN(line, " ", count)
	if len(fields) != count || len(fields[1]) != 2 {
		return "", ""
	}
	return strings.ReplaceAll(fields[1], ".", " "), fields[count-1]
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

// Log 返回当前 HEAD 的有界提交历史。
func (m *Manager) Log(ctx context.Context, subject, id, root, selected string, count, skip int) ([]GitLogEntry, error) {
	return m.gitLog(ctx, subject, id, root, selected, "", count, skip)
}

// LogComparison 返回当前分支在比较基准共同祖先之后的有界提交历史。
func (m *Manager) LogComparison(ctx context.Context, subject, id, root, selected, base string, count, skip int) ([]GitLogEntry, error) {
	if base == "" {
		base = gitComparisonAutoBase
	}
	return m.gitLog(ctx, subject, id, root, selected, base, count, skip)
}

func (m *Manager) gitLog(ctx context.Context, subject, id, root, selected, base string, count, skip int) ([]GitLogEntry, error) {
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
	var revision string
	if base == "" {
		head, _, err := comparisonGit(ctx, repo, gitCommandOptions{}, 512, true, "rev-parse", "--verify", "--quiet", "HEAD")
		if err != nil {
			return nil, err
		}
		revision = strings.TrimSpace(head)
	} else {
		if base == gitComparisonAutoBase {
			base = ""
		} else if strings.HasPrefix(base, ":") {
			return nil, status_error.WorkspacePathInvalid
		}
		span, err := comparisonRange(ctx, repo, base)
		if err != nil {
			return nil, err
		}
		if span.head != "" {
			revision = span.args[0] + ".." + span.head
		}
	}
	if revision == "" {
		return []GitLogEntry{}, nil
	}
	output, err := gitCommand(ctx, repo, "log", "--no-color", "--no-notes", "--no-show-signature", "--max-count="+strconv.Itoa(count), "--skip="+strconv.Itoa(skip), "--format=%h%x1f%H%x1f%s%x1f%an%x1f%aI%x1f%D%x1e", revision, "--")
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

// GitIndexRequest 是暂存区修改请求。
type GitIndexRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 授权根目录
	Root string `json:"root,omitempty"`
	// 仓库目录
	Repository string `json:"repository,omitempty"`
	// 相对路径
	RelativePath string `json:"relativePath" validate:"required"`
	// 预期分支
	ExpectedBranch *string `json:"expectedBranch,omitempty"`
	// 预期提交
	ExpectedHead *string `json:"expectedHead,omitempty"`
	// 预期暂存版本
	ExpectedIndex *string `json:"expectedIndex,omitempty"`
	// 确认丢弃的未暂存补丁版本
	ExpectedWorktreeVersion string `json:"expectedWorktreeVersion,omitempty"`
	// 比较时忽略空白
	IgnoreWhitespace bool `json:"ignoreWhitespace,omitempty"`
}

// ChangeIndex 核对用户确认时的仓库快照后修改单文件。
func (m *Manager) ChangeIndex(ctx context.Context, subject string, request GitIndexRequest, action string) error {
	relative := request.RelativePath
	if !validGitPath(relative) {
		return status_error.WorkspacePathInvalid
	}
	m.mutations.Lock()
	defer m.mutations.Unlock()
	snapshot, err := m.Status(ctx, subject, request.ScopeID, request.Root, request.Repository)
	if err != nil {
		return err
	}
	if request.ExpectedBranch != nil && *request.ExpectedBranch != snapshot.Branch ||
		request.ExpectedHead != nil && *request.ExpectedHead != snapshot.Head ||
		request.ExpectedIndex != nil && *request.ExpectedIndex != snapshot.Index {
		return status_error.WorkspaceResourceConflict
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
		if entry.XY[0] == 'R' && validGitPath(entry.oldPath) {
			args = append(args, ":(literal)"+entry.oldPath)
		}
	case "discard":
		if !unstaged || entry.XY == "??" || request.ExpectedWorktreeVersion == "" {
			return status_error.WorkspaceResourceConflict
		}
		branch := snapshot.Branch
		if branch == detachedGitBranch {
			branch = "HEAD"
		}
		_, version, _, err := worktreeComparisonPatch(ctx, snapshot.Root, relative, branch, snapshot.Head, request.IgnoreWhitespace)
		if err != nil {
			return err
		}
		if version != request.ExpectedWorktreeVersion {
			return status_error.WorkspaceResourceConflict
		}
		args = []string{"restore", "--worktree", "--no-recurse-submodules", "--", comparisonPathspec(relative)}
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
	// ref 是用于实际 Git 操作的完整引用。
	ref string
}

// GitBranches 返回分支选择器所需的仓库信息。
type GitBranches struct {
	// IsGitRepo 表明范围内存在仓库。
	IsGitRepo bool `json:"isGitRepo"`
	// GitRoot 是选中仓库的真实根目录。
	GitRoot string `json:"gitRoot,omitempty"`
	// Branches 是可选择分支。
	Branches []GitBranch `json:"branches"`
	// HasMore 表示还有匹配分支。
	HasMore bool `json:"hasMore,omitempty"`
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

const (
	gitBranchQueryMaxBytes = 256
	gitBranchSearchLimit   = 100
	gitBranchMaxLimit      = 200
)

// Branches 列出授权仓库的本地与远程跟踪分支，可按名称筛选与限制结果数量。
func (m *Manager) Branches(ctx context.Context, subject, id, root, selected, query string, limit *int) (GitBranches, error) {
	if len(query) > gitBranchQueryMaxBytes || strings.ContainsRune(query, '\x00') {
		return GitBranches{}, status_error.BadRequest
	}
	query = strings.ToLower(strings.TrimSpace(query))
	maxResults := 0
	if limit != nil {
		if *limit < 1 || *limit > gitBranchMaxLimit {
			return GitBranches{}, status_error.BadRequest
		}
		maxResults = *limit
	} else if query != "" {
		maxResults = gitBranchSearchLimit
	}
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
		if query != "" && !strings.Contains(strings.ToLower(fields[0]), query) {
			continue
		}
		result.Branches = append(result.Branches, GitBranch{Name: fields[0], IsCurrent: fields[2] == "*", IsRemote: strings.HasPrefix(fields[1], "refs/remotes/"), ref: fields[1]})
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
	if maxResults > 0 && len(result.Branches) > maxResults {
		result.Branches = result.Branches[:maxResults]
		result.HasMore = true
	}
	return result, nil
}

// PreviewCheckout 比较目标分支与本地变化，真正切换仍由 Git 再次拒绝覆盖竞态。
func (m *Manager) PreviewCheckout(ctx context.Context, subject, id, root, selected, branch string) (CheckoutPreview, error) {
	branches, err := m.Branches(ctx, subject, id, root, selected, "", nil)
	if err != nil {
		return CheckoutPreview{}, err
	}
	for _, item := range branches.Branches {
		if item.Name == branch {
			return m.previewCheckout(ctx, subject, id, root, branches.GitRoot, item.ref)
		}
	}
	return CheckoutPreview{}, status_error.WorkspacePathInvalid
}

func (m *Manager) previewCheckout(ctx context.Context, subject, id, root, repo, ref string) (CheckoutPreview, error) {
	result := CheckoutPreview{CanCheckout: true, Conflicts: []CheckoutConflict{}}
	snapshot, err := m.Status(ctx, subject, id, root, repo)
	if err != nil {
		return CheckoutPreview{}, err
	}
	changed, err := gitCommand(ctx, repo, "diff", "--name-only", "-z", "HEAD", ref, "--")
	if err != nil {
		return CheckoutPreview{}, err
	}
	paths := map[string]bool{}
	for _, name := range strings.Split(changed, "\x00") {
		paths[name] = true
	}
	targetFiles, err := gitCommand(ctx, repo, "ls-tree", "-r", "--name-only", "-z", ref)
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

// Checkout 安全切换分支；旧复合提交参数需要改用独立提交入口。
func (m *Manager) Checkout(ctx context.Context, subject, id, root, selected, branch, message string) (string, error) {
	if strings.TrimSpace(message) != "" {
		return "", status_error.WorkspaceResourceConflict
	}
	m.mutations.Lock()
	defer m.mutations.Unlock()
	return m.checkout(ctx, subject, id, root, selected, branch)
}

func (m *Manager) checkout(ctx context.Context, subject, id, root, selected, branch string) (string, error) {
	branches, err := m.Branches(ctx, subject, id, root, selected, "", nil)
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
	preview, err := m.previewCheckout(ctx, subject, id, root, branches.GitRoot, chosen.ref)
	if err != nil {
		return "", err
	}
	if !preview.CanCheckout {
		return "", status_error.WorkspaceResourceConflict
	}
	args := []string{"switch", "--quiet", "--no-overwrite-ignore", "--no-recurse-submodules"}
	if chosen.IsRemote {
		remote, local, found := strings.Cut(strings.TrimPrefix(chosen.ref, "refs/remotes/"), "/")
		if !found {
			return "", status_error.WorkspaceGitTargetInvalid
		}
		exists := false
		for _, item := range branches.Branches {
			if item.ref == "refs/heads/"+local {
				exists = true
				break
			}
		}
		if exists {
			trackedRemote, trackedBranch, err := gitUpstreamDestination(ctx, branches.GitRoot, local)
			if err != nil {
				return "", err
			}
			if trackedRemote != remote || trackedBranch != local {
				return "", status_error.WorkspaceResourceConflict
			}
			args = append(args, "--no-guess", "--", local)
		} else {
			args = append(args, "--track", "--", chosen.ref)
		}
	} else {
		args = append(args, "--no-guess", "--", strings.TrimPrefix(chosen.ref, "refs/heads/"))
	}
	if _, err = gitCommand(ctx, branches.GitRoot, args...); err != nil {
		return "", err
	}
	current, err := gitCommand(ctx, branches.GitRoot, "symbolic-ref", "HEAD")
	return strings.TrimPrefix(strings.TrimSuffix(current, "\n"), "refs/heads/"), err
}
