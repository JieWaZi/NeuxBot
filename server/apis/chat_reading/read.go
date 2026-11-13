package chat_reading

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// MarkChatRead 确认一个会话中已呈现的公开消息前缀。
type MarkChatRead struct {
	// MethodPost 声明写入操作。
	ginx.MethodPost
	// ChatID 是原公开会话标识。
	ChatID string `in:"path" name:"chatId" validate:"required,len=32"`
	// Body 固定已呈现边界及阅读意图。
	Body MarkChatReadRequest `in:"body"`
}

// MarkChatReadRequest 保存普通或显式阅读意图。
type MarkChatReadRequest struct {
	// ThroughMessageID 是用户实际看到的公开消息。
	ThroughMessageID string `json:"throughMessageID" validate:"required,max=128"`
	// ObservedUnreadGeneration 是客户端观察到的手动未读代次。
	ObservedUnreadGeneration int64 `json:"observedUnreadGeneration" validate:"min=0"`
	// Intent 指定是否为允许解除手动未读的显式阅读。
	Intent constants.ReadingIntent `json:"intent" validate:"required"`
}

func (*MarkChatRead) Path() string { return "/:chatId/reading" }
func (d *MarkChatRead) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	if err := validateChat(request, d.ChatID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.ChatReadingControllerManager.MarkRead(request, constants.LocalReaderID, d.ChatID, d.Body.ThroughMessageID, d.Body.ObservedUnreadGeneration, d.Body.Intent); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// UnmarshalJSON 校验 HTTP 阅读命令的完整字段。
func (d *MarkChatReadRequest) UnmarshalJSON(raw []byte) error {
	type payload MarkChatReadRequest
	return decodeReadingCommand(raw, (*payload)(d))
}
