package mainconversation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// InputQueueOptions 将主会话队列连接至公开记录、来源日志和运行生命周期。
type InputQueueOptions struct {
	// ReadSubmission 只读取原来源 Chat 日志中的提交回执。
	ReadSubmission func(context.Context, string, string) (*models.Execution, error)
	// ConfirmBootAbsence 在确认原提交不存在后，进一步核对上一 Core 是否已写入原执行。
	ConfirmBootAbsence func(context.Context, models.MainConversationInput) (bool, error)
	// Dispatch 重建已授权工作意图，在实际写入来源日志前登记原生提交门。
	Dispatch func(context.Context, models.MainConversationInput, func(context.Context) error) (models.Execution, error)
	// ReadRoutedPublication 在重新判断前查验同一输入已提交的直接答复。
	ReadRoutedPublication func(context.Context, models.MainConversationInput) (bool, error)
	// Route 在原生输入提交前登记边界，随后发布直接答复或在事务中交给工作来源。
	Route func(context.Context, models.MainConversationInput, bool, func(context.Context) error, func(context.Context, models.MainConversationInput) error) error
	// WaitForIdle 等待原执行释放 Chat 输入席位。
	WaitForIdle func(context.Context, string) error
	// FreezeWorkflow 按接纳时配置冻结工作流修订。
	FreezeWorkflow func(context.Context, *workinput.WorkflowReference) (*workinput.WorkflowReference, error)
	// Layout 定位各来源 Chat 的私有附件队列目录。
	Layout *chatstorage.Layout
}

// InputQueue 持久接纳用户发送，分别按主会话和来源 Chat 顺序处理。
type InputQueue struct {
	// store 保存接纳顺序和持久派发边界。
	store *controllers.MainConversationInputController
	// options 提供核心依赖与原记录读写能力。
	options InputQueueOptions
	// mu 保护启动、唤醒与进程内上传占位。
	mu sync.Mutex
	// ctx 是 Core 拥有的队列处理期。
	ctx context.Context
	// cancel 在关闭执行与存储前停止派发和等待。
	cancel context.CancelFunc
	// workers 等待锁内登记的所有队列协程。
	workers sync.WaitGroup
	// lanes 记录运行中的队列协程及本轮是否收到唤醒。
	lanes map[mainInputLane]bool
	// submissions 对同一公开消息的上传暂存串行占位。
	submissions map[string]chan struct{}
	// bootClaims 只记录本轮 Core 启动前登记过原生提交门的来源输入。
	bootClaims map[int64]struct{}
}

type mainInputLane struct {
	// chatID 标识按工作 Chat 串行派发的输入。
	chatID string
	// conversationID 标识尚无来源的主会话路由序列。
	conversationID string
}

// NewInputQueue 构造队列，等待 Core 启动后再派发。
func NewInputQueue(store *controllers.MainConversationInputController, options InputQueueOptions) *InputQueue {
	return &InputQueue{
		store: store, options: options, lanes: make(map[mainInputLane]bool),
		submissions: make(map[string]chan struct{}), bootClaims: make(map[int64]struct{}),
	}
}

// Open 启动队列处理，并按原身份恢复未交接的输入。
func (q *InputQueue) Open(ctx context.Context) error {
	if q.store == nil || q.options.ReadSubmission == nil || q.options.Dispatch == nil ||
		(q.options.Route == nil) != (q.options.ReadRoutedPublication == nil) {
		return status_error.ChatServiceNotStarted
	}
	q.mu.Lock()
	if q.ctx != nil {
		q.mu.Unlock()
		return nil
	}
	if err := q.captureBootClaims(ctx); err != nil {
		q.mu.Unlock()
		return err
	}
	if err := q.recoverAttachments(ctx); err != nil {
		logx.Warnf("recover main input attachments: error=%v", err)
	}
	q.ctx, q.cancel = context.WithCancel(ctx)
	q.mu.Unlock()
	const batch = 64
	for after := ""; ; {
		chats, err := q.store.ActiveChats(ctx, after, batch)
		if err != nil {
			q.Close()
			return err
		}
		for _, chat := range chats {
			q.Wake(chat)
		}
		if len(chats) < batch {
			break
		}
		after = chats[len(chats)-1]
	}
	if q.options.Route == nil {
		return nil
	}
	for after := ""; ; {
		conversations, err := q.store.ActiveRoutingConversations(ctx, after, batch)
		if err != nil {
			q.Close()
			return err
		}
		for _, conversation := range conversations {
			q.WakeRouting(conversation)
		}
		if len(conversations) < batch {
			return nil
		}
		after = conversations[len(conversations)-1]
	}
}

