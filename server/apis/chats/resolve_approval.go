package chats

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 审批请求
type ResolveApprovalRequest struct {
	// 选项标识
	OptionID string `json:"optionId" validate:"required,max=512"`
}

// 处理审批
type ResolveApproval struct {
	ginx.MethodPut
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 审批标识
	ApprovalID string `in:"path" name:"approvalId" validate:"required"`
	// 请求体
	Body ResolveApprovalRequest `in:"body"`
}

// Path 返回审批处理接口路径。
func (r *ResolveApproval) Path() string {
	return "/:chatId/approvals/:approvalId"
}

// Validate 规范化并校验路径与选项标识。
func (r *ResolveApproval) Validate(*gin.Context) error {
	r.ChatID = strings.TrimSpace(r.ChatID)
	r.ApprovalID = strings.TrimSpace(r.ApprovalID)
	r.Body.OptionID = strings.TrimSpace(r.Body.OptionID)
	if r.ChatID == "" || r.ApprovalID == "" || r.Body.OptionID == "" {
		return ginx.WithStack(status_error.BadRequest)
	}
	return nil
}

// Output 把用户选择交给 Execution Core 审批协调器。
func (r *ResolveApproval) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.ExecutionService.ResolveApproval(
		ctx,
		r.ChatID,
		r.ApprovalID,
		r.Body.OptionID,
	); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
