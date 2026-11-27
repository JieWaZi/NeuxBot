package main_conversations

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"strings"
)

// 处理来源补充输入
type ResolveMainConversationSourceElicitation struct {
	ginx.MethodPut
	// 主体类型
	SubjectType string `in:"path" name:"subjectType" validate:"required,oneof=waker team"`
	// 主体标识
	SubjectID string `in:"path" name:"subjectId" validate:"required,max=32"`
	// 工作对话
	WorkChatID string `in:"path" name:"workChatId" validate:"required,max=32"`
	// 补充输入标识
	ElicitationID string `in:"path" name:"elicitationId" validate:"required"`
	// 补充输入答复
	Body chats.ResolveElicitationRequest `in:"body"`
}

func (*ResolveMainConversationSourceElicitation) Path() string {
	return "/:subjectType/:subjectId/sources/:workChatId/elicitations/:elicitationId"
}

func (op *ResolveMainConversationSourceElicitation) Validate(*gin.Context) error {
	op.ElicitationID = strings.TrimSpace(op.ElicitationID)
	if op.ElicitationID == "" {
		return status_error.BadRequest
	}
	if !op.Body.Declined && op.Body.Content == nil {
		return status_error.BadRequest
	}
	return nil
}

// Output 将当前来源的用户答复交给产生该请求的执行。
// @err[DatabaseOperationCancelled][40800001001][数据库操作已取消]
// @err[DatabaseError][50000000002][数据库操作失败]
// @err[DatabaseBusy][50300001001][数据库暂忙]
func (op *ResolveMainConversationSourceElicitation) Output(ctx *gin.Context) (interface{}, error) {
	err := resolveSourceInteraction(ctx.Request.Context(), op.SubjectType, op.SubjectID, op.WorkChatID, func(ctx context.Context) error {
		return global.ExecutionService.ResolveElicitation(ctx, op.WorkChatID, op.ElicitationID, op.Body.Content, op.Body.Declined)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
