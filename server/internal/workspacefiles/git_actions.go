package workspacefiles

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const (
	gitCommitMessageLimit = 4096
	gitMessagePatchLimit  = 24 * 1024
	gitExcludeAccountRoot = ":(exclude,icase,glob)**/runtime/accounts"
	gitExcludeAccounts    = ":(exclude,icase,glob)**/runtime/accounts/**"
)

// GitActionRequest 是用户确认的仓库操作。
type GitActionRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 授权根目录
	Root string `json:"root,omitempty"`
	// 仓库目录
	Repository string `json:"repository,omitempty"`
	// 仓库动作
	Action constants.GitActionKind `json:"action" validate:"required"`
	// 提交说明
	Message string `json:"message,omitempty"`
	// 提交文件
	FilePaths []string `json:"filePaths,omitempty"`
	// 预期分支
	ExpectedBranch *string `json:"expectedBranch,omitempty"`
	// 预期提交
	ExpectedHead *string `json:"expectedHead,omitempty"`
	// 预期暂存版本
	ExpectedIndex *string `json:"expectedIndex,omitempty"`
	// 预期上游分支
	ExpectedUpstream *string `json:"expectedUpstream,omitempty"`
	// 推送远端
	Remote string `json:"remote,omitempty"`
	// 预期远端分支
	ExpectedRemoteBranch *string `json:"expectedRemoteBranch,omitempty"`
	// PR 基准分支
	Base string `json:"base,omitempty"`
	// 切换分支
	Branch string `json:"branch,omitempty"`
}

