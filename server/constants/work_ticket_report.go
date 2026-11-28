package constants

const (
	// WorkTicketCapability 标识由当前工单执行授予的工作报告能力。
	WorkTicketCapability = "work.tickets"
	// WorkTicketReportOperation 将当前工单的一份结构化报告交给原会话。
	WorkTicketReportOperation = "work.tickets.report"
	// WorkTicketDelegateOperation 接纳当前工单明确转交的一项子工作。
	WorkTicketDelegateOperation = "work.tickets.delegate"
	// WorkTicketResultsOperation 读取原委派的直接返回。
	WorkTicketResultsOperation = "work.tickets.results"
	// WorkTicketReplyOperation 回答一张直接子单已经提交的私有问题。
	WorkTicketReplyOperation = "work.tickets.reply"
	// WorkTicketEventWaitOperation 登记当前已订阅讨论内的一次事件等待。
	WorkTicketEventWaitOperation = "work.tickets.wait-for-event"
	// WorkTicketEventSubscribeOperation 为当前工单接纳已获授权的事件上下文。
	WorkTicketEventSubscribeOperation = "work.tickets.subscribe-event"
	// WorkTicketHostActor 标识持久委派和直接回件触发的宿主输入。
	WorkTicketHostActor = "work-ticket"
	// WorkTicketReportActor 标识独立投递到原会话的接手人回件。
	WorkTicketReportActor = "work_ticket_report"
	// WorkTicketHandoffActor 标识原普通会话里由宿主发布的移交确认。
	WorkTicketHandoffActor = "work_ticket_handoff"
	// WorkTicketReportKindProgress 表示已有新进展，工单仍由当前尝试负责。
	WorkTicketReportKindProgress = "progress"
)