// Close 停止后台处理，已接纳的未交接输入留待 Core 重启恢复。
func (q *InputQueue) Close() {
	q.mu.Lock()
	if q.cancel != nil {
		q.cancel()
	}
	q.mu.Unlock()
	q.workers.Wait()
}

// Enqueue 持久接纳输入，在原生准备前返回稳定回执。
func (q *InputQueue) Enqueue(ctx context.Context, input models.MainConversationInput, claim func(*workinput.Input) (func(bool), error)) (models.MainConversationInputReceipt, error) {
	submission := strings.TrimSpace(input.Input.SubmissionID)
	if submission == "" || len(submission) > constants.InputSubmissionIDMaximumLength ||
		input.ConversationID == "" || input.ExecutionID == "" ||
		(input.WorkChatID == "" && (input.RouterWakerID == "" || q.options.Route == nil || q.options.ReadRoutedPublication == nil || HasWorkResources(input.Input))) ||
		input.PublicMessageID != UserMessageID(input.ConversationID, submission) || input.RequestHash == "" ||
		input.Input.SteerExecutionID != "" {
		return models.MainConversationInputReceipt{}, status_error.BadRequest
	}
	input.Input.SubmissionID = submission
	release, err := q.acquireSubmission(ctx, input.PublicMessageID)
	if err != nil {
		return models.MainConversationInputReceipt{}, err
	}
	defer release()
	stored, found, err := q.store.Get(ctx, input.ConversationID, input.PublicMessageID)
	if err != nil {
		return models.MainConversationInputReceipt{}, err
	}
	if found {
		if err := validateMainInputReplay(input, stored); err != nil {
			return models.MainConversationInputReceipt{}, err
		}
		q.WakeReceipt(stored.Receipt())
		return stored.Receipt(), nil
	}
	if input.Input.Workflow != nil {
		if q.options.FreezeWorkflow == nil {
			return models.MainConversationInputReceipt{}, status_error.WorkflowCapabilityUnsupported
		}
		input.Input.Workflow, err = q.options.FreezeWorkflow(ctx, input.Input.Workflow)
		if err != nil {
			return models.MainConversationInputReceipt{}, err
		}
	}
	if q.options.Layout != nil && len(input.Input.Attachments) != 0 {
		release, err := chatstorage.AcquireReferences(q.options.Layout.Root(), input.WorkChatID)
		if err != nil {
			return models.MainConversationInputReceipt{}, err
		}
		defer release()
	}
	finish, err := q.stageAttachments(&input, claim)
	if err != nil {
		return models.MainConversationInputReceipt{}, err
	}
	stored, err = q.store.Insert(ctx, input)
	if err != nil {
		if isMainInputRetryable(err) {
			// A failed response can follow an applied transaction. Never remove the only staged
			// copy while the stored admission is uncertain.
			settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			recovered, found, readErr := q.store.Get(settle, input.ConversationID, input.PublicMessageID)
			cancel()
			if readErr == nil && found {
				stored, err = recovered, nil
			} else {
				finish(false, readErr != nil)
				return models.MainConversationInputReceipt{}, err
			}
		} else {
			finish(false, false)
			return models.MainConversationInputReceipt{}, err
		}
	}
	finish(true, true)
	if err := validateMainInputReplay(input, stored); err != nil {
		return models.MainConversationInputReceipt{}, err
	}
	q.WakeReceipt(stored.Receipt())
	return stored.Receipt(), nil
}

