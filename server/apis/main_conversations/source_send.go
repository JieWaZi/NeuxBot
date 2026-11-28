package main_conversations

import (
	"context"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 发送工作来源消息
type SendMainConversationSourceMessage struct {
	ginx.MethodPost
	// 主体类型
	SubjectType string `in:"path" name:"subjectType" validate:"required,oneof=waker team"`
	// 主体标识
	SubjectID string `in:"path" name:"subjectId" validate:"required,max=32"`
	// 来源对话
	WorkChatID string `in:"path" name:"workChatId" validate:"required,max=32"`
	// 会话切换
	SwitchSession bool `in:"query" name:"switchSession"`
	// 来源消息
	Body chats.SendMessageRequest `in:"body"`
}

// 来源消息接纳回执
type MainConversationSourceAcceptance struct {
	// 来源对话
	Chat *models.Chat `json:"chat,omitempty"`
	// 开始的执行
	Execution *models.Execution `json:"execution,omitempty"`
	// 重放已接纳执行
	Replayed bool `json:"replayed,omitempty"`
	// 等待交给来源的输入
	QueuedInput *MainConversationQueuedInput `json:"queuedInput,omitempty"`
}

func (*SendMainConversationSourceMessage) Path() string {
	return "/:subjectType/:subjectId/sources/:workChatId/messages"
}

// Output 在原工作范围续聊，来源内输入只在原 Chat 接纳。
// @err[ChatNotFound][40400000005][工作对话不存在]
// @err[BadRequest][40000000001][输入无效]
// @err[ChatReplyTargetUnavailable][40400007202][引用的消息不可用]
// @err[CreateRequestConflict][40900000023][同一提交的请求内容不同]
// @err[WorkTicketCannotContinue][40900007211][工单当前无法继续]
// @err[WorkTicketAttemptUnavailable][40900007212][这轮执行已无权更新工单]
// @err[RuntimeSessionStartFailed][50000000054][无法新建智能体会话]
// @err[RuntimeSessionRestoreFailed][50000000055][无法恢复已有的智能体会话]
// @err[DatabaseOperationCancelled][40800001001][数据库操作已取消]
// @err[DatabaseError][50000000002][数据库操作失败]
// @err[DatabaseBusy][50300001001][数据库暂忙]
func (op *SendMainConversationSourceMessage) Output(httpCtx *gin.Context) (interface{}, error) {
	executionContext := httpCtx.Request.Context()
	input, selected := op.Body.WorkInput()
	if input.SubmissionID == "" || (op.SwitchSession &&
		(op.SubjectType != "waker" || !op.Body.StartNewSession || input.SteerExecutionID != "")) {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	if op.SwitchSession {
		selected.HistorySourceChatID = op.WorkChatID
	}
	releaseSubmission, err := acquireMainSubmission(executionContext, mainSubmissionKey{kind: op.SubjectType, subject: op.SubjectID, submission: input.SubmissionID})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer releaseSubmission()
	ctx, releaseTeam, err := lockMainConversationTeam(executionContext, op.SubjectType, op.SubjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	releaseTeam = sync.OnceFunc(releaseTeam)
	defer releaseTeam()
	source, work, err := sourceChat(ctx, op.SubjectType, op.SubjectID, op.WorkChatID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	content, err := publicInputContent(input)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	request := []any{execution.MainConversationSourceEntryPoint, work.WorkChatID, input, selected}
	if op.Body.StartWork {
		request = append(request, map[string]bool{"startWork": true})
	}
	identity, err := requestidempotency.NewSubmission(input.SubmissionID, request)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	queued, err := replayMainConversationInput(ctx, work.ConversationID, identity)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if queued != nil {
		return MainConversationSourceAcceptance{QueuedInput: queued}, nil
	}
	publicID := mainconversation.UserMessageID(work.ConversationID, input.SubmissionID)
	sourceMessageID := richSourceMessageID(publicID, input)
	var ticketReplyID string
	if !op.Body.StartWork && input.ReplyToMessageID == "" && input.Workflow == nil && input.SteerExecutionID == "" {
		ticketReplyID = mainconversation.RoutedWorkTicketID(work.ScopeKey)
	}
	saved, err := global.MainConversationService.Message(ctx, work.ConversationID, publicID)
	if err == nil {
		if saved.SourceChatID != source.ID || saved.InputDigest != identity.Hash {
			return nil, ginx.WithStack(status_error.CreateRequestConflict)
		}
		accepted, err := global.ChatService.ReadSubmission(ctx, source.ID, input.SubmissionID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if accepted != nil {
			if saved.AdmissionPending {
				if _, err := global.MainConversationService.ConfirmUserReceipt(ctx, saved, *accepted); err != nil {
					return nil, ginx.WithStack(err)
				}
				if global.WorkTicketService != nil && (op.Body.StartWork || input.ReplyToMessageID != "" || ticketReplyID != "") {
					if err := global.WorkTicketService.ConfirmExecution(ctx, accepted.ID, accepted.Status); err != nil {
						return nil, ginx.WithStack(err)
					}
				}
			}
			return MainConversationSourceAcceptance{Chat: &source, Execution: accepted, Replayed: true}, nil
		}
		sourceMessageID = saved.SourceMessageID
	} else if !util.HasStatusCode(err, status_error.ChatMessageNotFound) {
		return nil, ginx.WithStack(err)
	}
	assignment := SendMainConversationMessage{SubjectType: op.SubjectType, SubjectID: op.SubjectID}
	_, options, err := assignment.actorOptions(ctx, source.WakerID, &selected)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if op.SubjectType == "waker" && options.Model == "" {
		options.Model, err = sourceModelDefault(ctx, source, options)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	executionID := identity.ResourceID("main-conversation-source", source.ID)
	private := !op.Body.StartWork && saved.ID == ""
	if global.MainConversationInputQueue != nil && saved.ID == "" && input.SteerExecutionID == "" {
		if input.ReplyToMessageID != "" {
			if op.Body.StartWork {
				return nil, ginx.WithStack(status_error.BadRequest)
			}
			if err := validateQueuedSourceReply(ctx, work, input.ReplyToMessageID); err != nil {
				return nil, ginx.WithStack(err)
			}
		}
		queued, err := enqueueMainConversationInput(ctx, httpCtx, options, models.MainConversationInput{
			ConversationID: work.ConversationID, WorkChatID: source.ID, PublicMessageID: publicID,
			ExecutionID: executionID, SourceMessageID: sourceMessageID, TicketReplyID: ticketReplyID,
			StartWork: op.Body.StartWork, EntryPoint: execution.MainConversationSourceEntryPoint, PrivateSource: private,
			PublicContent: content, RequestHash: identity.Hash, Input: input,
		})
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		return MainConversationSourceAcceptance{QueuedInput: queued}, nil
	}
	options.EntryPoint = execution.MainConversationSourceEntryPoint
	targetPublicID := publicID
	if private {
		targetPublicID = ""
		input.ContextInstructions = append(input.ContextInstructions, mainconversation.PrivateSourceInstruction())
	}
	options.ToolCapabilities = execution.MainConversationTools(source.ID, executionID, targetPublicID, global.MainConversationService)
	intent := execution.WorkIntent{ExecutionID: executionID, UserMessageID: sourceMessageID, Chat: execution.ChatTarget{ChatID: source.ID}, Input: input, Options: options, Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
		Ticket: execution.WorkTicketInput{Start: op.Body.StartWork, Address: workticket.Address{Kind: constants.WorkTicketOriginMain, ID: work.ConversationID},
			OwnerKind: op.SubjectType, OwnerID: op.SubjectID, PublicMessageID: publicID, PublicReplyID: ticketReplyID, MainConversation: global.MainConversationService},
		AdmissionBoundary: mainConversationAdmission(op.SubjectType, op.SubjectID, work, false, func(ctx context.Context, commit func(context.Context) error) error {
			if private {
				return commit(ctx)
			}
			_, err := global.MainConversationService.AdmitUser(ctx, op.SubjectType, op.SubjectID, input.SubmissionID, content, "", source.ID, executionID, sourceMessageID, "", identity.Hash, commit)
			return err
		}),
		ClaimAttachments: func(files []execution.AttachmentInput) (func(bool), error) {
			return uploads.ClaimWork(httpCtx, &execution.WorkInput{SubmissionID: input.SubmissionID, Attachments: files})
		},
	}
	releaseTeam()
	result, err := global.ExecutionService.Start(executionContext, intent)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if saved.AdmissionPending && result.Replayed {
		if _, err := global.MainConversationService.ConfirmUserReceipt(ctx, saved, result.Execution); err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	return MainConversationSourceAcceptance{Chat: &result.Chat, Execution: &result.Execution, Replayed: result.Replayed}, nil
}

func validateQueuedSourceReply(ctx context.Context, work models.MainConversationWork, messageID string) error {
	if global.WorkTicketService != nil {
		address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: work.ConversationID}
		_, found, err := global.WorkTicketService.ForSourceMessage(ctx, address, work.WorkChatID, messageID)
		if err != nil {
			return err
		}
		if found {
			_, err := global.ChatService.ReadWorkTicketReplyTarget(ctx, work.WorkChatID, messageID, true)
			return err
		}
	}
	_, err := global.ChatService.ReadSourceReplyTarget(ctx, work.WorkChatID, messageID)
	return err
}
