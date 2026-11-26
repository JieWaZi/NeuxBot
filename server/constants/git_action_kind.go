package constants

//go:generate toolx gen enum GitActionKind

// GitActionKind 表示用户明确选择的仓库动作。
type GitActionKind string

const (
	// GitActionCommit 提交当前索引。
	GitActionCommit GitActionKind = "commit"
	// GitActionPush 推送当前分支。
	GitActionPush GitActionKind = "push"
	// GitActionPushPR 推送当前分支后创建 PR。
	GitActionPushPR GitActionKind = "push_pr"
	// GitActionCommitPush 提交当前索引后推送。
	GitActionCommitPush GitActionKind = "commit_push"
	// GitActionCreatePR 创建或返回当前分支的 PR。
	GitActionCreatePR GitActionKind = "create_pr"
	// GitActionCommitPushPR 依次提交、推送和创建 PR。
	GitActionCommitPushPR GitActionKind = "commit_push_pr"
	// GitActionFindPR 查询当前分支的现有 PR。
	GitActionFindPR GitActionKind = "find_pr"
	// GitActionCreateBranch 创建并切换到本地分支。
	GitActionCreateBranch GitActionKind = "create_branch"
	// GitActionCheckout 安全地切换分支。
	GitActionCheckout GitActionKind = "checkout"
)
