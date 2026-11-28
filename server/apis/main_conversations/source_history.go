package main_conversations

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
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
	// 只读回件
	PublicationID string `in:"query" name:"publicationId" validate:"max=32"`
	// 消息游标
	BeforeSequence int64 `in:"query" name:"beforeSequence" validate:"min=0"`
	// 分页上限
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// 工作来源记录
type MainConversationSourceResponse struct {
	// 输入默认留在当前来源
	PrivateContinuation bool `json:"privateContinuation,omitempty"`
	// 已保存模型
	SelectedModel string `json:"selectedModel,omitempty"`
	// 本页模型变更
	ModelChanges []chat.MainConversationSourceModelChange `json:"modelChanges,omitempty"`
	// 仅在此来源的待处理输入
	PendingInputs []MainConversationQueuedInput `json:"pendingInputs,omitempty"`
	// 来源摘要
	Chat models.Chat `json:"chat"`
	// 消息列表
	chat.MessageHistoryPage
	// 已发布发言
	PublishedMessages []MainConversationSourcePublication `json:"publishedMessages,omitempty"`
	// 主会话原输入
	InputTriggers []MainConversationSourceTrigger `json:"inputTriggers,omitempty"`
}

// 主会话原输入
type MainConversationSourceTrigger struct {
	// 来源消息标识
	SourceMessageID string `json:"sourceMessageId"`
	// 公开消息标识
	PublicMessageID string `json:"publicMessageId,omitempty"`
	// 原话摘要
	Preview string `json:"preview"`
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
	if g.PublicationID != "" {
		response, err := g.readReportSource(request)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		return response, nil
	}
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
	response := MainConversationSourceResponse{Chat: source, MessageHistoryPage: page, PrivateContinuation: true}
	if g.SubjectType == "waker" && source.LastRuntimeID != "" {
		before := g.BeforeSequence
		if before == 0 {
			before = page.LatestSequence + 1
			response.SelectedModel, err = global.ChatService.ReadMainConversationSourceModel(request, source)
			if err != nil {
				return nil, ginx.WithStack(err)
			}
		}
		response.ModelChanges, err = global.ChatService.ReadMainConversationSourceModelChanges(request, source.ID, page.NextBeforeSequence, before)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	response.InputTriggers, err = readSourceInputTriggers(request, work, page.Resources)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if g.BeforeSequence == 0 && global.MainConversationInputQueue != nil {
		response.PendingInputs, err = readPendingMainInputs(request, work.ConversationID, work.WorkChatID, nil)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	var executionIDs []string
	for _, message := range page.Resources {
		if message.Role == constants.MessageRoleAssistant && message.ExecutionID != "" && !message.Inherited && message.Question == nil {
			executionIDs = append(executionIDs, message.ExecutionID)
		}
	}
	if len(executionIDs) == 0 {
		return response, nil
	}
	publications, err := global.MainConversationService.WorkPublications(request, work, executionIDs)
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

func readSourceInputTriggers(ctx context.Context, work models.MainConversationWork, messages []messageview.Message) ([]MainConversationSourceTrigger, error) {
	ids := make([]string, 0, len(messages))
	bySource := make(map[string]string)
	for _, message := range messages {
		if message.Role != constants.MessageRoleUser || message.Inherited || message.ID == "" || (message.Actor != "" && message.Actor != "user") {
			continue
		}
		id := message.MainConversationTriggerID
		if id == "" && message.SubmissionID != "" {
			id = mainconversation.UserMessageID(work.ConversationID, message.SubmissionID)
		}
		if id == "" {
			id = message.ID
		}
		ids = append(ids, id)
		bySource[message.ID] = id
	}
	rows, err := global.MainConversationService.WorkInputTriggers(ctx, work, ids)
	if err != nil {
		return nil, err
	}
	byPublic := make(map[string]models.MainConversationMessage, len(rows))
	for _, row := range rows {
		byPublic[row.ID] = row
	}
	var triggers []MainConversationSourceTrigger
	for _, message := range messages {
		id := bySource[message.ID]
		if id == "" {
			continue
		}
		row := byPublic[id]
		// 旧记录没有入口标记。公开输入必须仍与当前来源输入身份或接纳身份相符。
		linked := row.ID != "" && (row.SourceMessageID == message.ID ||
			(row.SourceMessageID == "" && (row.ID == message.ID ||
				(row.AdmissionSubmissionID != "" && row.AdmissionSubmissionID == message.SubmissionID))))
		if !linked && message.MainConversationTriggerID == "" {
			continue
		}
		trigger := MainConversationSourceTrigger{SourceMessageID: message.ID}
		text := message.Content
		if linked && !row.AdmissionPending && row.RemovedAt == 0 {
			trigger.PublicMessageID = row.ID
			text = row.Content
		}
		trigger.Preview = sourceTriggerPreview(text)
		triggers = append(triggers, trigger)
	}
	return triggers, nil
}

func sourceTriggerPreview(text string) string {
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		runes := []rune(line)
		if len(runes) > 200 {
			return string(runes[:199]) + "…"
		}
		return line
	}
	return ""
}

func (g *GetMainConversationSource) readReportSource(ctx context.Context) (MainConversationSourceResponse, error) {
	var empty MainConversationSourceResponse
	work, publication, err := global.MainConversationService.ReadOnlyReportSource(ctx, g.SubjectType, g.SubjectID, g.WorkChatID, g.PublicationID)
	if err != nil {
		return empty, err
	}
	authorized, err := global.WorkTicketService.AuthorizesReportSource(ctx, publication)
	if err != nil {
		return empty, err
	}
	notFound := status_error.ChatNotFound.WithParams(map[string]interface{}{"ChatID": g.WorkChatID})
	if !authorized {
		return empty, notFound
	}
	if _, err := authorizedSourceSummary(ctx, work); err != nil {
		return empty, err
	}
	execution, err := global.ChatService.ReadExecution(ctx, work.WorkChatID, publication.SourceExecutionID)
	if err != nil {
		return empty, err
	}
	response := MainConversationSourceResponse{
		Chat:               models.Chat{ID: work.WorkChatID, WakerID: work.WakerID},
		MessageHistoryPage: chat.MessageHistoryPage{Resources: make([]messageview.Message, 0, 2)},
		PublishedMessages: []MainConversationSourcePublication{
			{ID: publication.ID, ExecutionID: execution.ID, Content: publication.Content},
		},
	}
	for _, selected := range []struct {
		// id 是处于回件授权范围内的原消息。
		id string
		// role 区分原执行的实际输入和输出。
		role constants.MessageRole
	}{{execution.UserMessageID, constants.MessageRoleUser}, {execution.ID, constants.MessageRoleAssistant}} {
		message, err := global.ChatService.PublicReadingMessage(ctx, work.WorkChatID, selected.id)
		if err != nil {
			return empty, err
		}
		if message == nil || message.ID != selected.id || message.Role != selected.role || message.Inherited ||
			(selected.role == constants.MessageRoleAssistant && message.ExecutionID != execution.ID) {
			return empty, notFound
		}
		// 只公开本次执行的权威消息。来源文件、分叉和交互继续受主体授权约束。
		message.Fork, message.FileChangeRecord, message.ReplyTo = nil, nil, nil
		message.Attachments, message.CodeReferences = nil, nil
		response.Resources = append(response.Resources, *message)
	}
	return response, nil
}

func sourceChat(request context.Context, kind, id, chatID string) (models.Chat, models.MainConversationWork, error) {
	work, err := global.MainConversationService.Source(request, kind, id, chatID)
	if err != nil {
		return models.Chat{}, work, err
	}
	summary, err := authorizedSourceSummary(request, work)
	return summary, work, err
}

// authorizedSourceSummary 核对权威 Chat 的隐藏来源归属与业务绑定一致。
func authorizedSourceSummary(ctx context.Context, work models.MainConversationWork) (models.Chat, error) {
	summary, err := global.ChatService.GetChatSummary(ctx, work.WorkChatID)
	if err != nil {
		return models.Chat{}, err
	}
	if !summary.Chat.HiddenFromWakerHistory || summary.Chat.WakerID != work.WakerID {
		return models.Chat{}, status_error.ChatNotFound.WithParams(map[string]interface{}{"ChatID": work.WorkChatID})
	}
	return summary.Chat, nil
}
