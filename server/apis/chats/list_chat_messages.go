package chats

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 消息列表
type ListChatMessages struct {
	ginx.MethodGet
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 消息游标
	BeforeSequence int64 `in:"query" name:"beforeSequence"`
	// 分页上限
	Limit int `in:"query" name:"limit"`
}

// Path 返回 Chat 消息历史接口路径。
func (l *ListChatMessages) Path() string {
	return "/:chatId/messages"
}

// Validate 校验消息创建游标和窗口上限。
func (l *ListChatMessages) Validate(*gin.Context) error {
	if l.BeforeSequence < 0 || l.Limit < 0 || l.Limit > 100 {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.Limit == 0 {
		l.Limit = 50
	}
	return nil
}

// Output 返回不需要完整重放 JSONL 的消息历史窗口。
// [BadRequest][40000000001][请求参数无效]
// [ChatLogError][50000000004][Chat 权威日志读写失败]
func (l *ListChatMessages) Output(ctx *gin.Context) (interface{}, error) {
	page, err := global.ChatService.ReadMessageHistory(
		ctx,
		l.ChatID,
		l.BeforeSequence,
		l.Limit,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return page, nil
}
