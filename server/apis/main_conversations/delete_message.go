package main_conversations

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除主会话消息
type DeleteMainConversationMessage struct {
	// 删除请求
	ginx.MethodDelete
	// 主体类型
	SubjectType string `in:"path" name:"subjectType" validate:"required,oneof=waker team"`
	// 主体标识
	SubjectID string `in:"path" name:"subjectId" validate:"required,max=32"`
	// 消息标识
	MessageID string `in:"path" name:"messageId" validate:"required,max=32"`
}

// Path 返回主体内的单条公开消息地址。
func (*DeleteMainConversationMessage) Path() string {
	return "/:subjectType/:subjectId/messages/:messageId"
}

// Output 从主会话移除单条消息，原工作记录可继续访问。
// @err[BadRequest][40000000001][请求参数无效]
// @err[ChatMessageNotFound][40400007201][消息不存在]
// @err[WakerNotFound][40400000001][伙伴不存在]
// @err[ChannelNotFound][40400000012][团队不存在]
// @err[DatabaseOperationCancelled][40800001001][数据库操作已取消]
// @err[DatabaseError][50000000002][数据库操作失败]
// @err[DatabaseBusy][50300001001][数据库暂忙]
// @err[InternalServerError][50000000001][无法移除消息]
func (d *DeleteMainConversationMessage) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.MainConversationService.RemoveMessage(ctx.Request.Context(), d.SubjectType, d.SubjectID, d.MessageID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
