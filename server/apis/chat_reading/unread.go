package chat_reading

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// MarkChatUnread 记录一个公开会话的手动未读边界。
type MarkChatUnread struct {
	// MethodPost 声明写入操作。
	ginx.MethodPost
	// ChatID 是原公开会话标识。
	ChatID string `in:"path" name:"chatId" validate:"required,len=32"`
	// Body 保存准确消息边界和已知代次。
	Body MarkChatUnreadRequest `in:"body"`
}

// MarkChatUnreadRequest 把阅读位置移动到指定公开消息之前。
type MarkChatUnreadRequest struct {
	// FromMessageID 是用户希望标为未读的首条公开消息。
	FromMessageID string `json:"fromMessageID" validate:"required,max=128"`
	// ObservedUnreadGeneration 是客户端观察到的手动未读代次。
	ObservedUnreadGeneration int64 `json:"observedUnreadGeneration" validate:"min=0"`
}

func (*MarkChatUnread) Path() string { return "/:chatId/unread" }
func (d *MarkChatUnread) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	if err := validateChat(request, d.ChatID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.ChatReadingControllerManager.MarkUnread(request, constants.LocalReaderID, d.ChatID, d.Body.FromMessageID, d.Body.ObservedUnreadGeneration); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// UnmarshalJSON 校验 HTTP 阅读命令的完整字段。
func (d *MarkChatUnreadRequest) UnmarshalJSON(raw []byte) error {
	type payload MarkChatUnreadRequest
	return decodeReadingCommand(raw, (*payload)(d))
}
