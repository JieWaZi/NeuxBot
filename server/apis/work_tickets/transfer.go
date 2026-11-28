package work_tickets

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 转交工单请求
type TransferWorkTicketRequest struct {
	// 原会话类型
	OriginKind string `json:"originKind" validate:"required,oneof=chat main"`
	// 原会话标识
	OriginID string `json:"originId" validate:"required,max=32"`
	// 接手主体类型
	TargetKind string `json:"targetKind" validate:"required,oneof=waker team"`
	// 接手主体标识
	TargetID string `json:"targetId" validate:"required,max=32"`
	// 当前责任代次
	ExpectedGeneration int64 `json:"expectedGeneration" validate:"required,min=1"`
	// 稳定请求标识
	RequestID string `json:"requestId" validate:"required,max=64"`
	// 显式交接内容
	Content string `json:"content" validate:"required"`
}

// 工单转交回执
type TransferWorkTicketResponse struct {
	// 原工单标识
	TicketID string `json:"ticketId"`
	// 新责任代次
	Generation int64 `json:"generation"`
	// 接手执行标识
	ExecutionID string `json:"executionId"`
}

// 转交工单
type TransferWorkTicket struct {
	ginx.MethodPost
	// 工单标识
	TicketID string `in:"path" name:"ticketId" validate:"required,max=32"`
	// 请求体
	Body TransferWorkTicketRequest `in:"body"`
}

// Path 返回原用户发起责任转交的入口。
func (*TransferWorkTicket) Path() string { return "/:ticketId/transfer" }

// Output 原子更新工单责任并提交新尝试的派发意图。
// @err[WakerNotFound][40400000001][伙伴不存在]
// @err[ChannelNotFound][40400000012][团队不存在]
// @err[WorkTicketNotFound][40400007211][工单不存在]
// @err[WorkTicketCannotContinue][40900007211][当前工单无法转交]
func (o *TransferWorkTicket) Output(ctx *gin.Context) (interface{}, error) {
	body := o.Body
	receipt, err := global.TransferWorkTicket(ctx.Request.Context(), workticket.TransferInput{
		Address:  workticket.Address{Kind: constants.WorkTicketOrigin(body.OriginKind), ID: body.OriginID},
		TicketID: o.TicketID, ExpectedGeneration: body.ExpectedGeneration, RequestID: body.RequestID,
		OwnerKind: body.TargetKind, OwnerID: body.TargetID, Content: body.Content,
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return TransferWorkTicketResponse{TicketID: receipt.TicketID, Generation: receipt.Generation, ExecutionID: receipt.ExecutionID}, nil
}
