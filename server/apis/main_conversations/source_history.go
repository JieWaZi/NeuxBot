package main_conversations

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取来源记录
type GetMainConversationSource struct {
	ginx.MethodGet
	// 主体类型
	SubjectType string `in:"path" name:"subjectType" validate:"required,oneof=waker team"`
	// 主体标识
	SubjectID string `in:"path" name:"subjectId" validate:"required,max=32"`
	// 来源对话
	WorkChatID string `in:"path" name:"workChatId" validate:"required,max=32"`
	// 消息游标
	BeforeSequence int64 `in:"query" name:"beforeSequence" validate:"min=0"`
	// 分页上限
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// 工作来源记录
type MainConversationSourceResponse struct {
	// 来源摘要
	Chat models.Chat `json:"chat"`
	// 消息列表
	chat.MessageHistoryPage
	// 已发布发言
	PublishedMessages []MainConversationSourcePublication `json:"publishedMessages,omitempty"`
}

// 来源公开发言
type MainConversationSourcePublication struct {
	// 发言标识
	ID string `json:"id"`
	// 执行标识
	ExecutionID string `json:"executionId"`
	// 发言正文
	Content string `json:"content"`
}

// Path 返回限定在原主体中的工作会话历史路径。
func (*GetMainConversationSource) Path() string {
	return "/:subjectType/:subjectId/sources/:workChatId/messages"
}

// Output 按公开主体和来源绑定验证工作记录，再读取其原始执行状态、目录与历史。
// @err[ChatNotFound][40400000005][工作对话不存在]
// @err[ChatLogError][50000000004][工作记录不可读]
func (g *GetMainConversationSource) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	source, work, err := sourceChat(request, g.SubjectType, g.SubjectID, g.WorkChatID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	page, err := global.ChatService.ReadMessageTimeline(request, source.ID, g.BeforeSequence, 0, pageSize(g.Limit))
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if source.WaitingApproval || source.WaitingElicitation || len(source.PendingApprovals) != 0 || len(source.PendingElicitations) != 0 {
		err := global.MainConversationService.AuthorizeWorkActor(request, source)
		if err != nil && !util.HasStatusCode(err, status_error.AgentOperationForbidden) {
			return nil, ginx.WithStack(err)
		}
		if err != nil {
			// 原日志保留请求记录；已撤销的来源不再提供可答复的表单。
			source.WaitingApproval, source.WaitingElicitation = false, false
			source.PendingApprovals, source.PendingElicitations = nil, nil
		}
	}
	response := MainConversationSourceResponse{Chat: source, MessageHistoryPage: page}
	var executionIDs []string
	for _, message := range page.Resources {
		if message.Role == constants.MessageRoleAssistant && message.ExecutionID != "" && !message.Inherited && message.Question == nil {
			executionIDs = append(executionIDs, message.ExecutionID)
		}
	}
	if len(executionIDs) == 0 {
		return response, nil
	}
	publications, err := global.MainConversationControllerManager.WorkPublications(request, work, executionIDs)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	for _, publication := range publications {
		response.PublishedMessages = append(response.PublishedMessages, MainConversationSourcePublication{
			ID: publication.ID, ExecutionID: publication.SourceExecutionID, Content: publication.Content,
		})
	}
	return response, nil
}

func sourceChat(request context.Context, kind, id, chatID string) (models.Chat, models.MainConversationWork, error) {
	work, err := global.MainConversationService.Source(request, kind, id, chatID)
	if err != nil {
		return models.Chat{}, work, err
	}
	summary, err := global.ChatService.GetChatSummary(request, work.WorkChatID)
	if err != nil {
		return models.Chat{}, work, err
	}
	if !summary.Chat.HiddenFromWakerHistory || summary.Chat.WakerID != work.WakerID {
		return models.Chat{}, work, status_error.ChatNotFound.WithParams(map[string]interface{}{"ChatID": chatID})
	}
	return summary.Chat, work, nil
}
