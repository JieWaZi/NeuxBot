package review

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ResolveSkillSuggestionInput 保存或忽略技能建议。
type ResolveSkillSuggestionInput struct {
	// 操作决定
	Action string `json:"action" validate:"required,oneof=save ignore"`
	// 修订确认凭据
	ApprovalToken string `json:"approvalToken,omitempty" validate:"omitempty,len=64,hexadecimal"`
}

// ResolveChatSkillSuggestion 处理聊天技能建议。
type ResolveChatSkillSuggestion struct {
	ginx.MethodPost
	// 聊天标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 建议标识
	SuggestionID string `in:"path" name:"suggestionId" validate:"required"`
	// 请求体
	Body ResolveSkillSuggestionInput `in:"body"`
}

// Path 返回来源聊天的明确决策入口。
func (*ResolveChatSkillSuggestion) Path() string {
	return "/chats/:chatId/suggestions/:suggestionId/resolve"
}

// Output 保存用户决定及新的可见状态。
func (o *ResolveChatSkillSuggestion) Output(ctx *gin.Context) (interface{}, error) {
	if global.ReviewWorker == nil {
		return nil, ginx.WithStack(status_error.RuntimeUnavailable)
	}
	v, err := global.ReviewWorker.ResolveSuggestion(ctx, o.ChatID, o.SuggestionID, o.Body.Action, o.Body.ApprovalToken)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return suggestionView(v), nil
}
