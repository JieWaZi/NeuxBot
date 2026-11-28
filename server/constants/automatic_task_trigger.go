package constants

//go:generate toolx gen enum AutomaticTaskTrigger

// AutomaticTaskTrigger 是自动任务唯一的持久触发类型。
type AutomaticTaskTrigger string

const (
	// AutomaticTaskTriggerUnspecified 表示尚未选择触发类型。
	AutomaticTaskTriggerUnspecified AutomaticTaskTrigger = ""
	// AutomaticTaskTriggerSchedule 表示根据任务时区的时间规则触发。
	AutomaticTaskTriggerSchedule AutomaticTaskTrigger = "schedule"
	// AutomaticTaskTriggerAPI 表示由持有任务密钥的本机脚本触发。
	AutomaticTaskTriggerAPI AutomaticTaskTrigger = "api"
	// AutomaticTaskTriggerEvent 由规则授权的外部变化触发。
	AutomaticTaskTriggerEvent AutomaticTaskTrigger = "event"
)
