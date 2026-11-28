package constants

//go:generate toolx gen enum AutomaticReportPolicy

// AutomaticReportNoChanges 是任务明确确认没有可汇报变化的完整文本。
const AutomaticReportNoChanges = "[NEUX_NO_CHANGES]"

// AutomaticReportPolicy 决定完成运行后是否应汇报。
type AutomaticReportPolicy string

const (
	// AutomaticReportPolicyUnspecified 表示尚未设置汇报策略。
	AutomaticReportPolicyUnspecified AutomaticReportPolicy = ""
	// AutomaticReportPolicyAlways 对每次完成的运行发出报告。
	AutomaticReportPolicyAlways AutomaticReportPolicy = "always"
	// AutomaticReportPolicyChangesOnly 只发布有变化的正文。
	AutomaticReportPolicyChangesOnly AutomaticReportPolicy = "changes_only"
)
