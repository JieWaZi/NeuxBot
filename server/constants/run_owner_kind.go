package constants

//go:generate toolx gen enum RunOwnerKind

// RunOwnerKind 标识可以独立查看最终结果的原运行类型。
type RunOwnerKind string

const (
	// RunOwnerKindWorkflowRun 表示工作流运行。
	RunOwnerKindWorkflowRun RunOwnerKind = "workflow_run"
	// RunOwnerKindAutomaticRun 表示无已接纳目标的自动任务机会。
	RunOwnerKindAutomaticRun RunOwnerKind = "automatic_run"
)
