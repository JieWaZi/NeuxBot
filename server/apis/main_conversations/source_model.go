package main_conversations

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 设置工作来源模型
type SetMainConversationSourceModel struct {
	ginx.MethodPatch
	// 主体类型
	SubjectType string `in:"path" name:"subjectType" validate:"required,oneof=waker team"`
	// 主体标识
	SubjectID string `in:"path" name:"subjectId" validate:"required,max=32"`
	// 来源对话
	WorkChatID string `in:"path" name:"workChatId" validate:"required,max=32"`
	// 模型选择
	Body MainConversationSourceModelInput `in:"body"`
}

// 工作来源模型选择
type MainConversationSourceModelInput struct {
	// 模型标识
	Model string `json:"model" validate:"required,max=500"`
}

// 工作来源模型设置回执
type MainConversationSourceModelAcceptance struct {
	// 已保存模型
	SelectedModel string `json:"selectedModel"`
	// 模型变更
	Change *chat.MainConversationSourceModelChange `json:"change,omitempty"`
}

func (*SetMainConversationSourceModel) Path() string {
	return "/:subjectType/:subjectId/sources/:workChatId/model"
}

// Output 在已授权来源保存下一次输入默认使用的模型。
// @err[BadRequest][40000000001][模型选择无效]
// @err[ChatNotFound][40400000005][工作对话不存在]
// @err[ChatLogError][50000000004][工作记录不可读]
func (op *SetMainConversationSourceModel) Output(ctx *gin.Context) (interface{}, error) {
	// 团队工作来源沿用原成员固定配置。
	if op.SubjectType != "waker" {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	var response MainConversationSourceModelAcceptance
	err := resolveSourceInteraction(ctx.Request.Context(), op.SubjectType, op.SubjectID, op.WorkChatID, func(ctx context.Context) error {
		selected, change, err := global.ChatService.SetMainConversationSourceModel(ctx, op.WorkChatID, op.Body.Model)
		response = MainConversationSourceModelAcceptance{SelectedModel: selected, Change: change}
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return response, nil
}

func sourceModelDefault(ctx context.Context, source models.Chat, options execution.WorkOptions) (string, error) {
	if source.LastRuntimeID == "" || options.RuntimeID != source.LastRuntimeID {
		return "", nil
	}
	account := source.LastAccountID
	if account == "" {
		account = constants.AiAccountSystemID
	}
	if options.AccountID != "" && options.AccountID != account {
		return "", nil
	}
	return global.ChatService.ReadMainConversationSourceModel(ctx, source)
}
