package constants

//go:generate toolx gen enum AutomaticReportDestination

// AutomaticReportDestination 表示运行报告约定的收件位置。
type AutomaticReportDestination string

const (
	// AutomaticReportDestinationUnspecified 表示只保留运行记录。
	AutomaticReportDestinationUnspecified AutomaticReportDestination = ""
	// AutomaticReportDestinationMainConversation 将报告投递给当前主体的主会话。
	AutomaticReportDestinationMainConversation AutomaticReportDestination = "main_conversation"
	// AutomaticReportDestinationTopic 将报告投递到同一规则的专属话题。
	AutomaticReportDestinationTopic AutomaticReportDestination = "topic"
)
