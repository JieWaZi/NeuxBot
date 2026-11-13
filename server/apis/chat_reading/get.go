package chat_reading

import (
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetChatReading 获取一个公开对话的阅读状态。
type GetChatReading struct {
	// MethodGet 声明只读方法。
	ginx.MethodGet
	// ChatID 是原公开 Chat 的身份。
	ChatID string `in:"path" name:"chatId" validate:"required,len=32"`
}

// ChatReadingResponse 包含阅读事实和当前投影健康。
type ChatReadingResponse struct {
	// ChatReadingState 保存游标及公开边界。
	models.ChatReadingState
	// AttentionCompleteness 报告该会话投影完整性。
	models.AttentionCompleteness
}

func (*GetChatReading) Path() string { return "/:chatId/reading" }
func (d *GetChatReading) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	value := ChatReadingResponse{AttentionCompleteness: models.AttentionCompleteness{Complete: true, Epoch: global.EventHub.Epoch()}}
	err := validateChat(request, d.ChatID)
	if err == nil {
		value.ChatReadingState, err = global.ChatReadingControllerManager.Get(request, constants.LocalReaderID, d.ChatID)
	}
	reason := global.ChatService.ReadingIncompleteReason([]string{d.ChatID})
	if errors.Is(err, status_error.ReadingProjectionIncomplete) && reason == "" {
		reason = "recovering"
	}
	if reason != "" && (err == nil || errors.Is(err, status_error.ReadingProjectionIncomplete) || errors.Is(err, status_error.ChatLogError) || errors.Is(err, status_error.ChatLogCorrupted) || errors.Is(err, status_error.ChatLogIndexCorrupted)) {
		cursor := value.ChatReadCursor
		if err != nil {
			// 追平失败时仍需单独核验公开归属，再读取独立保存的游标。
			if err := validatePublicChat(request, d.ChatID); err != nil {
				return nil, ginx.WithStack(err)
			}
			cursor, err = global.ChatReadingControllerManager.GetCursor(request, constants.LocalReaderID, d.ChatID)
			if err != nil && !errors.Is(err, status_error.ReadingProjectionIncomplete) {
				return nil, ginx.WithStack(err)
			}
		}
		cursor.ChatID = d.ChatID
		value.ChatReadingState = models.ChatReadingState{ChatReadCursor: cursor}
		value.Complete = false
		value.IncompleteReason = reason
		return value, nil
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