func validateMainInputReplay(input, stored models.MainConversationInput) error {
	if (input.RouterWakerID == "" && input.WorkChatID != stored.WorkChatID) || input.ExecutionID != stored.ExecutionID {
		return status_error.CreateRequestConflict
	}
	identity := requestidempotency.Identity{ID: input.Input.SubmissionID, Hash: input.RequestHash}
	return identity.ValidateReplay(stored.Input.SubmissionID, stored.RequestHash, true)
}

// Receipt 按原提交查找稳定回执，包括已交接记录。
func (q *InputQueue) Receipt(ctx context.Context, conversationID, submissionID string) (models.MainConversationInputReceipt, error) {
	row, found, err := q.store.Get(ctx, conversationID, UserMessageID(conversationID, strings.TrimSpace(submissionID)))
	if err == nil && !found {
		err = status_error.ChatMessageNotFound
	}
	if err != nil {
		return models.MainConversationInputReceipt{}, err
	}
	return row.Receipt(), nil
}

// List 按升序读取一页未交接输入，可按来源过滤。
func (q *InputQueue) List(ctx context.Context, conversationID, workChatID string, after int64, limit int) ([]models.MainConversationInputReceipt, error) {
	return q.list(ctx, conversationID, workChatID, after, limit, false)
}

// ListFailed 单独分页读取已失败的输入。
func (q *InputQueue) ListFailed(ctx context.Context, conversationID, workChatID string, after int64, limit int) ([]models.MainConversationInputReceipt, error) {
	return q.list(ctx, conversationID, workChatID, after, limit, true)
}

func (q *InputQueue) list(ctx context.Context, conversationID, workChatID string, after int64, limit int, failed bool) ([]models.MainConversationInputReceipt, error) {
	if conversationID == "" || after < 0 || limit < 1 || limit > 100 {
		return nil, status_error.BadRequest
	}
	rows, err := q.store.List(ctx, conversationID, workChatID, after, limit, failed)
	if err != nil {
		return nil, err
	}
	receipts := make([]models.MainConversationInputReceipt, 0, len(rows))
	for _, row := range rows {
		receipts = append(receipts, row.Receipt())
	}
	return receipts, nil
}

// Wake 唤醒指定来源 Chat 的输入处理。
func (q *InputQueue) Wake(chatID string) {
	q.wake(mainInputLane{chatID: chatID})
}

// WakeRouting 唤醒主会话的待路由输入。
func (q *InputQueue) WakeRouting(conversationID string) {
	q.wake(mainInputLane{conversationID: conversationID})
}

// DefaultSourceForRouting 查找路由可以继续的常驻来源。
func (q *InputQueue) DefaultSourceForRouting(ctx context.Context, conversationID, wakerID string) (string, error) {
	return q.store.DefaultSourceForRouting(ctx, conversationID, wakerID)
}

// WakeReceipt 按已接纳回执唤醒原路由或来源队列。
func (q *InputQueue) WakeReceipt(receipt models.MainConversationInputReceipt) {
	if receipt.WorkChatID != "" {
		q.Wake(receipt.WorkChatID)
	} else if receipt.RouterWakerID != "" {
		q.WakeRouting(receipt.ConversationID)
	}
}

func (q *InputQueue) wake(lane mainInputLane) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.ctx == nil || q.ctx.Err() != nil || lane == (mainInputLane{}) {
		return
	}
	if _, active := q.lanes[lane]; active {
		q.lanes[lane] = true
		return
	}
	q.lanes[lane] = false
	q.workers.Go(func() { q.run(q.ctx, lane) })
}

func (q *InputQueue) acquireSubmission(ctx context.Context, id string) (func(), error) {
	for ctx.Err() == nil {
		q.mu.Lock()
		pending := q.submissions[id]
		if pending == nil {
			pending = make(chan struct{})
			q.submissions[id] = pending
			q.mu.Unlock()
			return func() {
				q.mu.Lock()
				defer q.mu.Unlock()
				delete(q.submissions, id)
				close(pending)
			}, nil
		}
		q.mu.Unlock()
		select {
		case <-pending:
		case <-ctx.Done():
		}
	}
	return nil, status_error.OperationCanceled
}

