package im

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListIMConversations 获取关联对话。
type ListIMConversations struct {
	ginx.MethodGet
	// 聊天授权标识
	BindingID string `in:"path" name:"bindingId" validate:"required"`
}

func (*ListIMConversations) Path() string { return "/bindings/:bindingId/conversations" }
func (o *ListIMConversations) Output(ctx *gin.Context) (interface{}, error) {
	values, err := global.IMControllerManager.Links(ctx, o.BindingID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources := make([]models.IMConversationLink, 0, len(values))
	for _, value := range values {
		if value.ChatID == "" {
			continue
		}
		chatIDs := []string{value.ChatID}
		if value.ThreadID != "" {
			_, err := global.ChannelThreadControllerManager.Get(ctx, value.ThreadID)
			if util.HasStatusCode(err, status_error.ChannelThreadNotFound) {
				continue
			}
			if err != nil {
				return nil, ginx.WithStack(err)
			}
			members, err := global.ChannelThreadControllerManager.ListMembers(ctx, value.ThreadID)
			if err != nil {
				return nil, ginx.WithStack(err)
			}
			chatIDs = nil
			for _, member := range members {
				chatIDs = append(chatIDs, member.ChatID)
			}
		}
		missing := false
		for _, chatID := range chatIDs {
			count, err := global.ChatService.CountUserMessages(ctx, chatID)
			if util.HasStatusCode(err, status_error.ChatNotFound) {
				missing = true
				break
			}
			if err != nil {
				return nil, ginx.WithStack(err)
			}
			value.TurnCount += count
		}
		if !missing && len(chatIDs) > 0 {
			resources = append(resources, value)
		}
	}
	return ListIMConversationsResponse{Resources: resources}, nil
}
