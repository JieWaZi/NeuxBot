package constants

//go:generate toolx gen enum WorkTicketStatus

// WorkTicketStatus 是工单的业务状态。
type WorkTicketStatus string

const (
	// WorkTicketStatusUnspecified 表示尚无业务状态。
	WorkTicketStatusUnspecified WorkTicketStatus = ""
	// WorkTicketStatusActive 表示当前工作仍在推进。
	WorkTicketStatusActive WorkTicketStatus = "active"
	// WorkTicketStatusWaiting 表示工单等待用户继续交代或补充。
	WorkTicketStatusWaiting WorkTicketStatus = "waiting"
	// WorkTicketStatusBlocked 表示工单受阻，需要用户处理。
	WorkTicketStatusBlocked WorkTicketStatus = "blocked"
	// WorkTicketStatusCompleted 表示当前尝试已提交结论。
	WorkTicketStatusCompleted WorkTicketStatus = "completed"
	// WorkTicketStatusStopped 表示用户已停止工单并撤销工作授权。
	WorkTicketStatusStopped WorkTicketStatus = "stopped"
)
