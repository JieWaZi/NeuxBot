package review

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListChatSkillSuggestions 获取技能学习建议和已提交的聊天资源变更。
type ListChatSkillSuggestions struct {
	ginx.MethodGet
	// 聊天标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 窗口最早的资源变更游标
	FromSequence int64 `in:"query" name:"fromSequence"`
	// 窗口最早的建议时间
	FromTime int64 `in:"query" name:"fromTime"`
}

// Path 返回原聊天的建议列表入口。
func (*ListChatSkillSuggestions) Path() string { return "/chats/:chatId/suggestions" }

// Validate 校验当前聊天窗口的下界。
func (o *ListChatSkillSuggestions) Validate(*gin.Context) error {
	if o.FromSequence < 0 || o.FromTime < 0 {
		return ginx.WithStack(status_error.BadRequest)
	}
	return nil
}

// Output 读取当前窗口的非阻塞建议和已保存结果。
func (o *ListChatSkillSuggestions) Output(ctx *gin.Context) (interface{}, error) {
	if global.ChatService == nil {
		return nil, ginx.WithStack(status_error.ChatServiceNotStarted)
	}
	view := SkillSuggestionsResponse{Items: []SkillSuggestionResponse{}}
	if global.ReviewWorker != nil {
		values, err := global.ReviewWorker.ChatSuggestions(ctx, o.ChatID, o.FromTime)
		if util.HasStatusCode(err, status_error.ChatNotFound) {
			return nil, ginx.WithStack(err)
		}
		if err != nil {
			logx.Warnf("read review suggestions: chat_id=%s error=%v", o.ChatID, err)
			view.SuggestionsUnavailable = true
		}
		for _, value := range values {
			view.Items = append(view.Items, suggestionView(value))
		}
	}
	if global.ReviewWorker == nil || view.SuggestionsUnavailable {
		item, err := global.ChatService.LoadExecutionChat(ctx, o.ChatID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if item.HiddenFromWakerHistory {
			return nil, ginx.WithStack(status_error.ChatNotFound)
		}
	}
	changes, err := global.ChatService.ReadManagedResourceChanges(ctx, o.ChatID, o.FromSequence)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	for _, change := range changes {
		view.Changes = append(view.Changes, ManagedResourceChangeResponse{Action: change.Action, SkillID: change.SkillID,
			ConnectorID: change.ConnectorID, OwnerWakerID: change.OwnerWakerID,
			VersionID: change.VersionID, Name: change.Name, Sequence: change.Sequence, OccurredAt: change.OccurredAt})
	}
	return view, nil
}
