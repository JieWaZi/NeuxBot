package channel

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// CreateTask 创建负责人维护的正式任务，并按入口语义立即或在负责人回合后派发。
func (s *service) CreateTask(
	ctx context.Context,
	input CreateTaskInput,
) (TaskResult, error) {
	input.ThreadID = strings.TrimSpace(input.ThreadID)
	input.SenderWakerID = strings.TrimSpace(input.SenderWakerID)
	input.AssigneeWakerID = strings.TrimSpace(input.AssigneeWakerID)
	input.Subject = strings.TrimSpace(input.Subject)
	input.Description = strings.TrimSpace(input.Description)
	if input.ThreadID == "" || input.AssigneeWakerID == "" ||
		input.Subject == "" || input.Description == "" ||
		len([]rune(input.Subject)) > constants.ChannelMaximumTaskSubjectRunes ||
		(input.DirectDelivery && (input.RequiresHumanReview || len(input.DependencyTaskIDs) > 0)) {
		return TaskResult{}, status_error.BadRequest
	}
	inputContext := ctx
	locked, releaseTeam, lockErr := s.acquireThreadOperation(ctx, input.ThreadID)
	if lockErr != nil {
		return TaskResult{}, lockErr
	}
	ctx = locked
	release := sync.OnceFunc(releaseTeam)
	defer release()
	if err := s.freezeBeforeUserInput(ctx, input.ThreadID); err != nil {
		return TaskResult{}, err
	}
	thread, err := s.threads.Get(ctx, input.ThreadID)
	if err != nil {
		return TaskResult{}, err
	}
	if thread.ReadyAt == nil {
		return TaskResult{}, status_error.ChannelTaskStateConflict
	}
	if input.AssigneeWakerID == thread.LeadWakerID {
		return TaskResult{}, status_error.BadRequest
	}
	channel, err := s.channels.Get(ctx, thread.ChannelID)
	if err != nil {
		return TaskResult{}, err
	}
	if _, err := s.threads.GetMember(ctx, thread.ID, input.AssigneeWakerID); err != nil {
		return TaskResult{}, err
	}
	if input.SenderWakerID != "" {
		if _, err := s.threads.GetMember(ctx, thread.ID, input.SenderWakerID); err != nil {
			return TaskResult{}, err
		}
	}
	dependencyIDs := uniqueIDs(input.DependencyTaskIDs)
	dependencies := make([]models.ChannelTaskDependency, 0, len(dependencyIDs))
	directInput, err := normalizeTaskInput(input.DirectInput)
	if err != nil {
		return TaskResult{}, err
	}
	if directInput != nil {
		input.imReplyLinkID = directInput.IMReplyLinkID
	}
	now := time.Now().UnixMilli()
	taskID := util.New()
	for _, dependencyID := range dependencyIDs {
		dependency, err := s.tasks.Get(ctx, dependencyID)
		if err != nil {
			return TaskResult{}, err
		}
		if dependency.ThreadID != thread.ID || dependency.Status.Terminal() {
			return TaskResult{}, status_error.BadRequest
		}
		dependencies = append(dependencies, models.ChannelTaskDependency{
			TaskID:          taskID,
			DependsOnTaskID: dependency.ID,
			CreatedAt:       now,
		})
	}
	task := models.ChannelTask{
		ID:                  taskID,
		ThreadID:            thread.ID,
		SenderWakerID:       input.SenderWakerID,
		AssigneeWakerID:     input.AssigneeWakerID,
		Subject:             input.Subject,
		Description:         input.Description,
		DirectInput:         directInput,
		IMReplyLinkID:       input.imReplyLinkID,
		DeferUntilLeadIdle:  input.DeferDispatch,
		DelegationSentAt:    now,
		Status:              constants.ChannelTaskStatusPending,
		Priority:            input.Priority,
		RequiresHumanReview: input.RequiresHumanReview,
		DirectDelivery:      input.DirectDelivery,
		ChangedPaths:        []string{},
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	finishAttachments, err := s.prepareTeamInputAttachments(thread, task.ID, directInput, input.claimAttachments)
	if err != nil {
		return TaskResult{}, err
	}
	err = s.transaction(ctx, func(transactionContext context.Context) error {
		if writableErr := s.ensureThreadWritable(transactionContext, thread); writableErr != nil {
			return writableErr
		}
		if directInput != nil {
			if err := s.checkStoppedThreadSubmission(thread, directInput.SubmissionID); err != nil {
				return err
			}
		}
		reserved, reserveErr := s.threads.ReserveTaskSlot(transactionContext, thread.ID)
		if reserveErr != nil {
			return reserveErr
		}
		if !reserved {
			return status_error.ChannelTaskLimitReached
		}
		if err := s.tasks.Create(transactionContext, task, dependencies); err != nil {
			return err
		}
		_, err := s.appendEvent(transactionContext, channel.ID, thread.ID, constants.ChannelEventTaskCreated, task)
		if err != nil {
			return err
		}
		return capability.Commit(transactionContext, map[string]any{"taskId": task.ID, "accepted": true})
	})
	finishAttachments(err == nil)
	if err != nil {
		return TaskResult{}, err
	}
	if input.DeferDispatch {
		return TaskResult{Task: task}, nil
	}
	release()
	ctx = inputContext
	executions, err := s.schedule(ctx, thread.ID)
	if err != nil {
		return TaskResult{Task: task}, err
	}
	updated, err := s.tasks.Get(ctx, task.ID)
	if err == nil {
		task = updated
	}
	return TaskResult{Task: task, Execution: executions[task.ID]}, nil
}

// updatePendingTask 允许负责人在任务尚未执行时修订说明、优先级或执行伙伴。
func (s *service) updatePendingTask(
	ctx context.Context,
	input UpdateTaskInput,
) (models.ChannelTask, error) {
	input.ThreadID = strings.TrimSpace(input.ThreadID)
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.AssigneeWakerID = strings.TrimSpace(input.AssigneeWakerID)
	input.Subject = strings.TrimSpace(input.Subject)
	input.Description = strings.TrimSpace(input.Description)
	if input.ThreadID == "" || input.TaskID == "" ||
		(input.AssigneeWakerID == "" && input.Subject == "" && input.Description == "" && input.Priority == nil) {
		return models.ChannelTask{}, status_error.BadRequest
	}
	locked, release, lockErr := s.acquireThreadOperation(ctx, input.ThreadID)
	if lockErr != nil {
		return models.ChannelTask{}, lockErr
	}
	ctx = locked
	defer release()
	task, err := s.tasks.Get(ctx, input.TaskID)
	if err != nil {
		return models.ChannelTask{}, err
	}
	if task.ThreadID != input.ThreadID || task.Status != constants.ChannelTaskStatusPending {
		return models.ChannelTask{}, status_error.ChannelTaskStateConflict
	}
	if len([]rune(input.Subject)) > constants.ChannelMaximumTaskSubjectRunes {
		return models.ChannelTask{}, status_error.BadRequest
	}
	now := time.Now().UnixMilli()
	fields := map[string]interface{}{
		"defer_until_lead_idle": true,
		"updated_at":            now,
	}
	editedText := (input.Subject != "" && input.Subject != task.Subject) ||
		(input.Description != "" && input.Description != task.Description)
	if editedText && task.IMReplyLinkID != input.imReplyLinkID {
		fields["im_reply_link_id"] = ""
	}
	if input.AssigneeWakerID != "" {
		if _, memberErr := s.threads.GetMember(ctx, task.ThreadID, input.AssigneeWakerID); memberErr != nil {
			return models.ChannelTask{}, memberErr
		}
		fields["assignee_waker_id"] = input.AssigneeWakerID
		if input.AssigneeWakerID != task.AssigneeWakerID {
			fields["delegation_sent_at"] = now
		}
	}
	if input.Subject != "" {
		fields["subject"] = input.Subject
	}
	if input.Description != "" {
		fields["description"] = input.Description
	}
	if input.Priority != nil {
		fields["priority"] = *input.Priority
	}
	thread, err := s.threads.Get(ctx, task.ThreadID)
	if err != nil {
		return models.ChannelTask{}, err
	}
	if input.AssigneeWakerID == thread.LeadWakerID {
		return models.ChannelTask{}, status_error.BadRequest
	}
	if err := s.ensureThreadWritable(ctx, thread); err != nil {
		return models.ChannelTask{}, err
	}
	err = s.transaction(ctx, func(transactionContext context.Context) error {
		if writableErr := s.ensureThreadWritable(transactionContext, thread); writableErr != nil {
			return writableErr
		}
		if updateErr := s.tasks.UpdateFields(transactionContext, task.ID, fields); updateErr != nil {
			return updateErr
		}
		_, eventErr := s.appendEvent(
			transactionContext,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventTaskUpdated,
			map[string]interface{}{"taskId": task.ID},
		)
		if eventErr != nil {
			return eventErr
		}
		return capability.Commit(transactionContext, map[string]any{"taskId": task.ID, "updated": true})
	})
	if err != nil {
		return models.ChannelTask{}, err
	}
	return s.tasks.Get(ctx, task.ID)
}

func (s *service) schedule(
	ctx context.Context,
	threadID string,
) (map[string]*models.Execution, error) {
	result := make(map[string]*models.Execution)
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return result, err
	}
	if err := s.ensureThreadWritable(ctx, thread); err != nil {
		return result, err
	}
	snapshot, err := s.tasks.LoadDispatchSnapshot(ctx, threadID)
	if err != nil {
		return result, err
	}
	sort.SliceStable(snapshot.Tasks, func(i, j int) bool {
		if snapshot.Tasks[i].Priority != snapshot.Tasks[j].Priority {
			return snapshot.Tasks[i].Priority > snapshot.Tasks[j].Priority
		}
		if snapshot.Tasks[i].CreatedAt != snapshot.Tasks[j].CreatedAt {
			return snapshot.Tasks[i].CreatedAt < snapshot.Tasks[j].CreatedAt
		}
		return snapshot.Tasks[i].ID < snapshot.Tasks[j].ID
	})
	// 快照只筛选候选，实际派发配额与成员占用由 ClaimForDispatch 原子确认。
	leadIdleKnown := false
	leadIdle := false
	for _, task := range snapshot.Tasks {
		if !dispatchableTask(thread, snapshot, task) {
			continue
		}
		if task.DeferUntilLeadIdle {
			if !leadIdleKnown {
				leadSummary, summaryErr := s.chats.GetChatSummary(ctx, thread.LeadChatID)
				if summaryErr != nil {
					return result, summaryErr
				}
				leadIdle = leadSummary.Chat.ActiveExecutionID == ""
				leadIdleKnown = true
			}
			if !leadIdle {
				continue
			}
			if updateErr := s.tasks.UpdateFields(ctx, task.ID, map[string]interface{}{
				"defer_until_lead_idle": false,
				"updated_at":            time.Now().UnixMilli(),
			}); updateErr != nil {
				return result, updateErr
			}
			task.DeferUntilLeadIdle = false
		}
		accepted, err := s.dispatchTask(ctx, thread, task, snapshot.DependenciesByTaskID[task.ID])
		if err != nil {
			return result, err
		}
		if accepted != nil {
			result[task.ID] = accepted
			snapshot.ActiveCount++
			snapshot.ActiveAssigneeWakerIDs[task.AssigneeWakerID] = struct{}{}
		}
	}
	return result, nil
}

