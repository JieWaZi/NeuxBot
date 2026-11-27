package main_conversations

import (
	"context"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 发送主会话消息
type SendMainConversationMessage struct {
	ginx.MethodPost
	// 主体类型
	SubjectType string `in:"path" name:"subjectType" validate:"required,oneof=waker team"`
	// 主体标识
	SubjectID string `in:"path" name:"subjectId" validate:"required,max=32"`
	// 消息内容
	Body MainConversationInput `in:"body"`
}

// 主会话输入
type MainConversationInput struct {
	// 提交标识
	SubmissionID string `json:"submissionId" validate:"required,max=64"`
	// 消息正文
	Content string `json:"content,omitempty"`
	// 消息输入
	Message *chats.MessageRequest `json:"message,omitempty"`
	// 引用消息
	ReplyToMessageID string `json:"replyToMessageId,omitempty" validate:"max=32"`
	// 交办项目
	ProjectID string `json:"projectId,omitempty" validate:"max=32"`
	// 临时目录
	WorkingDirectory string `json:"workingDirectory,omitempty"`
}

// 主会话接纳回执
type MainConversationAcceptance struct {
	// 已接纳消息
	Message models.MainConversationMessage `json:"message"`
	// 执行标识
	ExecutionID string `json:"executionId"`
}

// Path 返回指定主体唯一的公开输入入口。
func (*SendMainConversationMessage) Path() string { return "/:subjectType/:subjectId/messages" }

// Output 接纳用户交办，按原工作范围派发 CLI，并为本轮签发主会话发言能力。
// @err[BadRequest][40000000001][输入或工作范围无效]
// @err[ChatReplyTargetUnavailable][40400007202][引用的消息不可用]
// @err[RuntimeSessionStartFailed][50000000054][无法新建智能体会话]
// @err[RuntimeSessionRestoreFailed][50000000055][无法恢复已有的智能体会话]
// @err[DatabaseOperationCancelled][40800001001][数据库操作已取消]
// @err[DatabaseError][50000000002][数据库操作失败]
// @err[DatabaseBusy][50300001001][数据库暂忙]
func (op *SendMainConversationMessage) Output(ctx *gin.Context) (interface{}, error) {
	result, err := op.send(ctx.Request.Context(), ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}

func (op *SendMainConversationMessage) send(ctx context.Context, httpCtx *gin.Context) (MainConversationAcceptance, error) {
	var result MainConversationAcceptance
	executionContext := ctx
	ctx, releaseTeam, err := lockMainConversationTeam(ctx, op.SubjectType, op.SubjectID)
	if err != nil {
		return result, err
	}
	releaseTeam = sync.OnceFunc(releaseTeam)
	defer releaseTeam()
	body := op.Body
	body.SubmissionID = strings.TrimSpace(body.SubmissionID)
	body.ReplyToMessageID = strings.TrimSpace(body.ReplyToMessageID)
	if body.SubmissionID == "" {
		return result, status_error.BadRequest
	}
	input, selected, content, digest, err := body.workInput()
	if err != nil {
		return result, err
	}
	scope, err := body.scope()
	if err != nil {
		return result, err
	}
	service := global.MainConversationService
	conv, err := service.Ensure(ctx, op.SubjectType, op.SubjectID)
	if err != nil {
		return result, err
	}
	publicID := mainconversation.UserMessageID(conv.ID, body.SubmissionID)
	sourceMessageID := richSourceMessageID(publicID, input)
	var work models.MainConversationWork
	receipt, err := service.Message(ctx, conv.ID, publicID)
	if err == nil {
		if receipt.Content != content || receipt.InputDigest != digest || receipt.ReplyToMessageID != body.ReplyToMessageID || receipt.RequestedDirectory != body.WorkingDirectory {
			return result, status_error.CreateRequestConflict
		}
		sourceMessageID = receipt.SourceMessageID
		work, err = service.Source(ctx, op.SubjectType, op.SubjectID, receipt.SourceChatID)
		if err != nil {
			return result, err
		}
		if body.ReplyToMessageID == "" && body.WorkingDirectory == "" {
			originalScope := work.ScopeKey
			if i := strings.LastIndexByte(originalScope, '@'); body.ProjectID != "" && i >= 0 {
				originalScope = originalScope[:i]
			}
			if originalScope != scope {
				return result, status_error.CreateRequestConflict
			}
		}
		accepted, err := global.ChatService.ReadSubmission(ctx, work.WorkChatID, body.SubmissionID)
		if err != nil {
			return result, err
		}
		if accepted != nil {
			receipt, err = service.ConfirmUserReceipt(ctx, receipt, *accepted)
			if err != nil {
				return result, err
			}
			return MainConversationAcceptance{Message: receipt, ExecutionID: accepted.ID}, nil
		}
	} else if !util.HasStatusCode(err, status_error.ChatMessageNotFound) {
		return result, err
	}

	input.SubmissionID = body.SubmissionID
	target := execution.ChatTarget{}
	if body.ReplyToMessageID != "" {
		var quote workinput.Instruction
		work, quote, err = op.resolveReply(ctx, conv.ID, body.ReplyToMessageID)
		if err != nil {
			return result, err
		}
		input.ContextInstructions = []workinput.Instruction{quote}
	}
	actor, options, err := op.actorOptions(ctx, work.WakerID, selected)
	if err != nil {
		return result, err
	}
	newAssignment := work.WorkChatID == ""
	if newAssignment {
		source, frozenScope, err := body.freezeSource(ctx, actor, scope)
		if err != nil {
			return result, err
		}
		work, err = service.ReserveWork(ctx, op.SubjectType, op.SubjectID, actor, frozenScope)
		if err != nil {
			return result, err
		}
		target.MainConversationSource = source
	}
	if work.WakerID != actor {
		return result, status_error.ChatReplyTargetUnavailable
	}
	target.ChatID = work.WorkChatID
	options.EntryPoint = execution.MainConversationEntryPoint
	options.ToolCapabilities = execution.MainConversationTools(work.WorkChatID, publicID, publicID, service)
	release, err := uploads.ClaimWork(httpCtx, &input)
	if err != nil {
		return result, err
	}
	acceptedInput := false
	defer func() { release(acceptedInput) }()
	intent := execution.WorkIntent{Chat: target, Options: options, Input: input, ExecutionID: publicID, UserMessageID: sourceMessageID, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
	intent.AdmissionBoundary = mainConversationAdmission(op.SubjectType, op.SubjectID, work, newAssignment, func(ctx context.Context, commit func(context.Context) error) error {
		_, err := service.AdmitUser(ctx, op.SubjectType, op.SubjectID, body.SubmissionID, content, body.ReplyToMessageID, work.WorkChatID, publicID, sourceMessageID, body.WorkingDirectory, digest, commit)
		return err
	})
	// 来源已经按原主体、执行伙伴和工作范围冻结。原生准备期间允许处理团队的其它请求。
	releaseTeam()
	accepted, err := global.ExecutionService.Start(executionContext, intent)
	if err != nil {
		return result, err
	}
	acceptedInput = true
	message, err := service.Message(ctx, conv.ID, publicID)
	return MainConversationAcceptance{Message: message, ExecutionID: accepted.Execution.ID}, err
}
