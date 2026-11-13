package constants

//go:generate toolx gen enum WorkOwnerKind

// WorkOwnerKind 引用看板原有工作容器的类型。
type WorkOwnerKind string

const (
	// WorkOwnerKindChat 表示独立会话。
	WorkOwnerKindChat WorkOwnerKind = "chat"
	// WorkOwnerKindTeamThread 表示团队对话。
	WorkOwnerKindTeamThread WorkOwnerKind = "team_thread"
	// WorkOwnerKindWorkflowRun 表示对用户可见的工作流运行。
	WorkOwnerKindWorkflowRun WorkOwnerKind = "workflow_run"
	// WorkOwnerKindAutomaticRun 表示没有接纳目标的自动机会。
	WorkOwnerKindAutomaticRun WorkOwnerKind = "automatic_run"
)