func dispatchableTask(thread models.ChannelThread, snapshot controllers.ChannelDispatchSnapshot, task models.ChannelTask) bool {
	_, assigneeActive := snapshot.ActiveAssigneeWakerIDs[task.AssigneeWakerID]
	return task.Status == constants.ChannelTaskStatusPending &&
		snapshot.ActiveCount < int64(thread.MaxParallel) &&
		!assigneeActive &&
		allDependenciesTerminal(snapshot.DependenciesByTaskID[task.ID])
}

func (s *service) dispatchTask(
	ctx context.Context,
	thread models.ChannelThread,
	task models.ChannelTask,
	dependencies []models.ChannelTask,
) (*models.Execution, error) {
	member, err := s.threads.GetMember(ctx, thread.ID, task.AssigneeWakerID)
	if err != nil {
		return nil, err
	}
	summary, err := s.chats.GetChatSummary(ctx, member.ChatID)
	if err != nil {
		return nil, err
	}
	if summary.Chat.ActiveExecutionID != "" {
		s.watchRecoveryDrain(thread.ID)
		return nil, nil
	}
	automaticRunID, err := s.automaticWorkSource(ctx, thread)
	if err != nil {
		return nil, err
	}
	followUps, err := s.tasks.ListPendingFollowUps(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	followUps = nextFollowUpBatch(followUps)
	reason, err := s.taskAttemptReason(ctx, task, followUps)
	if err != nil {
		return nil, err
	}
	workInput, err := taskWorkInput(task, dependencies, followUps)
	if err != nil {
		return nil, err
	}
	teamInstruction, err := s.teamInstruction(ctx, thread, task.AssigneeWakerID)
	if err != nil {
		return nil, err
	}
	if task.DirectDelivery {
		teamInstruction.Content += agentapi.TeamDirectDeliveryInstructions
	}
	fence := task.CurrentFence + 1
	now := time.Now().UnixMilli()
	attempt := models.ChannelTaskAttempt{
		ID:               util.New(),
		TaskID:           task.ID,
		ChatID:           member.ChatID,
		ExecutionID:      util.New(),
		IMReplyLinkID:    workInput.IMReplyLinkID,
		AssigneeWakerID:  task.AssigneeWakerID,
		Fence:            fence,
		Reason:           reason,
		DelegationSentAt: task.DelegationSentAt,
		Status:           constants.ExecutionStatusPending,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	claimed := false
	if err := s.transaction(ctx, func(transactionContext context.Context) error {
		var claimErr error
		claimed, claimErr = s.tasks.ClaimForDispatch(
			transactionContext,
			task.ID,
			task.ThreadID,
			task.AssigneeWakerID,
			task.CurrentFence,
			fence,
			thread.MaxParallel,
			now,
		)
		if claimErr != nil || !claimed {
			return claimErr
		}
		if createErr := s.tasks.CreateAttempt(transactionContext, attempt); createErr != nil {
			return createErr
		}
		if len(followUps) == 0 {
			return nil
		}
		ids := make([]string, 0, len(followUps))
		for _, followUp := range followUps {
			ids = append(ids, followUp.ID)
		}
		return s.tasks.MarkFollowUpsDelivered(transactionContext, ids, attempt.ID, now)
	}); err != nil {
		// 精确撤回补充可使派发快照过期。领取事务已回滚，继续处理剩余队列。
		if errors.Is(err, status_error.ChannelTaskStateConflict) {
			s.watchRecoveryDrain(thread.ID)
			return nil, nil
		}
		return nil, err
	}
	if !claimed {
		return nil, nil
	}
	toolCapabilities := []execution.ToolCapability{}
	capabilityToken := ""
	if task.AssigneeWakerID != thread.LeadWakerID {
		capability, token, capabilityErr := s.mintMemberCapability(thread, attempt)
		if capabilityErr != nil {
			return nil, s.rejectTaskDispatch(ctx, thread, attempt, capabilityErr)
		}
		toolCapabilities = append(toolCapabilities, capability)
		capabilityToken = token
	}
	accepted, err := s.startThreadExecution(ctx, thread, execution.WorkIntent{AutomaticRunID: automaticRunID,
		ExecutionID: attempt.ExecutionID,
		Chat:        execution.ChatTarget{ChatID: member.ChatID},
		Input:       workInput,
		Options: execution.WorkOptions{
			ConfigurationResolved: true,
			RuntimeID:             member.RuntimeID,
			AccountID:             member.AccountID,
			Model:                 member.Model,
			Reasoning:             member.Reasoning,
			Fast:                  member.Fast,
			PermissionLevel:       member.PermissionLevel,
			SessionPolicy:         constants.RuntimeSessionPolicyContinue,
			ToolCapabilities:      toolCapabilities,
			SupplementalInstructions: []agentapi.Instruction{
				teamInstruction,
			},
		},
		Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
		AdmissionBoundary: func(ctx context.Context, commit func(context.Context) error) error {
			current, err := s.tasks.Get(ctx, task.ID)
			if err != nil {
				return err
			}
			if current.Status != constants.ChannelTaskStatusInProgress || current.CurrentFence != attempt.Fence {
				return status_error.ChannelTaskAttemptStale
			}
			if taskCancellationRequested(current, attempt.Fence) {
				return status_error.OperationCanceled
			}
			return commit(ctx)
		},
	})
	if err != nil {
		s.capabilities.Revoke(capabilityToken)
		return nil, s.rejectTaskDispatch(ctx, thread, attempt, err)
	}
	acceptedAt := time.Now().UnixMilli()
	if accepted.Execution.ID != attempt.ExecutionID {
		s.capabilities.Revoke(capabilityToken)
		if cancelErr := s.executions.CancelExecutionAndWait(ctx, member.ChatID, accepted.Execution.ID); cancelErr != nil {
			logx.Errorf("cancel mismatched channel task execution failed: thread_id=%s task_id=%s attempt_id=%s expected_execution_id=%s actual_execution_id=%s chat_id=%s error=%v", thread.ID, task.ID, attempt.ID, attempt.ExecutionID, accepted.Execution.ID, member.ChatID, cancelErr)
		}
		return nil, s.rejectTaskDispatch(ctx, thread, attempt, status_error.ChannelTaskAttemptStale)
	}
	attempt.StartedAt = &acceptedAt
	err = s.transaction(ctx, func(transactionContext context.Context) error {
		bound, bindErr := s.tasks.BindAttemptExecution(
			transactionContext,
			attempt.ID,
			attempt.ExecutionID,
			acceptedAt,
		)
		if bindErr != nil {
			return bindErr
		}
		if !bound {
			return status_error.ChannelTaskAttemptStale
		}
		_, err := s.appendEvent(
			transactionContext,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventTaskStarted,
			map[string]interface{}{
				"taskId":      task.ID,
				"attemptId":   attempt.ID,
				"executionId": attempt.ExecutionID,
				"fence":       fence,
				"reason":      reason,
			},
		)
		return err
	})
	if err != nil {
		s.capabilities.Revoke(capabilityToken)
		if cancelErr := s.executions.CancelExecutionAndWait(ctx, member.ChatID, attempt.ExecutionID); cancelErr != nil {
			logx.Errorf("cancel uncommitted channel task execution failed: thread_id=%s task_id=%s attempt_id=%s execution_id=%s chat_id=%s error=%v", thread.ID, task.ID, attempt.ID, attempt.ExecutionID, member.ChatID, cancelErr)
		}
		return nil, s.rejectTaskDispatch(ctx, thread, attempt, err)
	}
	for _, followUp := range followUps {
		if len(followUp.Payload.Attachments) > 0 {
			s.releaseTeamInputAttachments(thread, followUp.ID)
		}
	}
	s.watchAttempt(attempt, task.ThreadID, capabilityToken)
	return &accepted.Execution, nil
}

// rejectTaskDispatch 释放未绑定的尝试，并与恢复监听的退出使用同一团队保护。
func (s *service) rejectTaskDispatch(ctx context.Context, thread models.ChannelThread, attempt models.ChannelTaskAttempt, cause error) error {
	locked, release, err := teamwork.Acquire(context.WithoutCancel(ctx), thread.ChannelID)
	if err != nil {
		return err
	}
	defer release()
	stopped, err := s.releaseUnstartedAttempt(locked, attempt)
	if err != nil {
		logx.Errorf("release unstarted channel task attempt failed: thread_id=%s task_id=%s attempt_id=%s execution_id=%s chat_id=%s fence=%d error=%v",
			thread.ID, attempt.TaskID, attempt.ID, attempt.ExecutionID, attempt.ChatID, attempt.Fence, err)
		return cause
	}
	if stopped || isChatBusy(cause) || errors.Is(cause, agentapi.ErrTurnAdmissionBusy) {
		s.watchRecoveryDrain(thread.ID)
		return nil
	}
	return cause
}

// taskAttemptReason 区分同成员补充与真实改派，后续委派投影无需扫描全部补充历史。
func (s *service) taskAttemptReason(ctx context.Context, task models.ChannelTask, followUps []models.ChannelTaskFollowUp) (constants.ChannelAttemptReason, error) {
	if task.CurrentFence == 0 {
		return constants.ChannelAttemptReasonInitial, nil
	}
	previous, err := s.tasks.GetCurrentAttempt(ctx, task.ID, task.CurrentFence)
	if err != nil {
		return "", err
	}
	// 未接纳的 reservation 只消耗围栏；重试接纳沿用同一派发意图。
	if previous.StartedAt == nil && previous.AssigneeWakerID == task.AssigneeWakerID {
		return previous.Reason, nil
	}
	if len(followUps) == 0 {
		return constants.ChannelAttemptReasonRetry, nil
	}
	if previous.AssigneeWakerID != task.AssigneeWakerID {
		return constants.ChannelAttemptReasonReassigned, nil
	}
	if followUps[0].Actor == constants.ChannelMessageActorLead {
		return constants.ChannelAttemptReasonLeadFollowUp, nil
	}
	return constants.ChannelAttemptReasonUserFollowUp, nil
}

func (s *service) releaseUnstartedAttempt(
	ctx context.Context,
	attempt models.ChannelTaskAttempt,
) (bool, error) {
	stopped := false
	err := s.transaction(ctx, func(transactionContext context.Context) error {
		current, err := s.tasks.Get(transactionContext, attempt.TaskID)
		if err != nil {
			return err
		}
		stopped = false
		if current.Status != constants.ChannelTaskStatusInProgress || current.CurrentFence != attempt.Fence {
			return nil
		}
		taskStatus := constants.ChannelTaskStatusPending
		if taskCancellationRequested(current, attempt.Fence) {
			taskStatus = constants.ChannelTaskStatusCancelled
		}
		released, err := s.tasks.ReleaseDispatchClaim(
			transactionContext,
			attempt.TaskID,
			attempt.ID,
			attempt.Fence,
			constants.ExecutionStatusCanceled,
			taskStatus,
			"task execution was not started",
			time.Now().UnixMilli(),
		)
		if err != nil || !released {
			return err
		}
		if err := s.tasks.ReleaseAttemptFollowUps(transactionContext, attempt.ID); err != nil {
			return err
		}
		stopped = taskStatus == constants.ChannelTaskStatusCancelled
		if !stopped {
			return nil
		}
		thread, err := s.threads.Get(transactionContext, current.ThreadID)
		if err != nil {
			return err
		}
		_, err = s.appendEvent(transactionContext, thread.ChannelID, thread.ID, constants.ChannelEventTaskCancelled, map[string]interface{}{"taskId": current.ID})
		return err
	})
	return stopped && err == nil, err
}

func taskCancellationRequested(task models.ChannelTask, fence int) bool {
	return task.CancelRequestedAt != nil && task.CancelRequestedFence == fence
}

// taskDependencyFact 携带成员执行时可用的前置任务结果。
type taskDependencyFact struct {
	// Subject 是前置任务的标题。
	Subject string `json:"subject"`
	// Status 是前置任务记录中的状态值。
	Status string `json:"status"`
	// Report 是前置报告或其有界预览。
	Report string `json:"report,omitempty"`
	// Truncated 表示报告超过可交付上限。
	Truncated bool `json:"truncated,omitempty"`
}

func taskDependencyContext(dependencies []models.ChannelTask) []workinput.Instruction {
	if len(dependencies) == 0 {
		return nil
	}
	results := make([]taskDependencyFact, 0, len(dependencies))
	for _, dependency := range dependencies {
		report, truncated := teammateReportContent(dependency.Status, dependency.Result, dependency.ErrorMessage, []string(dependency.ChangedPaths))
		if truncated {
			report = strings.TrimSuffix(report, "\n\n"+agentapi.TeamReportTruncationNotice)
		}
		results = append(results, taskDependencyFact{Subject: dependency.Subject, Status: string(dependency.Status), Report: report, Truncated: truncated})
	}
	encoded, _ := json.Marshal(results)
	return []workinput.Instruction{{Name: agentapi.TeamDependencyContextName, Content: string(encoded)}}
}

// normalizeTaskInput 复制需要跨排队和重启保留的完整工作输入。
func normalizeTaskInput(input *execution.WorkInput) (*execution.WorkInput, error) {
	if input == nil {
		return nil, nil
	}
	cloned := *input
	cloned.Actor = strings.TrimSpace(cloned.Actor)
	if cloned.Actor == "" {
		cloned.Actor = string(constants.ChannelMessageActorUser)
	}
	if strings.TrimSpace(cloned.Content) == "" {
		return nil, status_error.BadRequest
	}
	cloned.Attachments = append([]execution.AttachmentInput(nil), cloned.Attachments...)
	cloned.CodeReferences = append([]execution.CodeReferenceInput(nil), cloned.CodeReferences...)
	cloned.PluginIDs = append([]string(nil), cloned.PluginIDs...)
	cloned.SkillIDs = append([]string(nil), cloned.SkillIDs...)
	cloned.ConnectorIDs = append([]string(nil), cloned.ConnectorIDs...)
	return &cloned, nil
}

// taskWorkInput 恢复显式用户输入或构造内部任务输入，并合并等待续跑的完整用户补充。
func taskWorkInput(
	task models.ChannelTask,
	dependencies []models.ChannelTask,
	followUps []models.ChannelTaskFollowUp,
) (execution.WorkInput, error) {
	workInput := execution.WorkInput{
		Actor:               string(constants.ChannelMessageActorLead),
		Content:             strings.TrimSpace(task.Subject + "\n\n" + task.Description),
		ContextInstructions: taskDependencyContext(dependencies),
		IMReplyLinkID:       task.IMReplyLinkID,
	}
	if task.DirectInput != nil {
		directInput := *task.DirectInput
		if task.CurrentFence == 0 {
			workInput = directInput
			workInput.IMReplyLinkID = task.IMReplyLinkID
		} else {
			appendWorkInputReferences(&workInput, directInput)
		}
	}
	if len(followUps) == 0 {
		return workInput, nil
	}
	followUpActor := followUps[0].Actor
	if followUpActor == "" {
		followUpActor = constants.ChannelMessageActorUser
	}
	if task.CurrentFence > 0 {
		workInput = execution.WorkInput{Actor: string(followUpActor)}
	} else if task.DirectInput == nil {
		workInput = execution.WorkInput{Actor: string(followUpActor), Content: task.Description}
	}
	generatedOnly := strings.TrimSpace(workInput.Content) == "" || workInput.AttachmentOnly || workInput.CodeReferenceOnly
	hasCodeReferencePrompt := workInput.CodeReferenceOnly
	for _, followUp := range followUps {
		input := followUp.Payload
		if strings.TrimSpace(input.Content) == "" {
			input = execution.WorkInput{Actor: string(followUpActor), Content: followUp.Text}
		}
		if workInput.Content != "" {
			workInput.Content += "\n\n"
		}
		workInput.Content += input.Content
		generatedOnly = generatedOnly && (input.AttachmentOnly || input.CodeReferenceOnly)
		hasCodeReferencePrompt = hasCodeReferencePrompt || input.CodeReferenceOnly
		appendWorkInputReferences(&workInput, input)
	}
	workInput.AttachmentOnly = generatedOnly && !hasCodeReferencePrompt
	workInput.CodeReferenceOnly = generatedOnly && hasCodeReferencePrompt
	workInput.Actor = string(followUpActor)
	workInput.IMReplyLinkID = followUps[0].Payload.IMReplyLinkID
	return workInput, nil
}

// nextFollowUpBatch 保持补充的全局先后顺序，限制同一批使用同一作者角色和回复来源。
func nextFollowUpBatch(followUps []models.ChannelTaskFollowUp) []models.ChannelTaskFollowUp {
	if len(followUps) < 2 {
		return followUps
	}
	actor := followUps[0].Actor
	if actor == "" {
		actor = constants.ChannelMessageActorUser
	}
	end := 1
	for end < len(followUps) {
		nextActor := followUps[end].Actor
		if nextActor == "" {
			nextActor = constants.ChannelMessageActorUser
		}
		if nextActor != actor || followUps[end].Payload.IMReplyLinkID != followUps[0].Payload.IMReplyLinkID {
			break
		}
		end++
	}
	return followUps[:end]
}

// appendWorkInputReferences 把一条后续输入的有序能力引用追加到本次成员执行。
func appendWorkInputReferences(target *execution.WorkInput, source execution.WorkInput) {
	target.Attachments = append(target.Attachments, source.Attachments...)
	target.CodeReferences = append(target.CodeReferences, source.CodeReferences...)
	target.PluginIDs = append(target.PluginIDs, source.PluginIDs...)
	target.SkillIDs = append(target.SkillIDs, source.SkillIDs...)
	target.ConnectorIDs = append(target.ConnectorIDs, source.ConnectorIDs...)
}

func allDependenciesTerminal(tasks []models.ChannelTask) bool {
	for _, task := range tasks {
		if !task.Status.Terminal() {
			return false
		}
	}
	return true
}

func uniqueIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func (s *service) activeTaskForAssignee(
	ctx context.Context,
	threadID string,
	wakerID string,
) (*models.ChannelTask, error) {
	task, exists, err := s.tasks.GetActiveForAssignee(ctx, threadID, wakerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return &task, nil
}
