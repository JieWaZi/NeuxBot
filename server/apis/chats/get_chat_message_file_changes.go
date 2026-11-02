package chats

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 文件成果摘要
type GetChatMessageFileChanges struct {
	ginx.MethodGet
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 消息标识
	MessageID string `in:"path" name:"messageId" validate:"required"`
}

// Path 返回独立文件成果摘要的读取地址。
func (g *GetChatMessageFileChanges) Path() string {
	return "/:chatId/messages/:messageId/file-changes/summary"
}

// Output 按原消息归属读取有界摘要，不加载 CLI 工具历史。
// [BadRequest][40000000001][请求参数无效]
// [ChatNotFound][40400000005][对话不存在]
// [ChatMessageFileChangeNotFound][40400000011][消息中的文件改动不存在]
func (g *GetChatMessageFileChanges) Output(ctx *gin.Context) (interface{}, error) {
	value, err := global.ChatService.GetMessageFileChanges(ctx, g.ChatID, g.MessageID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
