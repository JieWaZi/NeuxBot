package chats

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 文件变更
type GetChatMessageFileChange struct {
	ginx.MethodGet
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 消息标识
	MessageID string `in:"path" name:"messageId" validate:"required"`
	// 文件路径
	FilePath string `in:"query" name:"path" validate:"required"`
}

// Path 返回消息执行级文件 Diff 的接口路径。
func (g *GetChatMessageFileChange) Path() string {
	return "/:chatId/messages/:messageId/file-changes"
}

// Output 从 Chat 权威 JSONL 返回执行级文件 Diff。
// [BadRequest][40000000001][请求参数无效]
// [ChatNotFound][40400000005][对话不存在]
// [ChatMessageFileChangeNotFound][40400000011][消息中的文件改动不存在]
// [ChatLogError][50000000004][Chat 权威日志读写失败]
func (g *GetChatMessageFileChange) Output(ctx *gin.Context) (interface{}, error) {
	diff, err := global.ChatService.GetMessageFileChange(
		ctx,
		g.ChatID,
		g.MessageID,
		g.FilePath,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return diff, nil
}
