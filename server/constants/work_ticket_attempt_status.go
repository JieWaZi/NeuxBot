package constants

//go:generate toolx gen enum WorkTicketAttemptStatus

// WorkTicketAttemptStatus 表示当前尝试的业务资格。
type WorkTicketAttemptStatus string

const (
	// WorkTicketAttemptStatusUnspecified 表示尚未建立尝试。
	WorkTicketAttemptStatusUnspecified WorkTicketAttemptStatus = ""
	// WorkTicketAttemptStatusPending 表示尝试已持久预留，待输入接纳。
	WorkTicketAttemptStatusPending WorkTicketAttemptStatus = "pending"
	// WorkTicketAttemptStatusRunning 表示尝试可以跨回合继续推进工单。
	WorkTicketAttemptStatusRunning WorkTicketAttemptStatus = "running"
	// WorkTicketAttemptStatusEnded 表示尝试已结束，不能再推进工单。
	WorkTicketAttemptStatusEnded WorkTicketAttemptStatus = "ended"
)
