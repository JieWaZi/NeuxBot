package constants

//go:generate toolx gen enum AutomaticRunSource

// AutomaticRunSource 是一次运行机会的实际来源。
type AutomaticRunSource string

const (
	// AutomaticRunSourceUnspecified 表示缺少运行来源。
	AutomaticRunSourceUnspecified AutomaticRunSource = ""
	// AutomaticRunSourceSchedule 表示一次计划时间到达。
	AutomaticRunSourceSchedule AutomaticRunSource = "schedule"
	// AutomaticRunSourceAPI 表示一个带幂等标识的本机请求。
	AutomaticRunSourceAPI AutomaticRunSource = "api"
	// AutomaticRunSourceManual 表示用户明确点击立即运行。
	AutomaticRunSourceManual AutomaticRunSource = "manual"
	// AutomaticRunSourceEvent 表示规则接纳的真实来源变化。
	AutomaticRunSourceEvent AutomaticRunSource = "event"
	// AutomaticRunSourceEventTest 表示用户明确请求的事件测试。
	AutomaticRunSourceEventTest AutomaticRunSource = "event_test"
)
