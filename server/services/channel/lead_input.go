package channel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/uploads"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// LeadInputID 从原提交身份派生负责人输入的持久队列标识。
func LeadInputID(threadID, submissionID string) string {
	return (requestidempotency.Identity{ID: submissionID}).ResourceID("lead_input", threadID)
}

// enqueueLeadInput 把负责人忙碌期间的完整用户补充及其协调或直接执行模式写入可靠队列。
func (s *service) enqueueLeadInput(
	ctx context.Context,
	thread models.ChannelThread,
	request SendMessageInput,
) (models.ChannelLeadInput, error) {
	locked, release, lockErr := s.acquireThreadOperation(ctx, thread.ID)
	if lockErr != nil {
		return models.ChannelLeadInput{}, lockErr
	}
	ctx = locked
	defer release()
	input := request.Input
	directExecution := request.MentionWakerID != "" || input.Workflow != nil
	queueID := util.New()
	var requestHash string
	if input.SubmissionID != "" {
		identity, err := requestidempotency.New(input.SubmissionID, []any{input, directExecution})
		if err != nil {
			return models.ChannelLeadInput{}, err
		}
		queueID = LeadInputID(thread.ID, identity.ID)
		requestHash = identity.Hash
		previous, exists, err := s.threads.GetLeadInput(ctx, queueID)
		if err != nil {
			return models.ChannelLeadInput{}, err
		}
		if exists {
			if err := identity.ValidateReplay(previous.Payload.SubmissionID, previous.RequestHash, true); err != nil {
				return previous, err
			}
			return previous, nil
		}
	}
	if input.ReplyToMessageID != "" {
		if _, err := s.chats.ReadTeamReplyTarget(ctx, thread.ID, thread.LeadChatID, input.ReplyToMessageID); err != nil {
			return models.ChannelLeadInput{}, err
		}
	}
	if input.Workflow != nil {
		if s.freezeWorkflowSelection == nil {
			return models.ChannelLeadInput{}, status_error.WorkflowCapabilityUnsupported
		}
		var err error
		input.Workflow, err = s.freezeWorkflowSelection(ctx, input.Workflow)
		if err != nil {
			return models.ChannelLeadInput{}, err
		}
	}
	committed := false
	if request.ClaimAttachments != nil && len(input.Attachments) != 0 {
		input.Attachments = slices.Clone(input.Attachments)
		release, err := request.ClaimAttachments(&input)
		if err != nil {
			return models.ChannelLeadInput{}, err
		}
		defer func() { release(committed) }()
		attachments, err := s.stageLeadInputAttachments(thread, queueID, request.Input.Attachments, input.Attachments)
		if err != nil {
			return models.ChannelLeadInput{}, err
		}
		if attachments.Rollback != nil {
			defer func() {
				if !committed {
					if err := attachments.Rollback(); err != nil {
						logx.Warnf("rollback lead input attachments: thread_id=%s input_id=%s error=%v", thread.ID, queueID, err)
					}
				}
			}()
		}
	}
	now := time.Now().UnixMilli()
	queued := models.ChannelLeadInput{
		ID:              queueID,
		ThreadID:        thread.ID,
		Payload:         input,
		RequestHash:     requestHash,
		DirectExecution: directExecution,
		Status:          constants.ChannelInputStatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	err := request.AdmissionBoundary.Commit(ctx, func(commitContext context.Context) error {
		err := s.transaction(commitContext, func(transactionContext context.Context) error {
			if err := s.ensureThreadWritable(transactionContext, thread); err != nil {
				return err
			}
			if err := s.checkStoppedThreadSubmission(thread, input.SubmissionID); err != nil {
				return err
			}
			if err := s.threads.CreateLeadInput(transactionContext, queued); err != nil {
				return err
			}
			_, err := s.appendEvent(transactionContext, thread.ChannelID, thread.ID, constants.ChannelEventLeadInputQueued, map[string]interface{}{
				"inputId": queued.ID,
			})
			return err
		})
		committed = err == nil
		return err
	})
	return queued, err
}

// stageLeadInputAttachments 在上传租约内复制队列所需的字节，原生派发仍沿用 Chat 附件接纳。
func (s *service) stageLeadInputAttachments(thread models.ChannelThread, inputID string, original, resolved []execution.AttachmentInput) (workspace.PreparedAttachments, error) {
	var files []workspace.AttachmentInput
	var positions []int
	for index, source := range original {
		if uploads.IsReference(source.SourcePath) {
			positions = append(positions, index)
			files = append(files, workspace.AttachmentInput{SourcePath: resolved[index].SourcePath})
		}
	}
	if len(files) == 0 {
		return workspace.PreparedAttachments{}, nil
	}
	layout, err := chatstorage.NewLayout(s.managedFilesDirectory)
	if err != nil {
		return workspace.PreparedAttachments{}, err
	}
	directory, err := layout.TeamRoot(thread.ID)
	if err != nil {
		return workspace.PreparedAttachments{}, err
	}
	owner, err := workspace.ReadExecutionWorkspaceOwner(directory)
	if err != nil {
		return workspace.PreparedAttachments{}, err
	}
	if owner.Kind != constants.ThreadExecutionWorkspaceOwnerKind || owner.ThreadID != thread.ID {
		return workspace.PreparedAttachments{}, status_error.ChatDirectoryOwnershipInvalid
	}
	if err := workspace.RemoveQueuedAttachments(directory, thread.LeadChatID, inputID); err != nil {
		return workspace.PreparedAttachments{}, err
	}
	prepared, err := workspace.StageQueuedAttachments(directory, thread.LeadChatID, inputID, files)
	if err != nil {
		return workspace.PreparedAttachments{}, err
	}
	for index, position := range positions {
		resolved[position].SourcePath = prepared.RuntimePaths[index]
	}
	return prepared, nil
}

// releaseLeadInputAttachments 回收队列副本；已接纳的附件由原 Chat 保存。
func (s *service) releaseLeadInputAttachments(thread models.ChannelThread, inputID string) {
	if s.managedFilesDirectory == "" {
		return
	}
	layout, err := chatstorage.NewLayout(s.managedFilesDirectory)
	if err == nil {
		var directory string
		directory, err = layout.TeamRoot(thread.ID)
		if err == nil {
			err = workspace.RemoveQueuedAttachments(directory, thread.LeadChatID, inputID)
		}
	}
	if err != nil {
		logx.Warnf("release lead input attachments: thread_id=%s input_id=%s error=%v", thread.ID, inputID, err)
	}
}

// drainLeadInputs 在负责人空闲时按顺序把一条用户补充送入原 Chat。
func (s *service) drainLeadInputs(
	ctx context.Context,
	threadID string,
) (*models.Execution, error) {
	return s.dispatchLeadInputs(ctx, threadID, nil)
}

// dispatchLeadInputs 跳过已确认失败的队首，直到接纳下一条输入或遇到需要等待的条件。
func (s *service) dispatchLeadInputs(ctx context.Context, threadID string, submitted *models.ChannelLeadInput) (*models.Execution, error) {
	locked, release, lockErr := acquireLeadDispatch(ctx, threadID)
	if lockErr != nil {
		return nil, lockErr
	}
	ctx = locked
	defer release()
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureThreadWritable(ctx, thread); err != nil {
		return nil, err
	}
	lead, err := s.threads.GetMember(ctx, thread.ID, thread.LeadWakerID)
	if err != nil {
		return nil, err
	}
	summary, err := s.chats.GetChatSummary(ctx, thread.LeadChatID)
	if err != nil {
		return nil, err
	}
	if summary.Chat.ActiveExecutionID != "" {
		return nil, nil
	}
	for {
		inputs, err := s.threads.ListLeadInputs(ctx, thread.ID, []constants.ChannelInputStatus{constants.ChannelInputStatusPending}, 1)
		if err != nil || len(inputs) == 0 {
			return nil, err
		}
		queued := inputs[0]
		currentSubmission := submitted != nil && submitted.ID == queued.ID
		reportedBySend := currentSubmission && queued.Payload.IMReplyLinkID == ""
		accepted, err := s.dispatchLeadInput(ctx, thread, lead, &queued, reportedBySend)
		if currentSubmission {
			*submitted = queued
		} else if submitted != nil {
			// 等待中的新输入不能取得已经派发的其他输入的接纳身份。
			accepted = nil
		}
		if err != nil || queued.Status != constants.ChannelInputStatusFailed {
			return accepted, err
		}
	}
}

// dispatchLeadInput 将单条输入交给原 Chat，或者提交这个输入无法接纳的明确结果。
func (s *service) dispatchLeadInput(ctx context.Context, thread models.ChannelThread, lead models.ChannelThreadMember, queued *models.ChannelLeadInput, reportedBySend bool) (*models.Execution, error) {
	workInput := queued.Payload
	workInput.SubmissionID = strings.TrimSpace(workInput.SubmissionID)
	if workInput.SubmissionID == "" {
		workInput.SubmissionID = queued.ID
	}
	leadExecutionID := (requestidempotency.Identity{ID: workInput.SubmissionID}).ResourceID("lead_execution", thread.ID)
	// 恢复已接纳输入时，当前成员资料和工具租约不应参与原提交的对账。
	receipt, err := s.readLeadInputReceipt(ctx, thread, workInput, leadExecutionID)
	if err != nil {
		return nil, err
	}
	if receipt != nil {
		var token string
		if scope, ok := s.capabilities.Resolve(thread.ID); ok && scope.ExecutionID == receipt.ID {
			token = scope.LeaseID
		}
		return s.trackLeadInput(ctx, thread, queued.ID, *receipt, token), nil
	}
	automaticRunID, err := s.automaticWorkSource(ctx, thread)
	if err != nil {
		return nil, err
	}
	capability, capabilityToken, err := s.mintLeadCapability(ctx, thread, leadExecutionID, true)
	if err != nil {
		return nil, err
	}
	teamInstruction, err := s.teamInstruction(ctx, thread, thread.LeadWakerID)
	if err != nil {
		s.capabilities.Revoke(capabilityToken)
		return nil, err
	}
	workOptions := leaderWorkOptions(
		lead.RuntimeID,
		lead.Model,
		lead.Reasoning,
		lead.Fast,
		lead.PermissionLevel,
		capability,
		teamInstruction,
		queued.DirectExecution,
	)
	workOptions.AccountID = lead.AccountID
	workInput.ContextInstructions = append(workInput.ContextInstructions, leaderTurnInstruction(queued.DirectExecution))

	accepted, err := s.startThreadExecution(ctx, thread, execution.WorkIntent{AutomaticRunID: automaticRunID,
		ExecutionID:       leadExecutionID,
		Chat:              execution.ChatTarget{ChatID: thread.LeadChatID},
		Input:             workInput,
		Options:           workOptions,
		Admission:         constants.ExecutionAdmissionCommitBeforeDispatch,
		TeamReplyThreadID: thread.ID,
	})
	if err != nil {
		switch {
		case isChatBusy(err) || errors.Is(err, agentapi.ErrTurnAdmissionBusy):
			s.capabilities.Revoke(capabilityToken)
			s.watchRecoveryDrain(thread.ID)
			return nil, nil
		case ctx.Err() != nil || !rejectedLeadInput(err):
			s.capabilities.Revoke(capabilityToken)
			return nil, err
		}
		// 接纳响应不确定时以原提交在 Chat 权威日志中的收据为准。
		receipt, readErr := s.readLeadInputReceipt(ctx, thread, workInput, leadExecutionID)
		if readErr != nil {
			s.capabilities.Revoke(capabilityToken)
			return nil, readErr
		}
		if receipt == nil {
			s.capabilities.Revoke(capabilityToken)
			return nil, s.failLeadInput(ctx, thread, queued, util.ErrorCode(err, status_error.InternalServerError.Key()), reportedBySend)
		}
		accepted.Execution = *receipt
	}
	return s.trackLeadInput(ctx, thread, queued.ID, accepted.Execution, capabilityToken), nil
}

// readLeadInputReceipt 核对原提交、负责人 Chat、执行及外部回复归属。
func (s *service) readLeadInputReceipt(ctx context.Context, thread models.ChannelThread, input execution.WorkInput, executionID string) (*models.Execution, error) {
	receipt, err := s.chats.ReadSubmission(ctx, thread.LeadChatID, input.SubmissionID)
	if err != nil || receipt == nil {
		return receipt, err
	}
	if receipt.ID != executionID || receipt.ChatID != thread.LeadChatID || receipt.IMReplyLinkID != input.IMReplyLinkID {
		return nil, status_error.ChatLogCorrupted
	}
	return receipt, nil
}

// trackLeadInput 保留已接纳的工作。临时关联失败时由同一终态监听补齐队列交接。
func (s *service) trackLeadInput(ctx context.Context, thread models.ChannelThread, inputID string, accepted models.Execution, capabilityToken string) *models.Execution {
	s.releaseLeadInputAttachments(thread, inputID)
	if err := s.threads.UpdateLeadInputs(
		context.WithoutCancel(ctx),
		[]string{inputID},
		constants.ChannelInputStatusDelivering,
		accepted.ID,
		time.Now().UnixMilli(),
	); err != nil {
		logx.Warnf("accepted lead input linkage pending: thread_id=%s input_id=%s execution_id=%s error=%v", thread.ID, inputID, accepted.ID, err)
	}
	s.watchLeadExecution(
		thread,
		accepted.ID,
		nil,
		[]string{inputID},
		capabilityToken,
	)
	return &accepted
}

// leadInputRejections 只包含旧输入在接纳前被明确拒绝的错误，未知错误由原队列继续恢复。
var leadInputRejections = [...]status_error.StatusError{
	status_error.BadRequest,
	status_error.ChatReplyTargetUnavailable,
	status_error.MessageAttachmentInvalid,
	status_error.AIAccountConfigurationInvalid,
	status_error.RuntimeConfigurationInvalid,
	status_error.RuntimePolicyUnsupported,
	status_error.RuntimeUnavailable,
	status_error.WorkflowCapabilityUnsupported,
	status_error.WorkflowNotFound,
	status_error.WorkflowStateConflict,
}

func rejectedLeadInput(err error) bool {
	for _, rejection := range leadInputRejections {
		if util.HasStatusCode(err, rejection) {
			return true
		}
	}
	return false
}

// leadInputFailureError 还原桌面入口同键重试时已经结算的原业务错误。
func leadInputFailureError(code string) error {
	for _, rejection := range leadInputRejections {
		if rejection.Key() == code {
			return rejection
		}
	}
	return status_error.InternalServerError
}

// failLeadInput 原子提交队列终态、团队事件和应当交付到原 IM 会话的失败通知。
func (s *service) failLeadInput(ctx context.Context, thread models.ChannelThread, input *models.ChannelLeadInput, code string, reportedBySend bool) error {
	var failed bool
	now := time.Now().UnixMilli()
	err := s.transaction(ctx, func(tx context.Context) error {
		if err := s.ensureThreadWritable(tx, thread); err != nil {
			return err
		}
		var err error
		failed, err = s.threads.FailLeadInput(tx, input.ID, code, now)
		if err != nil || !failed {
			return err
		}
		if _, err := s.appendEvent(tx, thread.ChannelID, thread.ID, constants.ChannelEventLeadInputFailed, map[string]any{
			"inputId": input.ID, "errorCode": code, "reportedBySend": reportedBySend,
		}); err != nil {
			return err
		}
		if input.Payload.IMReplyLinkID != "" && s.recordIMLeadInputFailure != nil {
			return s.recordIMLeadInputFailure(tx, thread, *input, code)
		}
		return nil
	})
	if err == nil && failed {
		input.Status, input.ErrorCode, input.UpdatedAt = constants.ChannelInputStatusFailed, code, now
		s.releaseLeadInputAttachments(thread, input.ID)
	}
	return err
}