func (q *InputQueue) run(ctx context.Context, lane mainInputLane) {
	for {
		q.drainWithRetry(ctx, lane)
		q.mu.Lock()
		if q.lanes[lane] && ctx.Err() == nil {
			q.lanes[lane] = false
			q.mu.Unlock()
			continue
		}
		delete(q.lanes, lane)
		q.mu.Unlock()
		return
	}
}

type mainInputDrainStep uint8

const (
	mainInputDrainStop mainInputDrainStep = iota
	mainInputDrainAdvance
	mainInputDrainRetryDispatch
)

func (q *InputQueue) drainWithRetry(ctx context.Context, lane mainInputLane) {
	const maxRetryDelay = 5 * time.Second
	const maxReconciliationFailures = 4
	reconciliationFailures := 0
	for delay := 100 * time.Millisecond; ctx.Err() == nil; delay = min(delay*2, maxRetryDelay) {
		step, err := q.drain(ctx, lane)
		if err == nil || ctx.Err() != nil {
			return
		}
		if step == mainInputDrainRetryDispatch || isMainInputRetryable(err) || util.HasStatusCode(err, status_error.ChatLogError) {
			// 暂时无法读到原回执时仍由这个来源队列负责对账。下一次领取前会重新查证。
			reconciliationFailures = 0
		} else {
			reconciliationFailures++
			if reconciliationFailures == maxReconciliationFailures {
				logx.Warnf("main input queue reconciliation deferred: chat_id=%s conversation_id=%s error=%v", lane.chatID, lane.conversationID, err)
				return
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
	}
}

func (q *InputQueue) drain(ctx context.Context, lane mainInputLane) (mainInputDrainStep, error) {
	for ctx.Err() == nil {
		readHead := q.store.Head
		key := lane.chatID
		advance := q.advance
		if lane.conversationID != "" {
			readHead, key, advance = q.store.RoutingHead, lane.conversationID, q.advanceRouting
		}
		input, found, err := readHead(ctx, key)
		if err != nil || !found {
			return mainInputDrainStop, err
		}
		step, err := advance(ctx, input)
		if err != nil || step != mainInputDrainAdvance {
			return step, err
		}
	}
	return mainInputDrainStop, ctx.Err()
}

func (q *InputQueue) advanceRouting(ctx context.Context, input models.MainConversationInput) (mainInputDrainStep, error) {
	published, err := q.options.ReadRoutedPublication(ctx, input)
	if err != nil {
		return mainInputDrainStop, err
	}
	if published {
		return q.settle(ctx, input, constants.MainConversationInputAccepted, "")
	}
	recovering := input.CommitStarted
	if input.Status != constants.MainConversationInputDispatching {
		claimed, err := q.store.Claim(ctx, input.Sequence)
		if err != nil || !claimed {
			return mainInputDrainStop, err
		}
		input.Status = constants.MainConversationInputDispatching
	}
	start := time.Now()
	var destination, defaultSource string
	// 已到达提交门又缺少公开回执，重新派发时按公开历史生成新原生会话。
	err = q.options.Route(ctx, input, recovering, func(submitCtx context.Context) error {
		if input.CommitStarted {
			return nil
		}
		marked, err := q.store.MarkCommitting(submitCtx, input.Sequence)
		if err != nil {
			return err
		}
		if !marked {
			return status_error.CreateRequestInProgress
		}
		input.CommitStarted = true
		return nil
	}, func(tx context.Context, routed models.MainConversationInput) error {
		promoted, err := q.store.PromoteRoute(tx, routed)
		if err != nil {
			return err
		}
		if !promoted {
			return status_error.CreateRequestInProgress
		}
		defaultSource, err = q.store.DefaultSourceForRouting(tx, input.ConversationID, input.RouterWakerID)
		if err != nil {
			return err
		}
		destination = routed.WorkChatID
		return nil
	})
	logx.Infof("main conversation routing: conversation_id=%s input_id=%s queue_wait_ms=%d dispatch_ms=%d", input.ConversationID, input.PublicMessageID, start.UnixMilli()-input.CreatedAt, time.Since(start).Milliseconds())
	if ctx.Err() != nil {
		return mainInputDrainStop, ctx.Err()
	}
	switch {
	case err == nil && destination != "":
		q.Wake(destination)
		if defaultSource != destination {
			q.Wake(defaultSource)
		}
		return mainInputDrainAdvance, nil
	case err == nil:
		return q.settle(ctx, input, constants.MainConversationInputAccepted, "")
	case errors.Is(err, context.DeadlineExceeded):
		return q.settle(ctx, input, constants.MainConversationInputFailed, util.ErrorCode(err, status_error.RuntimeExecutionFailed.Key()))
	case isMainInputRetryable(err):
		return mainInputDrainRetryDispatch, err
	default:
		return q.settle(ctx, input, constants.MainConversationInputFailed, util.ErrorCode(err, status_error.InternalServerError.Key()))
	}
}

func (q *InputQueue) advance(ctx context.Context, input models.MainConversationInput) (mainInputDrainStep, error) {
	receipt, err := q.readReceipt(ctx, input)
	switch {
	case util.HasStatusCode(err, status_error.ChatNotFound):
		return q.settle(ctx, input, constants.MainConversationInputFailed, status_error.ChatNotFound.Key())
	case err != nil:
		return q.unreadableReceipt(ctx, input, err)
	case receipt != nil:
		return q.settle(ctx, input, constants.MainConversationInputAccepted, "")
	}
	if input.CommitStarted && q.isBootClaim(input.Sequence) {
		absent, err := q.options.ConfirmBootAbsence(ctx, input)
		if err != nil {
			return q.unreadableReceipt(ctx, input, err)
		}
		if absent {
			return q.settle(ctx, input, constants.MainConversationInputQueued, "")
		}
		q.forgetBootClaim(input.Sequence)
	}
	switch {
	case input.Status == constants.MainConversationInputUncertain:
		return mainInputDrainStop, nil
	case input.CommitStarted:
		return q.markUncertain(ctx, input, "")
	case input.Status == constants.MainConversationInputDispatching:
		// A previous process could prepare native resources before this marker.
		// The original journal commit and actual prompt cannot run until it persists.
		return q.settle(ctx, input, constants.MainConversationInputQueued, "")
	}
	claimed, err := q.store.Claim(ctx, input.Sequence)
	if err != nil || !claimed {
		return mainInputDrainStop, err
	}
	input.Status = constants.MainConversationInputDispatching
	return q.dispatch(ctx, input)
}

func (q *InputQueue) dispatch(ctx context.Context, input models.MainConversationInput) (mainInputDrainStep, error) {
	_, dispatchErr := q.options.Dispatch(ctx, input, func(commitCtx context.Context) error {
		claimed, err := q.store.MarkCommitting(commitCtx, input.Sequence)
		if err != nil {
			return err
		}
		if !claimed {
			return status_error.CreateRequestInProgress
		}
		input.CommitStarted = true
		return nil
	})
	if ctx.Err() != nil {
		return mainInputDrainStop, ctx.Err()
	}
	receipt, readErr := q.readReceipt(ctx, input)
	switch {
	case util.HasStatusCode(readErr, status_error.ChatNotFound):
		return q.settle(ctx, input, constants.MainConversationInputFailed, status_error.ChatNotFound.Key())
	case readErr != nil:
		return q.unreadableReceipt(ctx, input, readErr)
	case receipt != nil:
		return q.settle(ctx, input, constants.MainConversationInputAccepted, "")
	case input.CommitStarted || dispatchErr == nil:
		return q.markUncertain(ctx, input, "")
	case util.HasStatusCode(dispatchErr, status_error.ChatHasActiveExecution):
		if _, err := q.settle(ctx, input, constants.MainConversationInputQueued, ""); err != nil {
			return mainInputDrainStop, err
		}
		if q.options.WaitForIdle == nil {
			return mainInputDrainStop, nil
		}
		return mainInputDrainAdvance, q.options.WaitForIdle(ctx, input.WorkChatID)
	case isMainInputRetryable(dispatchErr):
		// 发送门前失败时保留原队列位置。重试前仍先核验原日志回执。
		if _, err := q.settle(ctx, input, constants.MainConversationInputQueued, ""); err != nil {
			return mainInputDrainRetryDispatch, err
		}
		return mainInputDrainRetryDispatch, dispatchErr
	default:
		return q.settle(ctx, input, constants.MainConversationInputFailed, util.ErrorCode(dispatchErr, status_error.InternalServerError.Key()))
	}
}

func isMainInputRetryable(err error) bool {
	return errors.Is(err, agentapi.ErrTurnAdmissionBusy) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		util.HasStatusCode(err, status_error.DatabaseBusy) || util.HasStatusCode(err, status_error.DatabaseError) ||
		util.HasStatusCode(err, status_error.DatabaseOperationCancelled) || util.HasStatusCode(err, status_error.CreateRequestInProgress) ||
		util.HasStatusCode(err, status_error.RuntimeSessionRestoreFailed)
}

func (q *InputQueue) readReceipt(ctx context.Context, input models.MainConversationInput) (*models.Execution, error) {
	receipt, err := q.options.ReadSubmission(ctx, input.WorkChatID, input.Input.SubmissionID)
	if input.RoutedSource != nil && !input.CommitStarted && util.HasStatusCode(err, status_error.ChatNotFound) {
		return nil, nil
	}
	if err != nil || receipt == nil {
		return receipt, err
	}
	if receipt.ID != input.ExecutionID || receipt.ChatID != input.WorkChatID || receipt.SubmissionID != input.Input.SubmissionID ||
		(input.SourceMessageID != "" && receipt.UserMessageID != input.SourceMessageID) || receipt.IMReplyLinkID != input.Input.IMReplyLinkID {
		return nil, status_error.ChatLogCorrupted
	}
	return receipt, nil
}

func (q *InputQueue) unreadableReceipt(ctx context.Context, input models.MainConversationInput, err error) (mainInputDrainStep, error) {
	if input.CommitStarted && input.Status != constants.MainConversationInputUncertain && ctx.Err() == nil {
		if _, markErr := q.markUncertain(ctx, input, util.ErrorCode(err, status_error.ChatLogError.Key())); markErr != nil {
			return mainInputDrainStop, markErr
		}
	}
	return mainInputDrainStop, err
}

func (q *InputQueue) markUncertain(ctx context.Context, input models.MainConversationInput, code string) (mainInputDrainStep, error) {
	_, err := q.settle(ctx, input, constants.MainConversationInputUncertain, code)
	return mainInputDrainStop, err
}

func (q *InputQueue) settle(ctx context.Context, input models.MainConversationInput, status constants.MainConversationInputStatus, code string) (mainInputDrainStep, error) {
	var applied bool
	var err error
	if input.WorkChatID == "" {
		var defaultSource string
		applied, defaultSource, err = q.store.SettleRouting(ctx, input, status, code)
		if applied && err == nil {
			q.Wake(defaultSource)
		}
	} else {
		applied, err = q.store.Settle(ctx, input, status, code)
	}
	if err != nil || !applied {
		return mainInputDrainStop, err
	}
	if status == constants.MainConversationInputAccepted || status == constants.MainConversationInputFailed {
		q.releaseAttachments(input)
	}
	if status != constants.MainConversationInputUncertain && input.CommitStarted && input.WorkChatID != "" {
		q.forgetBootClaim(input.Sequence)
	}
	return mainInputDrainAdvance, nil
}

// captureBootClaims 在新 Core 派发前记录上一进程的提交门，随后由原来源按顺序对账。
func (q *InputQueue) captureBootClaims(ctx context.Context) error {
	if q.options.ConfirmBootAbsence == nil {
		return nil
	}
	const batch = 64
	for after := int64(0); ; {
		rows, err := q.store.MarkedInputs(ctx, after, batch)
		if err != nil {
			return err
		}
		for _, row := range rows {
			after = row.Sequence
			q.bootClaims[row.Sequence] = struct{}{}
		}
		if len(rows) < batch {
			return nil
		}
	}
}

func (q *InputQueue) isBootClaim(sequence int64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, exists := q.bootClaims[sequence]
	return exists
}

func (q *InputQueue) forgetBootClaim(sequence int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.bootClaims, sequence)
}
