package main_conversations

import (
	"github.com/JieWaZi/NeuxBot/server/apis/chat_reading"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 标记主会话已读
type MarkMainConversationRead struct {
	// MethodPost 声明已读命令。
	ginx.MethodPost
	// 主体类型
	SubjectType string `in:"path" name:"subjectType" validate:"required,oneof=waker team"`
	// 主体标识
	SubjectID string `in:"path" name:"subjectId" validate:"required,max=32"`
	// 请求体
	Body chat_reading.MarkChatReadRequest `in:"body"`
}

// Path 返回写入主会话已读的地址。
func (*MarkMainConversationRead) Path() string { return "/:subjectType/:subjectId/reading" }

// Output 只推进到指定主会话中已呈现的合格消息。
// @err[BadRequest][40000000001][消息边界无效]
// @err[WakerNotFound][40400000001][伙伴不存在]
// @err[ChannelNotFound][40400000012][团队不存在]
// @err[ReadingIntentConflict][40900009201][阅读位置已变化]
// @err[ReadingProjectionIncomplete][50300009201][阅读边界不可用]
func (d *MarkMainConversationRead) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	conversation, err := global.MainConversationService.Ensure(request, d.SubjectType, d.SubjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.MainConversationControllerManager.MarkRead(request, constants.LocalReaderID, conversation.ID, d.Body.ThroughMessageID, d.Body.ObservedUnreadGeneration, d.Body.Intent); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
