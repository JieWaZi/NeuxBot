package chats

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 事件列表
type ListChatEvents struct {
	ginx.MethodGet
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 恢复游标
	After int64 `in:"query" name:"after"`
}

// 事件列表响应
type ListChatEventsResponse struct {
	// 资源列表
	Resources []chat.PublicEvent `json:"resources"`
}

// Path 返回对话事件历史接口路径。
func (l *ListChatEvents) Path() string {
	return "/:chatId/events/history"
}

// Validate 校验恢复游标。
func (l *ListChatEvents) Validate(*gin.Context) error {
	if l.After < 0 {
		return ginx.WithStack(status_error.BadRequest)
	}
	return nil
}

// Output 返回游标之后的安全公开事件。
// [ChatLogError][50000000004][Chat 权威日志读写失败]
func (l *ListChatEvents) Output(ctx *gin.Context) (interface{}, error) {
	resources, err := global.ChatService.ListEvents(
		ctx,
		l.ChatID,
		l.After,
		1000,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListChatEventsResponse{Resources: resources}, nil
}
