package main_conversations

import (
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取主会话阅读
type GetMainConversationReading struct {
	// MethodGet 声明只读查询。
	ginx.MethodGet
	// 主体类型
	SubjectType string `in:"path" name:"subjectType" validate:"required,oneof=waker team"`
	// 主体标识
	SubjectID string `in:"path" name:"subjectId" validate:"required,max=32"`
}

// 主会话阅读响应
type MainConversationReadingResponse struct {
	// 阅读状态
	models.MainConversationReadingState
	// 查询完整性
	models.AttentionCompleteness
}

// Path 返回主会话阅读状态地址。
func (*GetMainConversationReading) Path() string { return "/:subjectType/:subjectId/reading" }

// Output 由真实主体解析会话，并读取公开消息的权威阅读边界。
// @err[WakerNotFound][40400000001][伙伴不存在]
// @err[ChannelNotFound][40400000012][团队不存在]
func (d *GetMainConversationReading) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	conversation, err := global.MainConversationService.Ensure(request, d.SubjectType, d.SubjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	value := MainConversationReadingResponse{AttentionCompleteness: models.AttentionCompleteness{Complete: true, Epoch: global.EventHub.Epoch()}}
	value.MainConversationReadingState, err = global.MainConversationControllerManager.Reading(request, constants.LocalReaderID, conversation.ID)
	if errors.Is(err, status_error.ReadingProjectionIncomplete) {
		value.ConversationID = conversation.ID
		value.Complete = false
		value.IncompleteReason = "isolated"
		return value, nil
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
