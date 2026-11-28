package work_tickets

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 停止工单请求
type StopWorkTicketRequest struct {
	// 会话类型
	OriginKind string `json:"originKind" validate:"required,oneof=chat main"`
	// 会话标识
	OriginID string `json:"originId" validate:"required,max=32"`
}

// 停止工单
type StopWorkTicket struct {
	ginx.MethodPost
	// 工单标识
	TicketID string `in:"path" name:"ticketId" validate:"required,max=32"`
	// 请求体
	Body StopWorkTicketRequest `in:"body"`
}

// Path 返回该工单的停止入口。
func (*StopWorkTicket) Path() string { return "/:ticketId/stop" }

// Output 提交撤权并取消原尝试关联的具体执行。
// @err[WakerNotFound][40400000001][伙伴不存在]
// @err[ChannelNotFound][40400000012][团队不存在]
// @err[WorkTicketNotFound][40400007211][工单不存在]
// @err[WorkTicketCannotContinue][40900007211][当前工单无法继续或停止]
func (o *StopWorkTicket) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	address := workticket.Address{Kind: constants.WorkTicketOrigin(o.Body.OriginKind), ID: o.Body.OriginID}
	if err := global.ValidateWorkTicketOrigin(request, address); err != nil {
		return nil, ginx.WithStack(err)
	}
	ticket, err := global.WorkTicketService.Stop(request, address, o.TicketID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if ticket.CurrentExecutionID != "" {
		global.ExecutionService.CancelExecution(ticket.WorkChatID, ticket.CurrentExecutionID)
	}
	return ginx.CommonSuccessResponse(), nil
}
