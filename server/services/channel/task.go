package channel

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
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
	locked, release, lockErr := s.acquireThreadOperation(ctx, input.ThreadID)
	if lockErr != nil {
		return TaskResult{}, lockErr
	}
	ctx = locked
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
		DeferUntilLeadIdle:  input.DeferDispatch,
		Status:              constants.ChannelTaskStatusPending,
		Priority:            input.Priority,
		RequiresHumanReview: input.RequiresHumanReview,
		DirectDelivery:      input.DirectDelivery,
		ChangedPaths:        []string{},
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	err = s.transaction(ctx, func(transactionContext context.Context) error {
		if writableErr := s.ensureThreadWritable(transactionContext, thread); writableErr != nil {
			return writableErr
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
		return agentcommand.Commit(transactionContext, map[string]any{"taskId": task.ID, "accepted": true})
	})
	if err != nil {
		return TaskResult{}, err
	}
	if input.DeferDispatch {
		return TaskResult{Task: task}, nil
	}
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
	fields := map[string]interface{}{
		"defer_until_lead_idle": true,
		"updated_at":            time.Now().UnixMilli(),
	}
	if input.AssigneeWakerID != "" {
		if _, memberErr := s.threads.GetMember(ctx, task.ThreadID, input.AssigneeWakerID); memberErr != nil {
			return models.ChannelTask{}, memberErr
		}
		fields["assignee_waker_id"] = input.AssigneeWakerID
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
		return agentcommand.Commit(transactionContext, map[string]any{"taskId": task.ID, "updated": true})
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
	locked, release, lockErr := s.acquireThreadOperation(ctx, threadID)
	if lockErr != nil {
		return nil, lockErr
	}
	ctx = locked
	defer release()
	result := make(map[string]*models.Execution)
	var thread models.ChannelThread
	var snapshot controllers.ChannelDispatchSnapshot
	if err := func() error {
		releaseSchedule := s.acquireScheduleLock(threadID)
		defer releaseSchedule()
		var err error
		thread, err = s.threads.Get(ctx, threadID)
		if err != nil {
			return err
		}
		if err := s.ensureThreadWritable(ctx, thread); err != nil {
			return err
		}
		snapshot, err = s.tasks.LoadDispatchSnapshot(ctx, threadID)
		if err != nil {
			return err
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
		return nil
	}(); err != nil {
		return result, err
	}
	// 从这里开始只使用不可变快照；领取与配额由 ClaimForDispatch 的数据库 CAS 决定，
	// 因而 Runtime/Chat 外部调用不会占用 thread 调度锁。
	active := snapshot.ActiveCount
	activeAssignees := snapshot.ActiveAssigneeWakerIDs
	leadIdleKnown := false
	leadIdle := false
	for _, task := range snapshot.Tasks {
		if task.Status != constants.ChannelTaskStatusPending || active >= int64(thread.MaxParallel) {
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
		dependencies := snapshot.DependenciesByTaskID[task.ID]
		if !allDependenciesTerminal(dependencies) {
			continue
		}
		if _, assigneeActive := activeAssignees[task.AssigneeWakerID]; assigneeActive {
			continue
		}
		accepted, err := s.dispatchTask(ctx, thread, task, dependencies)
		if err != nil {
			return result, err
		}
		if accepted != nil {
			result[task.ID] = accepted
			active++
			activeAssignees[task.AssigneeWakerID] = struct{}{}
		}
	}
	return result, nil
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
	reason := constants.ChannelAttemptReasonInitial
	if len(followUps) > 0 {
		if followUps[0].Actor == constants.ChannelMessageActorLead {
			reason = constants.ChannelAttemptReasonLeadFollowUp
		} else {
			reason = constants.ChannelAttemptReasonUserFollowUp
		}
	} else if task.CurrentFence > 0 {
		reason = constants.ChannelAttemptReasonRetry
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
		teamInstruction.Content += "\nDirect delivery: put the complete checked answer in `allycli team reports submit` with `readyForUser=true` only when it can be shown to the user without lead synthesis. Otherwise report the facts without that flag.\n"
	}
	fence := task.CurrentFence + 1
	now := time.Now().UnixMilli()
	attempt := models.ChannelTaskAttempt{
		ID:              util.New(),
		TaskID:          task.ID,
		ChatID:          member.ChatID,
		ExecutionID:     util.New(),
		AssigneeWakerID: task.AssigneeWakerID,
		Fence:           fence,
		Reason:          reason,
		Status:          constants.ExecutionStatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
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
			if releaseErr := s.releaseUnstartedAttempt(ctx, attempt); releaseErr != nil {
				logx.Errorf(
					"release channel task attempt after member capability failure failed: thread_id=%s task_id=%s attempt_id=%s execution_id=%s chat_id=%s fence=%d error=%v",
					thread.ID,
					task.ID,
					attempt.ID,
					attempt.ExecutionID,
					member.ChatID,
					attempt.Fence,
					releaseErr,
				)
			}
			return nil, capabilityErr
		}
		toolCapabilities = append(toolCapabilities, capability)
		capabilityToken = token
	}
	accepted, err := s.executions.Start(ctx, execution.WorkIntent{AutomaticRunID: automaticRunID,
		ExecutionID: attempt.ExecutionID,
		Chat:        execution.ChatTarget{ChatID: member.ChatID},
		Input:       workInput,
		Options: execution.WorkOptions{
			RuntimeID:        member.RuntimeID,
			AccountID:        member.AccountID,
			Model:            member.Model,
			Reasoning:        member.Reasoning,
			PermissionLevel:  member.PermissionLevel,
			SessionPolicy:    constants.RuntimeSessionPolicyContinue,
			ToolCapabilities: toolCapabilities,
			SupplementalInstructions: []runtimecontract.Instruction{
				teamInstruction,
			},
		},
		Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil {
		s.capabilities.Revoke(capabilityToken)
		if releaseErr := s.releaseUnstartedAttempt(ctx, attempt); releaseErr != nil {
			logx.Errorf(
				"release unstarted channel task attempt failed: thread_id=%s task_id=%s attempt_id=%s execution_id=%s chat_id=%s fence=%d error=%v",
				thread.ID,
				task.ID,
				attempt.ID,
				attempt.ExecutionID,
				member.ChatID,
				attempt.Fence,
				releaseErr,
			)
		}
		if isChatBusy(err) {
			return nil, nil
		}
		return nil, err
	}
	acceptedAt := time.Now().UnixMilli()
	if accepted.Execution.ID != attempt.ExecutionID {
		s.capabilities.Revoke(capabilityToken)
		if cancelErr := s.executions.CancelAndWait(ctx, member.ChatID); cancelErr != nil {
			logx.Errorf("cancel mismatched channel task execution failed: thread_id=%s task_id=%s attempt_id=%s expected_execution_id=%s actual_execution_id=%s chat_id=%s error=%v", thread.ID, task.ID, attempt.ID, attempt.ExecutionID, accepted.Execution.ID, member.ChatID, cancelErr)
		}
		if releaseErr := s.releaseUnstartedAttempt(ctx, attempt); releaseErr != nil {
			logx.Errorf("release mismatched channel task attempt failed: thread_id=%s task_id=%s attempt_id=%s execution_id=%s chat_id=%s fence=%d error=%v", thread.ID, task.ID, attempt.ID, attempt.ExecutionID, member.ChatID, attempt.Fence, releaseErr)
		}
		return nil, status_error.ChannelTaskAttemptStale
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
		if cancelErr := s.executions.CancelAndWait(ctx, member.ChatID); cancelErr != nil {
			logx.Errorf("cancel uncommitted channel task execution failed: thread_id=%s task_id=%s attempt_id=%s execution_id=%s chat_id=%s error=%v", thread.ID, task.ID, attempt.ID, attempt.ExecutionID, member.ChatID, cancelErr)
		}
		if releaseErr := s.releaseUnstartedAttempt(ctx, attempt); releaseErr != nil {
			logx.Errorf(
				"release unbound channel task attempt failed: thread_id=%s task_id=%s attempt_id=%s execution_id=%s chat_id=%s fence=%d error=%v",
				thread.ID,
				task.ID,
				attempt.ID,
				attempt.ExecutionID,
				member.ChatID,
				attempt.Fence,
				releaseErr,
			)
		}
		return nil, err
	}
	s.watchAttempt(attempt, task.ThreadID, capabilityToken)
	return &accepted.Execution, nil
}

func (s *service) releaseUnstartedAttempt(
	ctx context.Context,
	attempt models.ChannelTaskAttempt,
) error {
	return s.transaction(ctx, func(transactionContext context.Context) error {
		released, err := s.tasks.ReleaseDispatchClaim(
			transactionContext,
			attempt.TaskID,
			attempt.ID,
			attempt.Fence,
			constants.ExecutionStatusCanceled,
			"task execution was not started",
			time.Now().UnixMilli(),
		)
		if err != nil || !released {
			return err
		}
		return s.tasks.ReleaseAttemptFollowUps(transactionContext, attempt.ID)
	})
}

// dispatchTaskContent 按任务上下文组装成员任务正文：标题、说明与上游结果。
func dispatchTaskContent(
	task models.ChannelTask,
	dependencies []models.ChannelTask,
) string {
	var builder strings.Builder
	builder.WriteString("[Assigned task]: ")
	builder.WriteString(task.Subject)
	if description := strings.TrimSpace(task.Description); description != "" {
		builder.WriteString("\n\n")
		builder.WriteString(description)
	}
	if len(dependencies) > 0 {
		builder.WriteString("\n\n=== Completed blocker task results ===")
		for _, dependency := range dependencies {
			dependencyReport, _ := teammateReportContent(
				dependency.Status,
				dependency.Result,
				dependency.ErrorMessage,
				[]string(dependency.ChangedPaths),
			)
			builder.WriteString(fmt.Sprintf(
				"\n\n--- Result from blocker task (%s) [%s] ---\n%s",
				dependency.Subject,
				dependency.Status,
				dependencyReport,
			))
		}
	}
	return builder.String()
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
		Actor:   string(constants.ChannelMessageActorLead),
		Content: dispatchTaskContent(task, dependencies),
	}
	if task.DirectInput != nil {
		directInput := *task.DirectInput
		if task.CurrentFence == 0 {
			workInput = directInput
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
	for _, followUp := range followUps {
		input := followUp.Payload
		if strings.TrimSpace(input.Content) == "" {
			input = execution.WorkInput{Actor: string(followUpActor), Content: followUp.Text}
		}
		if workInput.Content != "" {
			workInput.Content += "\n\n"
		}
		workInput.Content += input.Content
		appendWorkInputReferences(&workInput, input)
	}
	workInput.Actor = string(followUpActor)
	return workInput, nil
}

// nextFollowUpBatch 保持补充的全局先后顺序，并避免一次成员输入混合用户和负责人作者。
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
		if nextActor != actor {
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
