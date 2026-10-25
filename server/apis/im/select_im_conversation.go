package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// SelectIMConversationRequest 对话选择请求。
type SelectIMConversationRequest struct {
	// 对话标识
	LinkID string `json:"linkId" validate:"max=32"`
}

// SelectIMConversation 切换关联对话。
type SelectIMConversation struct {
	ginx.MethodPost
	// 聊天授权身份
	BindingID string `in:"path" name:"bindingId" validate:"required"`
	// 对话选择
	Body SelectIMConversationRequest `in:"body"`
}

func (*SelectIMConversation) Path() string { return "/bindings/:bindingId/conversation" }
func (o *SelectIMConversation) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.IMService.SelectConversation(ctx, o.BindingID, o.Body.LinkID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
