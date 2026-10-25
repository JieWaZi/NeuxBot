package channel

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// enqueueLeadInput 把负责人忙碌期间的完整用户补充及其协调或直接执行模式写入可靠队列。
func (s *service) enqueueLeadInput(
	ctx context.Context,
	thread models.ChannelThread,
	input execution.WorkInput,
	directExecution bool,
) (models.ChannelLeadInput, error) {
	locked, release, lockErr := s.acquireThreadOperation(ctx, thread.ID)
	if lockErr != nil {
		return models.ChannelLeadInput{}, lockErr
	}
	ctx = locked
	defer release()
	queueID := util.New()
	if input.SubmissionID != "" {
		identity, err := requestidempotency.New(input.SubmissionID, []any{input, directExecution})
		if err != nil {
			return models.ChannelLeadInput{}, err
		}
		queueID = identity.ResourceID("lead_input", thread.ID)
		previous, exists, err := s.threads.GetLeadInput(ctx, queueID)
		if err != nil {
			return models.ChannelLeadInput{}, err
		}
		if exists {
			prior, err := requestidempotency.New(previous.Payload.SubmissionID, []any{previous.Payload, previous.DirectExecution})
			if err != nil {
				return previous, err
			}
			if err := identity.ValidateReplay(prior.ID, prior.Hash, true); err != nil {
				return previous, err
			}
			return previous, nil
		}
	}
	now := time.Now().UnixMilli()
	queued := models.ChannelLeadInput{
		ID:              queueID,
		ThreadID:        thread.ID,
		Payload:         input,
		DirectExecution: directExecution,
		Status:          constants.ChannelInputStatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	err := s.transaction(ctx, func(transactionContext context.Context) error {
		if err := s.ensureThreadWritable(transactionContext, thread); err != nil {
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
	return queued, err
}

// drainLeadInputs 在负责人空闲时按顺序把一条用户补充送入原 Chat。
func (s *service) drainLeadInputs(
	ctx context.Context,
	threadID string,
) (*models.Execution, error) {
	locked, release, lockErr := s.acquireThreadOperation(ctx, threadID)
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
	inputs, err := s.threads.ListLeadInputs(
		ctx,
		thread.ID,
		[]constants.ChannelInputStatus{constants.ChannelInputStatusPending},
		1,
	)
	if err != nil || len(inputs) == 0 {
		return nil, err
	}
	queued := inputs[0]
	workInput := queued.Payload
	leadExecutionID := util.New()
	if workInput.SubmissionID != "" {
		identity, err := requestidempotency.New(workInput.SubmissionID, workInput)
		if err != nil {
			return nil, err
		}
		leadExecutionID = identity.ResourceID("lead_execution", thread.ID)
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
		lead.PermissionLevel,
		capability,
		teamInstruction,
		queued.DirectExecution,
	)
	workOptions.AccountID = lead.AccountID

	accepted, err := s.executions.Start(ctx, execution.WorkIntent{AutomaticRunID: automaticRunID,
		ExecutionID: leadExecutionID,
		Chat:        execution.ChatTarget{ChatID: thread.LeadChatID},
		Input:       workInput,
		Options:     workOptions,
		Admission:   constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil {
		s.capabilities.Revoke(capabilityToken)
		if isChatBusy(err) {
			return nil, nil
		}
		return nil, err
	}
	if err := s.threads.UpdateLeadInputs(
		ctx,
		[]string{queued.ID},
		constants.ChannelInputStatusDelivering,
		accepted.Execution.ID,
		time.Now().UnixMilli(),
	); err != nil {
		s.capabilities.Revoke(capabilityToken)
		if cancelErr := s.executions.Cancel(thread.LeadChatID); cancelErr != nil {
			logx.Errorf(
				"cancel uncommitted lead input execution failed: thread_id=%s input_id=%s execution_id=%s chat_id=%s error=%v",
				thread.ID,
				queued.ID,
				accepted.Execution.ID,
				thread.LeadChatID,
				cancelErr,
			)
		}
		return nil, err
	}
	s.watchLeadExecution(
		thread,
		accepted.Execution.ID,
		nil,
		[]string{queued.ID},
		capabilityToken,
	)
	return &accepted.Execution, nil
}
