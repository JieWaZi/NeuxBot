package chats

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 结构化输入请求
type ResolveElicitationRequest struct {
	// 表单内容
	Content map[string]any `json:"content"`
	// 是否拒绝本次输入
	Declined bool `json:"declined"`
}

// 处理结构化输入
type ResolveElicitation struct {
	ginx.MethodPut
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 结构化输入标识
	ElicitationID string `in:"path" name:"elicitationId" validate:"required"`
	// 请求体
	Body ResolveElicitationRequest `in:"body"`
}

// Path 返回结构化输入处理接口路径。
func (r *ResolveElicitation) Path() string {
	return "/:chatId/elicitations/:elicitationId"
}

// Validate 规范化并校验路径与请求体。
func (r *ResolveElicitation) Validate(*gin.Context) error {
	r.ChatID = strings.TrimSpace(r.ChatID)
	r.ElicitationID = strings.TrimSpace(r.ElicitationID)
	if r.ChatID == "" || r.ElicitationID == "" {
		return ginx.WithStack(status_error.BadRequest)
	}
	if !r.Body.Declined && r.Body.Content == nil {
		return ginx.WithStack(status_error.BadRequest)
	}
	return nil
}

// Output 把用户提交交给 Execution Core 结构化输入协调器。
func (r *ResolveElicitation) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.ExecutionService.ResolveElicitation(
		ctx,
		r.ChatID,
		r.ElicitationID,
		r.Body.Content,
		r.Body.Declined,
	); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
