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
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
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
	// 来源消息
	Body chats.SendMessageRequest `in:"body"`
}

func (*SendMainConversationSourceMessage) Path() string {
	return "/:subjectType/:subjectId/sources/:workChatId/messages"
}

// Output 在原工作范围续聊，接纳用户消息时同步写入其主会话。
// @err[ChatNotFound][40400000005][工作对话不存在]
// @err[BadRequest][40000000001][输入无效]
// @err[RuntimeSessionStartFailed][50000000054][无法新建智能体会话]
// @err[RuntimeSessionRestoreFailed][50000000055][无法恢复已有的智能体会话]
// @err[DatabaseOperationCancelled][40800001001][数据库操作已取消]
// @err[DatabaseError][50000000002][数据库操作失败]
// @err[DatabaseBusy][50300001001][数据库暂忙]
func (op *SendMainConversationSourceMessage) Output(httpCtx *gin.Context) (interface{}, error) {
	executionContext := httpCtx.Request.Context()
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
	input, selected := op.Body.WorkInput()
	if input.SubmissionID == "" {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	content, err := publicInputContent(input)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	identity, err := requestidempotency.NewSubmission(input.SubmissionID, []any{execution.MainConversationSourceEntryPoint, work.WorkChatID, input, selected})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	publicID := mainconversation.UserMessageID(work.ConversationID, input.SubmissionID)
	sourceMessageID := richSourceMessageID(publicID, input)
	saved, err := global.MainConversationService.Message(ctx, work.ConversationID, publicID)
	if err == nil {
		if saved.SourceChatID != source.ID || saved.InputDigest != identity.Hash {
			return nil, ginx.WithStack(status_error.CreateRequestConflict)
		}
		if saved.AdmissionPending {
			accepted, err := global.ChatService.ReadSubmission(ctx, source.ID, input.SubmissionID)
			if err != nil {
				return nil, ginx.WithStack(err)
			}
			if accepted != nil {
				if _, err := global.MainConversationService.ConfirmUserReceipt(ctx, saved, *accepted); err != nil {
					return nil, ginx.WithStack(err)
				}
				return execution.AcceptedExecution{Chat: source, Execution: *accepted, Replayed: true}, nil
			}
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
	executionID := identity.ResourceID("main-conversation-source", source.ID)
	options.EntryPoint = execution.MainConversationSourceEntryPoint
	options.ToolCapabilities = execution.MainConversationTools(source.ID, executionID, publicID, global.MainConversationService)
	intent := execution.WorkIntent{ExecutionID: executionID, UserMessageID: sourceMessageID, Chat: execution.ChatTarget{ChatID: source.ID}, Input: input, Options: options, Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
		AdmissionBoundary: mainConversationAdmission(op.SubjectType, op.SubjectID, work, false, func(ctx context.Context, commit func(context.Context) error) error {
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
	return result, nil
}
