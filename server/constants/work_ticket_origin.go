package constants

//go:generate toolx gen enum WorkTicketOrigin

// WorkTicketOrigin 区分工单所归属的聊天和主体公开主会话。
type WorkTicketOrigin string

const (
	// WorkTicketOriginUnspecified 表示尚未指定来源。
	WorkTicketOriginUnspecified WorkTicketOrigin = ""
	// WorkTicketOriginChat 表示普通聊天。
	WorkTicketOriginChat WorkTicketOrigin = "chat"
	// WorkTicketOriginMain 表示主体主会话。
	WorkTicketOriginMain WorkTicketOrigin = "main"
)
