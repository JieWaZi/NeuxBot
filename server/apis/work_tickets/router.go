// Package work_tickets 提供按原交办会话读取和停止工单的接口。
package work_tickets

import "github.com/shrewx/ginx"

// Router 注册本机工单接口。
var Router = ginx.NewRouter(ginx.Group("/work-tickets"))

func init() {
	Router.Register(&ListWorkTickets{})
	Router.Register(&StopWorkTicket{})
	Router.Register(&TransferWorkTicket{})
}