// GitActionResult 是仓库操作结果。
type GitActionResult struct {
	// 当前分支
	Branch string `json:"branch,omitempty"`
	// 新建提交
	Commit string `json:"commit,omitempty"`
	// 已经推送
	Pushed bool `json:"pushed,omitempty"`
	// PR 地址
	PRURL string `json:"prUrl,omitempty"`
	// PR 基准分支
	Base string `json:"base,omitempty"`
	// 失败步骤
	FailedStep string `json:"failedStep,omitempty"`
	// 失败错误码
	Error string `json:"error,omitempty"`
	// 公开失败原因
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// GitMessageGenerator 根据所属对话和暂存补丁生成说明，fallback 表明运行时不支持无工具调用。
type GitMessageGenerator func(ctx context.Context, chatID, patch string) (message string, fallback bool, err error)

// GitRefChangeGuard 在执行范围内保护真实工作树的短时分支切换。
type GitRefChangeGuard func(ctx context.Context, repository string, change func() error) error

// RunGitAction 核对用户确认的仓库和版本，然后依次执行 Git 步骤。
func (m *Manager) RunGitAction(ctx context.Context, subject string, request GitActionRequest, generate GitMessageGenerator, guard GitRefChangeGuard) (GitActionResult, error) {
	m.mutations.Lock()
	defer m.mutations.Unlock()
	var result GitActionResult
	repo, _, err := m.repository(ctx, subject, request.ScopeID, request.Root, request.Repository)
	if err != nil {
		return result, err
	}
	if repo == "" {
		return result, status_error.WorkspaceResourceNotFound
	}
	refs, err := checkedGitRefs(ctx, repo, request)
	if err != nil {
		return result, err
	}
	result.Branch = refs.branch

	if request.Action == constants.GitActionCreateBranch || request.Action == constants.GitActionCheckout {
		change := func() error {
			if _, err = checkedGitRefs(ctx, repo, request); err != nil {
				return err
			}
			if request.Action == constants.GitActionCreateBranch {
				result.FailedStep = "branch"
				if !validGitBranch(ctx, repo, request.Branch) {
					return status_error.WorkspaceGitTargetInvalid
				}
				if _, err = gitCommand(ctx, repo, "switch", "--quiet", "--no-overwrite-ignore", "--no-recurse-submodules", "--create", request.Branch); err == nil {
					result.Branch = request.Branch
				}
				return err
			}
			result.FailedStep = "checkout"
			branch, err := m.checkout(ctx, subject, request.ScopeID, request.Root, repo, request.Branch)
			if err == nil {
				result.Branch = branch
			}
			return err
		}
		if guard != nil {
			err = guard(ctx, repo, change)
		} else {
			err = change()
		}
		if err == nil {
			result.FailedStep = ""
		}
		return result, err
	}

	commit := request.Action == constants.GitActionCommit || request.Action == constants.GitActionCommitPush || request.Action == constants.GitActionCommitPushPR
	push := request.Action == constants.GitActionPush || request.Action == constants.GitActionCommitPush || request.Action == constants.GitActionCommitPushPR || request.Action == constants.GitActionPushPR
	pr := request.Action == constants.GitActionCreatePR || request.Action == constants.GitActionCommitPushPR || request.Action == constants.GitActionFindPR || request.Action == constants.GitActionPushPR
	if !commit && !push && !pr {
		return result, status_error.BadRequest
	}
	var target gitRemoteTarget
	if push || pr {
		target, err = prepareGitRemote(ctx, repo, refs, request.Remote)
		if err != nil {
			return result, err
		}
		if request.ExpectedRemoteBranch != nil && *request.ExpectedRemoteBranch != target.branch {
			return result, status_error.WorkspaceResourceConflict
		}
	}
	var prTarget gitHubPRTarget
	if pr {
		if request.Base != "" && !validGitBranch(ctx, repo, request.Base) {
			return result, status_error.WorkspaceGitTargetInvalid
		}
		prTarget, err = func() (gitHubPRTarget, error) {
			m.mutations.Unlock()
			defer m.mutations.Lock()
			return prepareGitHubPR(ctx, repo, target.name, request.Base, request.Action == constants.GitActionFindPR)
		}()
		if err != nil {
			return result, err
		}
		result.Base = prTarget.base
		if prTarget.base == target.branch {
			if request.Action == constants.GitActionFindPR {
				return result, nil
			}
			return result, status_error.WorkspaceGitTargetInvalid
		}
		if _, err = m.scope(subject, request.ScopeID); err != nil {
			return result, err
		}
		if _, err = checkedGitRefs(ctx, repo, GitActionRequest{ExpectedBranch: &refs.branch, ExpectedHead: &refs.head, ExpectedUpstream: &refs.upstream}); err != nil {
			return result, err
		}
	}
	if commit {
		result.FailedStep = "commit"
		result.Commit, err = m.commitGitAction(ctx, subject, repo, refs, request, generate)
		if err != nil {
			return result, err
		}
		refs.head = result.Commit
	}
	if (push || pr) && refs.head == "" {
		return result, status_error.WorkspaceGitTargetInvalid
	}
	if push {
		result.FailedStep = "push"
		if pr {
			repository, err := githubPushRepository(ctx, repo, target.name)
			if err != nil {
				return result, err
			}
			if repository != prTarget.repository {
				return result, status_error.WorkspaceResourceConflict
			}
		}
		if result.Pushed, err = pushGitBranch(ctx, repo, refs, target); err != nil {
			return result, err
		}
	}
	if pr {
		result.FailedStep = "pr"
		m.mutations.Unlock()
		defer m.mutations.Lock()
		result.PRURL, err = runGitHubPR(ctx, repo, refs, target, prTarget, request.Action == constants.GitActionFindPR)
		if err != nil {
			return result, err
		}
	}
	result.FailedStep = ""
	return result, nil
}

func checkedGitRefs(ctx context.Context, repo string, request GitActionRequest) (gitRefState, error) {
	refs, err := gitRefs(ctx, repo)
	if err != nil {
		return refs, err
	}
	if request.ExpectedBranch != nil && *request.ExpectedBranch != refs.branch ||
		request.ExpectedHead != nil && *request.ExpectedHead != refs.head ||
		request.ExpectedUpstream != nil && *request.ExpectedUpstream != refs.upstream {
		return refs, status_error.WorkspaceResourceConflict
	}
	return refs, nil
}

func (m *Manager) commitGitAction(ctx context.Context, subject, repo string, refs gitRefState, request GitActionRequest, generate GitMessageGenerator) (string, error) {
	message := strings.TrimSpace(request.Message)
	if len(message) > gitCommitMessageLimit || strings.ContainsRune(message, '\x00') {
		return "", status_error.BadRequest
	}
	index := refs.index
	if request.ExpectedIndex != nil && *request.ExpectedIndex != index || index == "" && request.FilePaths == nil {
		return "", status_error.WorkspaceResourceConflict
	}
	var selection *gitCommitSelection
	var options gitCommandOptions
	if request.FilePaths != nil {
		scope, err := m.scope(subject, request.ScopeID)
		if err != nil {
			return "", err
		}
		selection, err = prepareGitSelection(ctx, scope, repo, refs.head, request.FilePaths)
		if err != nil {
			return "", err
		}
		defer selection.close()
		options = selection.preview
		options.input = ""
	}
	var err error
	if message == "" {
		message, err = m.generateGitMessage(ctx, subject, repo, refs, index, request, generate, options)
		if err != nil {
			return "", err
		}
		message = strings.TrimSpace(message)
		if message == "" || len(message) > gitCommitMessageLimit || strings.ContainsRune(message, '\x00') {
			return "", status_error.RuntimeExecutionFailed
		}
	}
	if selection != nil {
		return selection.commit(ctx, repo, refs, message)
	}
	if err = verifyGitCommitState(ctx, repo, refs, index); err != nil {
		return "", err
	}
	if _, err = gitCommand(ctx, repo, "commit", "-m", message); err != nil {
		return "", err
	}
	hash, err := gitCommand(ctx, repo, "rev-parse", "--verify", "HEAD")
	return strings.TrimSpace(hash), err
}

func (m *Manager) generateGitMessage(ctx context.Context, subject, repo string, refs gitRefState, index string, request GitActionRequest, generate GitMessageGenerator, options gitCommandOptions) (string, error) {
	scope, err := m.scope(subject, request.ScopeID)
	if err != nil {
		return "", err
	}
	if scope.chatID == "" {
		return stagedCommitDescription(ctx, repo, options)
	}
	if generate == nil {
		return "", status_error.RuntimeUnavailable
	}
	output := &boundedGitOutput{limit: gitMessagePatchLimit, keepPrefix: true}
	err = runGitWithOptions(ctx, repo, false, output, options, "diff", "--cached", "--stat", "--stat-count=50", "--patch", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", "--ignore-submodules=dirty", "--", gitExcludeAccountRoot, gitExcludeAccounts)
	if err != nil {
		return "", err
	}
	if err = verifyGitCommitState(ctx, repo, refs, index); err != nil {
		return "", err
	}
	if output.buffer.Len() == 0 {
		return "Update staged files", nil
	}
	patch := strings.ToValidUTF8(output.buffer.String(), "\uFFFD")
	if output.truncated {
		patch += "\n[Staged patch truncated]\n"
	}
	message, fallback, err := func() (string, bool, error) {
		m.mutations.Unlock()
		defer m.mutations.Lock()
		return generate(ctx, scope.chatID, patch)
	}()
	if err != nil {
		return "", err
	}
	repository, _, err := m.repository(ctx, subject, request.ScopeID, request.Root, repo)
	if err != nil {
		return "", err
	}
	if repo != repository {
		return "", status_error.WorkspaceResourceConflict
	}
	if fallback {
		return stagedCommitDescription(ctx, repo, options)
	}
	return message, nil
}

func stagedCommitDescription(ctx context.Context, repo string, options gitCommandOptions) (string, error) {
	output, err := gitCommandWithOptions(ctx, repo, options, "diff", "--cached", "--name-status", "--no-renames", "--no-ext-diff", "--ignore-submodules=dirty", "-z", "--", gitExcludeAccountRoot, gitExcludeAccounts)
	if err != nil {
		return "", err
	}
	if output == "" {
		return "Update staged files", nil
	}
	records := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	if len(records) != 2 {
		return "Update " + strconv.Itoa(len(records)/2) + " files", nil
	}
	verb := "Update"
	switch records[0] {
	case "A":
		verb = "Add"
	case "D":
		verb = "Remove"
	}
	name := strings.TrimSpace(strings.Map(func(value rune) rune {
		if unicode.IsControl(value) {
			return ' '
		}
		return value
	}, records[1]))
	if name == "" {
		name = "one file"
	} else if runes := []rune(name); len(runes) > 65 {
		name = string(runes[:62]) + "..."
	}
	return verb + " " + name, nil
}

func verifyGitCommitState(ctx context.Context, repo string, refs gitRefState, index string) error {
	current, err := checkedGitRefs(ctx, repo, GitActionRequest{ExpectedBranch: &refs.branch, ExpectedHead: &refs.head})
	if err != nil {
		return err
	}
	if current.index != index {
		return status_error.WorkspaceResourceConflict
	}
	return nil
}

func validGitBranch(ctx context.Context, repo, name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsRune(name, '\x00') {
		return false
	}
	_, err := gitCommand(ctx, repo, "check-ref-format", "refs/heads/"+name)
	return err == nil
}

type gitRemoteTarget struct {
	// name 是显式选择的已配置远端。
	name string
	// branch 是远端的推送目标短分支。
	branch string
	// setupUpstream 指定首次推送时建立上游关系。
	setupUpstream bool
}

func prepareGitRemote(ctx context.Context, repo string, refs gitRefState, remote string) (gitRemoteTarget, error) {
	target := gitRemoteTarget{name: remote, branch: refs.branch}
	if refs.branch == "" || refs.branch == detachedGitBranch || remote == "" || strings.HasPrefix(remote, "-") {
		return target, status_error.WorkspaceGitTargetInvalid
	}
	names, err := gitRemoteNames(ctx, repo)
	if err != nil {
		return target, err
	}
	if !slices.Contains(names, remote) {
		return target, status_error.WorkspaceGitTargetInvalid
	}
	upstreamRemote, upstreamBranch, err := gitUpstreamDestination(ctx, repo, refs.branch)
	if err != nil {
		return target, err
	}
	if upstreamRemote == remote && upstreamBranch != "" {
		target.branch = upstreamBranch
	}
	target.setupUpstream = upstreamRemote == ""
	return target, nil
}

func pushGitBranch(ctx context.Context, repo string, refs gitRefState, target gitRemoteTarget) (bool, error) {
	if _, err := checkedGitRefs(ctx, repo, GitActionRequest{ExpectedBranch: &refs.branch, ExpectedHead: &refs.head, ExpectedUpstream: &refs.upstream}); err != nil {
		return false, err
	}
	// 来源固定为已确认的提交，避免 Git 启动时本地分支已经前移。
	if _, err := gitCommand(ctx, repo, "push", "--no-mirror", "--no-follow-tags", "--recurse-submodules=no", "--", target.name, refs.head+":refs/heads/"+target.branch); err != nil {
		return false, err
	}
	if target.setupUpstream {
		// Git 按远端的 fetch refspec 解析跟踪引用。这里只记录远端和分支。
		key := "branch." + refs.branch
		if _, err := gitCommand(ctx, repo, "config", "--local", "--replace-all", key+".remote", target.name); err != nil {
			return true, err
		}
		_, err := gitCommand(ctx, repo, "config", "--local", "--replace-all", key+".merge", "refs/heads/"+target.branch)
		return true, err
	}
	return true, nil
}

// RepositoryIntersectsWorkspace 校验真实仓库与冻结执行目录是否存在路径交集。
func RepositoryIntersectsWorkspace(repository string, directories ...string) bool {
	for _, directory := range directories {
		actual, err := canonicalDirectory(directory)
		if err == nil && (within(actual, repository) || within(repository, actual)) {
			return true
		}
	}
	return false
}
