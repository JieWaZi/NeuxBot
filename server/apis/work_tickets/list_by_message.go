package work_tickets

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查询消息工单
type ListWorkTickets struct {
	ginx.MethodGet
	// 会话类型
	OriginKind string `in:"query" name:"originKind" validate:"required,oneof=chat main"`
	// 会话标识
	OriginID string `in:"query" name:"originId" validate:"required,max=32"`
	// 消息标识
	MessageIDs []string `in:"query" name:"messageId" validate:"required,min=1,max=100,dive,required,max=32"`
}

// Path 返回当前消息页的工单查询入口。
func (*ListWorkTickets) Path() string { return "" }

// Output 在原会话范围内批量读取当前消息关联的工单。
// @err[WakerNotFound][40400000001][伙伴不存在]
// @err[ChannelNotFound][40400000012][团队不存在]
func (o *ListWorkTickets) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	address := workticket.Address{Kind: constants.WorkTicketOrigin(o.OriginKind), ID: o.OriginID}
	if err := global.ValidateWorkTicketOrigin(request, address); err != nil {
		return nil, ginx.WithStack(err)
	}
	rows, err := global.WorkTicketService.Lookup(request, address, o.MessageIDs)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if rows == nil {
		rows = []models.WorkTicketMessageView{}
	}
	return rows, nil
}
