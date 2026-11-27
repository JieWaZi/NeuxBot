package main_conversations

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取主会话
type GetMainConversation struct {
	ginx.MethodGet
	// 主体类型
	SubjectType string `in:"path" name:"subjectType" validate:"required,oneof=waker team"`
	// 主体标识
	SubjectID string `in:"path" name:"subjectId" validate:"required,max=32"`
	// 消息游标
	BeforeSequence int64 `in:"query" name:"beforeSequence" validate:"min=0"`
	// 分页上限
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// 主会话消息
type MainConversationResponse struct {
	// 会话身份
	models.MainConversation
	// 消息列表
	Messages []MainConversationTimelineMessage `json:"messages"`
	// 活跃工作
	ActiveWork []models.MainConversationActivity `json:"activeWork"`
	// 更早记录
	HasMore bool `json:"hasMore"`
	// 上页游标
	NextBeforeSequence int64 `json:"nextBeforeSequence,omitempty"`
}

// 主会话发言
type MainConversationTimelineMessage struct {
	// 公开发言
	models.MainConversationMessage
	// 可读发言
	Readable bool `json:"readable,omitempty"`
	// 分支回合
	ForkExecutionID string `json:"forkExecutionId,omitempty"`
	// 历史提问
	Questions []messageview.MessageQuestion `json:"questions,omitempty"`
	// 失败原因码
	FailureCode string `json:"failureCode,omitempty"`
	// 原始正文
	InputContent *string `json:"inputContent,omitempty"`
	// 附件列表
	Attachments []eventlog.AttachmentReference `json:"attachments,omitempty"`
	// 仅附件
	AttachmentOnly bool `json:"attachmentOnly,omitempty"`
	// 代码引用
	CodeReferences []eventlog.CodeReference `json:"codeReferences,omitempty"`
	// 仅代码引用
	CodeReferenceOnly bool `json:"codeReferenceOnly,omitempty"`
}

// Path 返回主体的固定主会话地址。
func (*GetMainConversation) Path() string { return "/:subjectType/:subjectId" }

// Output 打开主体唯一主会话并分页读取已接纳的公开消息。
// @err[BadRequest][40000000001][请求参数无效]
// @err[WakerNotFound][40400000001][伙伴不存在]
// @err[ChannelNotFound][40400000012][团队不存在]
func (g *GetMainConversation) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	conversation, err := global.MainConversationService.Ensure(request, g.SubjectType, g.SubjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if global.ChatService != nil {
		if err := global.MainConversationService.RecoverUserAdmissions(request, conversation.ID, global.ChatService.ReadSubmission); err != nil {
			logx.Warn("reconcile_main_conversation_admissions: ", err)
		}
	}
	page := MainConversationResponse{MainConversation: conversation}
	var messages []models.MainConversationMessage
	var failures map[string]controllers.MainConversationFailureCandidate
	// 让公开发言和工作状态取自同一快照，避免新一轮的等待状态套到上一轮。
	err = global.TransactionControllerManager.Transaction(request, func(tx context.Context) error {
		rows, more, err := global.MainConversationService.Messages(tx, conversation.ID, g.BeforeSequence, pageSize(g.Limit))
		if err != nil {
			return err
		}
		active, err := global.MainConversationControllerManager.ActiveWork(tx, conversation.ID)
		if err != nil {
			return err
		}
		failures, err = global.MainConversationControllerManager.UnansweredFailures(tx, conversation.ID, rows)
		if err != nil {
			return err
		}
		messages, page.HasMore, page.ActiveWork = rows, more, active
		if more {
			page.NextBeforeSequence = messages[0].Sequence
		}
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	questions, err := resolvedMainQuestions(request, conversation.ID, messages)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	page.Messages = make([]MainConversationTimelineMessage, 0, len(messages))
	for _, row := range messages {
		message, err := timelineMessage(request, row)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		message.Questions = questions[row.SourceChatID][publicInputExecution(row)]
		if failure, exists := failures[row.ID]; exists {
			if err := message.addUnansweredFailure(request, failure); err != nil {
				return nil, ginx.WithStack(err)
			}
		}
		page.Messages = append(page.Messages, message)
	}
	return page, nil
}

func publicInputExecution(row models.MainConversationMessage) string {
	if row.Role != string(constants.MessageRoleUser) || row.SourceChatID == "" {
		return ""
	}
	if row.SourceExecutionID != "" {
		return row.SourceExecutionID
	}
	// 早期交办使用公开输入标识作为同轮执行标识。
	return row.ID
}

// resolvedMainQuestions 只读取当前公开页交办所属回合的历史提问，活动表单仍由原请求拥有。
func resolvedMainQuestions(ctx context.Context, conversationID string, rows []models.MainConversationMessage) (map[string]map[string][]messageview.MessageQuestion, error) {
	executions := make(map[string][]string)
	var candidateSources []string
	for _, row := range rows {
		execution := publicInputExecution(row)
		if execution == "" {
			continue
		}
		if _, exists := executions[row.SourceChatID]; !exists {
			candidateSources = append(candidateSources, row.SourceChatID)
		}
		executions[row.SourceChatID] = append(executions[row.SourceChatID], execution)
	}
	if len(candidateSources) == 0 {
		return nil, nil
	}
	owned, err := global.MainConversationControllerManager.OwnedWorkChatIDs(ctx, conversationID, candidateSources)
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string][]messageview.MessageQuestion, len(owned))
	for _, source := range owned {
		questions, err := global.ChatService.ReadResolvedExecutionQuestions(ctx, source, executions[source])
		if err != nil {
			if isUnavailableSourceHistory(err) {
				continue
			}
			return nil, err
		}
		result[source] = questions
	}
	return result, nil
}

func isUnavailableSourceHistory(err error) bool {
	for _, unavailable := range []status_error.StatusError{
		status_error.ChatNotFound, status_error.ExecutionNotFound, status_error.ChatLogError,
		status_error.ChatLogCorrupted, status_error.ChatLogIndexCorrupted,
	} {
		if util.HasStatusCode(err, unavailable) {
			return true
		}
	}
	return false
}

// addUnansweredFailure 核验原执行及交办身份后才公开失败码与来源头像。
func (m *MainConversationTimelineMessage) addUnansweredFailure(ctx context.Context, failure controllers.MainConversationFailureCandidate) error {
	outcome, err := global.ChatService.ReadExecutionOutcome(ctx, failure.ChatID, failure.ExecutionID)
	if err != nil {
		// 来源缺失或损坏时，主会话仍可读取已经保存的公开正文。
		if isUnavailableSourceHistory(err) {
			return nil
		}
		return err
	}
	execution := outcome.Execution
	if execution.Status == constants.ExecutionStatusFailed && execution.ChatID == failure.ChatID && execution.ID == failure.ExecutionID &&
		execution.SubmissionID != "" && m.ID == mainconversation.UserMessageID(m.ConversationID, execution.SubmissionID) {
		m.FailureCode = publicFailureCode(outcome)
		m.SourceWakerID = failure.WakerID
	}
	return nil
}

// timelineMessage 只读取接纳时在同一主会话内登记的原用户输入，内容缺失时仍提供公开摘要。
func timelineMessage(ctx context.Context, row models.MainConversationMessage) (MainConversationTimelineMessage, error) {
	result := MainConversationTimelineMessage{MainConversationMessage: row, Readable: row.IsReadablePublication()}
	if result.Readable {
		result.ForkExecutionID = row.SourceExecutionID
	}
	if row.Role != string(constants.MessageRoleUser) || row.SourceChatID == "" || row.SourceMessageID == "" {
		return result, nil
	}
	input, err := global.ChatService.PublicReadingMessage(ctx, row.SourceChatID, row.SourceMessageID)
	if isUnavailableSourceHistory(err) || util.HasStatusCode(err, status_error.ChatMessageNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	// 来源富内容不可用时保留已经公开的正文摘要。
	if input == nil {
		return result, nil
	}
	if input.ID != row.SourceMessageID || input.Role != constants.MessageRoleUser || input.Actor != string(constants.ChannelMessageActorUser) || input.Inherited {
		return result, nil
	}
	result.InputContent = &input.Content
	result.Attachments, result.AttachmentOnly = input.Attachments, input.AttachmentOnly
	result.CodeReferences, result.CodeReferenceOnly = input.CodeReferences, input.CodeReferenceOnly
	return result, nil
}

func pageSize(n int) int {
	if n == 0 {
		return 50
	}
	return n
}

// publicFailureCode 只允许本地化目录中的身份；工作日志里的原生文本及参数均留在来源中。
func publicFailureCode(outcome chat.ExecutionOutcome) string {
	key := outcome.Execution.ErrorCode
	if key == status_error.RuntimeExecutionFailed.Key() && outcome.ErrorParams["ModelUnavailable"] == true {
		return string(constants.RuntimeFailureCodeModelUnavailable)
	}
	if key != "" && key != status_error.RuntimeExecutionFailed.Key() {
		for code := range status_error.MessageTemplates()["en"] {
			if code.Key() == key {
				return key
			}
		}
	}
	for _, candidate := range []constants.RuntimeFailureCode{outcome.FailureCode, constants.RuntimeFailureCode(key)} {
		if candidate != constants.RuntimeFailureCodeUnspecified && candidate != constants.RuntimeFailureCodeExecutionFailed {
			if _, err := candidate.MarshalText(); err == nil {
				return string(candidate)
			}
		}
	}
	return status_error.RuntimeExecutionFailed.Key()
}
