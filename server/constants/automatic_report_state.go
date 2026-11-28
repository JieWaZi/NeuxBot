package constants

//go:generate toolx gen enum AutomaticReportState

// AutomaticReportState 表示与运行终态分别记录的报告交付进度。
type AutomaticReportState string

const (
	// AutomaticReportStateUnspecified 表示该次运行没有报告约定。
	AutomaticReportStateUnspecified AutomaticReportState = ""
	// AutomaticReportStatePending 表示等待可交付终态或投递收据。
	AutomaticReportStatePending AutomaticReportState = "pending"
	// AutomaticReportStateDelivered 表示报告已进入实际收件位置。
	AutomaticReportStateDelivered AutomaticReportState = "delivered"
	// AutomaticReportStateQuiet 表示规则只汇报变化且此次运行未提供变化内容。
	AutomaticReportStateQuiet AutomaticReportState = "quiet"
	// AutomaticReportStateUnavailable 表示运行未完成，无可交付正文。
	AutomaticReportStateUnavailable AutomaticReportState = "unavailable"
)
