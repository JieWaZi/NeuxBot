package chats

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新对话请求
type UpdateChatRequest struct {
	// 标题
	Title string `json:"title" validate:"required,max=120"`
}

// 更新对话
type UpdateChat struct {
	ginx.MethodPut
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 请求体
	Body UpdateChatRequest `in:"body"`
}

// Path 返回 Chat 更新接口路径。
func (u *UpdateChat) Path() string {
	return "/:chatId"
}

// Output 追加标题事件并刷新查询索引。
// [BadRequest][40000000001][请求参数无效]
// [ChatNotFound][40400000005][Chat 不存在]
// [ChatLogError][50000000004][Chat 权威日志读写失败]
func (u *UpdateChat) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.ChatService.RenameChat(
		ctx,
		u.ChatID,
		u.Body.Title,
	); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
