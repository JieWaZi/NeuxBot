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
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
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
	// 接力来源
	HistorySourceChatID string `json:"historySourceChatId,omitempty" validate:"max=32"`
	// 创建工单
	StartWork bool `json:"startWork,omitempty"`
	// 交办项目
	ProjectID string `json:"projectId,omitempty" validate:"max=32"`
	// 临时目录
	WorkingDirectory string `json:"workingDirectory,omitempty"`
}

// 主会话接纳回执
type MainConversationAcceptance struct {
	// 已接纳消息
	Message models.MainConversationMessage `json:"message,omitempty,omitzero"`
	// 执行标识
	ExecutionID string `json:"executionId,omitempty"`
	// 等待交给来源的输入
	QueuedInput *MainConversationQueuedInput `json:"queuedInput,omitempty"`
}

// 主会话已接纳输入
type MainConversationQueuedInput struct {
	// 仅在来源会话显示
	PrivateSource bool `json:"privateSource,omitempty"`
	// 主会话输入
	FromMain bool `json:"fromMain,omitempty"`
	// 公开消息标识
	ID string `json:"id"`
	// 工作对话标识
	WorkChatID string `json:"workChatId"`
	// 来源消息标识
	SourceMessageID string `json:"sourceMessageId"`
	// 公开引用
	ReplyToMessageID string `json:"replyToMessageId,omitempty"`
	// 输入正文
	Content string `json:"content"`
	// 接纳时间
	CreatedAt int64 `json:"createdAt"`
	// 失败原因码
	FailureCode string `json:"failureCode,omitempty"`
}

// Path 返回指定主体唯一的公开输入入口。
func (*SendMainConversationMessage) Path() string { return "/:subjectType/:subjectId/messages" }

// Output 从主会话答复用户或接纳交办，并为所选工作来源签发发言能力。
// @err[BadRequest][40000000001][输入或工作范围无效]
// @err[MainConversationProjectRequired][40000007201][指定可用项目或绝对工作目录后重新发送]
// @err[ChatReplyTargetUnavailable][40400007202][引用的消息不可用]
// @err[CreateRequestConflict][40900000023][同一提交的请求内容不同]
// @err[WorkTicketCannotContinue][40900007211][工单当前无法继续]
// @err[WorkTicketAttemptUnavailable][40900007212][这轮执行已无权更新工单]
// @err[RuntimeSessionStartFailed][50000000054][无法新建智能体会话]
// @err[RuntimeSessionRestoreFailed][50000000055][无法恢复已有的智能体会话]
// @err[DatabaseOperationCancelled][40800001001][数据库操作已取消]
// @err[DatabaseError][50000000002][数据库操作失败]
// @err[DatabaseBusy][50300001001][数据库暂忙]
func (op *SendMainConversationMessage) Output(ctx *gin.Context) (interface{}, error) {
	result, err := op.send(ctx.Request.Context(), ctx, func(conversationID, actor string, options execution.WorkOptions, detached bool) mainconversation.ProjectRoutingModel {
		return func(ctx context.Context, system, input string) (string, error) {
			if detached {
				return global.GenerateDetachedMainConversationRoute(ctx, conversationID, actor, options, system, input)
			}
			return global.GenerateMainConversationRoute(ctx, conversationID, actor, options, system, input, false, nil)
		}
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}

func (op *SendMainConversationMessage) send(ctx context.Context, httpCtx *gin.Context, router func(string, string, execution.WorkOptions, bool) mainconversation.ProjectRoutingModel) (MainConversationAcceptance, error) {
	var result MainConversationAcceptance
	body := op.Body
	body.SubmissionID = strings.TrimSpace(body.SubmissionID)
	body.ReplyToMessageID = strings.TrimSpace(body.ReplyToMessageID)
	if body.SubmissionID == "" || (body.StartWork && body.ReplyToMessageID != "") ||
		(body.HistorySourceChatID != "" && (body.Message == nil || !body.Message.StartNewSession || op.SubjectType != "waker")) {
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
	releaseSubmission, err := acquireMainSubmission(ctx, mainSubmissionKey{kind: op.SubjectType, subject: op.SubjectID, submission: body.SubmissionID})
	if err != nil {
		return result, err
	}
	defer releaseSubmission()
	executionContext := ctx
	ctx, releaseTeam, err := lockMainConversationTeam(ctx, op.SubjectType, op.SubjectID)
	if err != nil {
		return result, err
	}
	releaseTeam = sync.OnceFunc(releaseTeam)
	defer releaseTeam()
	service := global.MainConversationService
	conv, err := service.Ensure(ctx, op.SubjectType, op.SubjectID)
	if err != nil {
		return result, err
	}
	publicID := mainconversation.UserMessageID(conv.ID, body.SubmissionID)
	sourceMessageID := richSourceMessageID(publicID, input)
	canRoute := router != nil && body.ProjectID == "" && body.WorkingDirectory == "" && input.Workflow == nil
	routedDigest := mainconversation.RoutedInputDigest(publicID, digest, body.ReplyToMessageID, false)
	continuedDigest := mainconversation.RoutedInputDigest(publicID, digest, body.ReplyToMessageID, true)
	queueIdentity, err := requestidempotency.NewSubmission(body.SubmissionID,
		[]any{execution.MainConversationEntryPoint, digest, content, body.ReplyToMessageID, body.ProjectID, body.WorkingDirectory})
	if err != nil {
		return result, err
	}
	queued, err := replayMainConversationInput(ctx, conv.ID, queueIdentity)
	if err != nil || queued != nil {
		return MainConversationAcceptance{QueuedInput: queued}, err
	}
	routed := false
	shared := false
	continued := false
	var work models.MainConversationWork
	receipt, err := service.Message(ctx, conv.ID, publicID)
	if body.ReplyToMessageID != "" && global.WorkTicketService != nil && (err == nil || util.HasStatusCode(err, status_error.ChatMessageNotFound)) {
		address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: conv.ID}
		ticket, found, lookupErr := global.WorkTicketService.ForMessage(ctx, address, body.ReplyToMessageID)
		if lookupErr != nil {
			return result, lookupErr
		}
		if receipt.SourceReadOnly || (receipt.ID == "" && found && ticket.Transferred) {
			releaseTeam()
			return op.continueTransferredWork(executionContext, httpCtx, conv, receipt, ticket, body, input, content, queueIdentity.Hash)
		}
	}
	if err == nil {
		continued = canRoute && receipt.InputDigest == continuedDigest
		routed = canRoute && (receipt.InputDigest == routedDigest || continued)
		if receipt.Content != content || (!routed && (receipt.InputDigest != digest || receipt.ReplyToMessageID != body.ReplyToMessageID)) || receipt.RequestedDirectory != body.WorkingDirectory {
			return result, status_error.CreateRequestConflict
		}
		if receipt.SourceChatID == "" {
			return MainConversationAcceptance{Message: receipt}, nil
		}
		sourceMessageID = receipt.SourceMessageID
		work, err = service.Source(ctx, op.SubjectType, op.SubjectID, receipt.SourceChatID)
		if err != nil {
			return result, err
		}
		shared = work.ScopeKey == mainconversation.DefaultWorkScope
		if !routed && body.ReplyToMessageID == "" && body.WorkingDirectory == "" {
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
			if global.WorkTicketService != nil && (body.StartWork || routed || body.ReplyToMessageID != "") {
				if err := global.WorkTicketService.ConfirmExecution(ctx, accepted.ID, accepted.Status); err != nil {
					return result, err
				}
			}
			return MainConversationAcceptance{Message: receipt, ExecutionID: accepted.ID}, nil
		}
	} else if !util.HasStatusCode(err, status_error.ChatMessageNotFound) {
		return result, err
	}

	input.SubmissionID = body.SubmissionID
	target := execution.ChatTarget{}
	var referenced models.MainConversationMessage
	ticketReplyID := body.ReplyToMessageID
	if body.ReplyToMessageID != "" {
		var quotedWork models.MainConversationWork
		var quote workinput.Instruction
		referenced, quotedWork, quote, err = op.resolveReply(ctx, conv.ID, body.ReplyToMessageID)
		if err != nil {
			return result, err
		}
		if quotedWork.WorkChatID != "" {
			if work.WorkChatID != "" && work.WorkChatID != quotedWork.WorkChatID {
				return result, status_error.CreateRequestConflict
			}
			work = quotedWork
		} else {
			ticketReplyID = ""
		}
		input.ContextInstructions = []workinput.Instruction{quote}
	}
	actor, options, err := op.actorOptions(ctx, work.WakerID, selected)
	if err != nil {
		return result, err
	}
	newAssignment := work.WorkChatID == ""
	hasWorkResources := mainconversation.HasWorkResources(input)
	if canRoute && newAssignment && global.MainConversationInputQueue != nil && !hasWorkResources {
		if err := mainconversation.ValidateProjectRoutingContent(input.Content); err != nil {
			return result, err
		}
		frozen, err := global.FreezeMainConversationRoutingOptions(ctx, options)
		if err != nil {
			return result, err
		}
		queued, err := global.MainConversationInputQueue.Enqueue(ctx, models.MainConversationInput{
			ConversationID: conv.ID, RouterWakerID: actor, PublicMessageID: publicID, ExecutionID: publicID,
			PublicReplyID: body.ReplyToMessageID, StartWork: body.StartWork,
			EntryPoint: execution.MainConversationEntryPoint, PublicContent: content,
			InputDigest: digest, RequestHash: queueIdentity.Hash, Input: input, Options: frozen,
		}, nil)
		if err != nil {
			return result, err
		}
		pending := publicQueuedInput(queued)
		return MainConversationAcceptance{QueuedInput: &pending}, nil
	}
	acknowledgment := ""
	resolved := body
	if canRoute && (newAssignment || routed) {
		var before int64
		contextQuote := referenced
		if routed {
			before = receipt.Sequence
			if continued && receipt.ReplyToMessageID != referenced.ID {
				contextQuote, err = service.Message(ctx, conv.ID, receipt.ReplyToMessageID)
				if err != nil {
					return result, err
				}
			}
		}
		history, previous, questionID, err := service.RouterContext(ctx, conv.ID, before, contextQuote)
		if err != nil {
			return result, err
		}
		digest = routedDigest
		if newAssignment {
			// 目录选择模型调用在团队操作门外，最终接纳仍核对现任负责人。
			releaseTeam()
			ctx = executionContext
			ctx = global.WithMainConversationRuleManagement(ctx, conv, actor, options)
			route, err := service.PlanInput(ctx, global.ProjectControllerManager, actor, router(conv.ID, actor, options, hasWorkResources), input, body.StartWork, history, referenced, previous)
			if err != nil {
				return result, err
			}
			continued = route.ContinuePrevious
			if continued {
				body.ReplyToMessageID = questionID
				digest = continuedDigest
			}
			route, err = mainconversation.CompleteRuleRoute(ctx, route, body.SubmissionID, content)
			if err != nil {
				return result, err
			}
			if route.Message != "" || len(route.AutomaticTaskReceipts) != 0 {
				message, err := service.PublishRoute(ctx, conv, actor, body.SubmissionID, body.ReplyToMessageID, content, digest, input, route)
				return MainConversationAcceptance{Message: message}, err
			}
			acknowledgment = route.Acknowledgment
			resolved.ProjectID = route.ProjectID
			resolved.WorkingDirectory = route.WorkingDirectory
			_, err = resolved.scopeForAssignment()
			if err != nil {
				return result, err
			}
			routed = true
		}
		if continued {
			if previous == "" || questionID == "" {
				return result, status_error.ChatReplyTargetUnavailable
			}
			body.ReplyToMessageID = questionID
			digest = continuedDigest
			input.ContextInstructions = append(input.ContextInstructions, mainconversation.UnresolvedAssignment(previous))
		}
	}
	if newAssignment {
		source, frozenScope, err := global.FreezeMainConversationWorkScope(ctx, actor, resolved.ProjectID, resolved.WorkingDirectory)
		if err == nil && routed && source.ProjectScope != nil {
			err = source.ProjectScope.ValidateAvailable()
		}
		if err != nil {
			if routed && (util.HasStatusCode(err, status_error.ProjectNotFound) || util.HasStatusCode(err, status_error.ProjectSourceUnavailable)) {
				route := mainconversation.ProjectRoute{Message: mainconversation.ProjectRoutingQuestion(input.Content), Clarification: true}
				message, err := service.PublishRoute(ctx, conv, actor, body.SubmissionID, body.ReplyToMessageID, content, digest, input, route)
				return MainConversationAcceptance{Message: message}, err
			}
			return result, err
		}
		if routed || body.StartWork {
			frozenScope = mainconversation.SourceScopeKey(publicID, frozenScope, shared)
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
	startTicket := body.StartWork || routed && !shared
	// 旧版已预留的接纳沿用原摘要；新提交进入来源的同一持久入口。
	queueSource := global.MainConversationInputQueue != nil && receipt.ID == "" && !(newAssignment && routed)
	useModelDefault := op.SubjectType == "waker" && (selected == nil || selected.Model == "")
	if queueSource || useModelDefault {
		source, _, err := sourceChat(ctx, op.SubjectType, op.SubjectID, work.WorkChatID)
		if err != nil {
			if !util.HasStatusCode(err, status_error.ChatNotFound) || queueSource && target.MainConversationSource == nil {
				return result, err
			}
		} else {
			if useModelDefault {
				model, err := sourceModelDefault(ctx, source, options)
				if err != nil {
					return result, err
				}
				if model != "" {
					options.Model = model
					if selected == nil {
						options.Reasoning = ""
					}
				}
			}
			if queueSource {
				queued, err := enqueueMainConversationInput(ctx, httpCtx, options, models.MainConversationInput{
					ConversationID: conv.ID, WorkChatID: work.WorkChatID, PublicMessageID: publicID,
					ExecutionID: publicID, SourceMessageID: sourceMessageID, PublicReplyID: body.ReplyToMessageID,
					TicketReplyID: ticketReplyID, StartWork: startTicket, EntryPoint: execution.MainConversationEntryPoint,
					PublicContent: content, RequestedDirectory: body.WorkingDirectory, RequestHash: queueIdentity.Hash, Input: input,
				})
				return MainConversationAcceptance{QueuedInput: queued}, err
			}
		}
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
	if acknowledgment != "" {
		input.ContextInstructions = append(input.ContextInstructions, mainconversation.DispatchAcknowledgmentInstruction())
	}
	intent := execution.WorkIntent{Chat: target, Options: options, Input: input, ExecutionID: publicID, UserMessageID: sourceMessageID, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
	intent.Ticket = execution.WorkTicketInput{Start: startTicket, Address: workticket.Address{Kind: constants.WorkTicketOriginMain, ID: conv.ID},
		OwnerKind: op.SubjectType, OwnerID: op.SubjectID, PublicMessageID: publicID, PublicReplyID: ticketReplyID, MainConversation: service}
	intent.AdmissionBoundary = mainConversationAdmission(op.SubjectType, op.SubjectID, work, newAssignment, func(ctx context.Context, commit func(context.Context) error) error {
		_, err := service.AdmitUser(ctx, op.SubjectType, op.SubjectID, body.SubmissionID, content, body.ReplyToMessageID, work.WorkChatID, publicID, sourceMessageID, body.WorkingDirectory, digest, commit)
		if err == nil {
			service.AcknowledgeDispatch(ctx, work.WorkChatID, publicID, publicID, acknowledgment)
		}
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

func replayMainConversationInput(ctx context.Context, conversationID string, identity requestidempotency.Identity) (*MainConversationQueuedInput, error) {
	if global.MainConversationInputQueue == nil {
		return nil, nil
	}
	receipt, err := global.MainConversationInputQueue.Receipt(ctx, conversationID, identity.ID)
	if util.HasStatusCode(err, status_error.ChatMessageNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := identity.ValidateReplay(receipt.SubmissionID, receipt.RequestHash, true); err != nil {
		return nil, err
	}
	if receipt.Status == constants.MainConversationInputQueued || receipt.Status == constants.MainConversationInputUncertain {
		global.MainConversationInputQueue.WakeReceipt(receipt)
	}
	input := publicQueuedInput(receipt)
	return &input, nil
}

func enqueueMainConversationInput(ctx context.Context, httpCtx *gin.Context, options execution.WorkOptions, input models.MainConversationInput) (*MainConversationQueuedInput, error) {
	if input.SourceMessageID == "" {
		input.SourceMessageID = (requestidempotency.Identity{ID: input.PublicMessageID}).ResourceID("main-conversation-user-input")
	}
	options.EntryPoint = input.EntryPoint
	if input.EntryPoint == models.MainConversationTransferredReply {
		options.EntryPoint = execution.MainConversationSourceEntryPoint
	}
	frozen, err := global.FreezeMainConversationInputOptions(ctx, input.WorkChatID, options)
	if err != nil {
		return nil, err
	}
	input.Options = frozen
	receipt, err := global.MainConversationInputQueue.Enqueue(ctx, input, func(message *workinput.Input) (func(bool), error) {
		return uploads.ClaimWork(httpCtx, message)
	})
	if err != nil {
		return nil, err
	}
	public := publicQueuedInput(receipt)
	return &public, nil
}

func publicQueuedInput(receipt models.MainConversationInputReceipt) MainConversationQueuedInput {
	if receipt.TransferredReply {
		receipt.WorkChatID, receipt.SourceMessageID = "", ""
	}
	var failureCode string
	if receipt.Status == constants.MainConversationInputFailed {
		failureCode = receipt.ErrorCode
	}
	if receipt.RouterWakerID != "" {
		// A selected scope alone does not make the Worker history readable.
		receipt.WorkChatID, receipt.SourceMessageID = "", ""
	}
	return MainConversationQueuedInput{
		ID: receipt.PublicMessageID, WorkChatID: receipt.WorkChatID, SourceMessageID: receipt.SourceMessageID,
		ReplyToMessageID: receipt.PublicReplyID, Content: receipt.Content, CreatedAt: receipt.CreatedAt, FailureCode: failureCode,
		PrivateSource: receipt.PrivateSource,
		FromMain:      receipt.EntryPoint == execution.MainConversationEntryPoint,
	}
}
