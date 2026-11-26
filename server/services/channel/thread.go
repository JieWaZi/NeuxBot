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
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// SubmitMessage 统一承接两个 HTTP 入口，并保证首次发送与续发严格互斥。
func (s *service) SubmitMessage(
	ctx context.Context,
	input SubmitMessageInput,
) (SubmitMessageResult, error) {
	if (input.Create == nil) == (input.Continue == nil) {
		return SubmitMessageResult{}, status_error.BadRequest
	}
	if input.Create != nil {
		result, err := s.createThread(ctx, *input.Create)
		if err != nil {
			return SubmitMessageResult{}, err
		}
		return SubmitMessageResult{Thread: &result}, nil
	}
	result, err := s.sendMessage(ctx, *input.Continue)
	if err != nil {
		return SubmitMessageResult{}, err
	}
	return SubmitMessageResult{Message: &result}, nil
}

// directedPluginReferences 为公开首发保留成员当时选择的插件版本，不向负责人交付能力。
func (s *service) directedPluginReferences(ctx context.Context, wakerID string, ids []string) ([]plugins.Snapshot, error) {
	if s.plugins == nil {
		return nil, status_error.PluginNotReady
	}
	var refs []models.PluginReference
	if err := s.transaction(ctx, func(txctx context.Context) error {
		var err error
		refs, err = s.plugins.InstalledReferences(txctx, wakerID)
		return err
	}); err != nil {
		return nil, err
	}
	byID := make(map[string]models.PluginReference, len(refs))
	for _, ref := range refs {
		byID[ref.ID] = ref
	}
	snapshots := make([]plugins.Snapshot, 0, len(ids))
	for _, id := range ids {
		ref, ok := byID[strings.TrimSpace(id)]
		if !ok {
			return nil, status_error.PluginNotReady
		}
		snapshots = append(snapshots, plugins.Snapshot{ID: ref.ID, Name: ref.Name, VersionID: ref.VersionID, Version: ref.Version})
	}
	return snapshots, nil
}

// createThread 在首次发送时建立成员快照和一人一个 Chat，并按提及路由启动负责人执行。
func (s *service) createThread(
	ctx context.Context,
	input CreateThreadInput,
) (ThreadResult, error) {
	input.ChannelID = strings.TrimSpace(input.ChannelID)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.WorkingDirectory = strings.TrimSpace(input.WorkingDirectory)
	input.MentionWakerID = strings.TrimSpace(input.MentionWakerID)
	if input.Input.ReplyToMessageID != "" {
		return ThreadResult{}, status_error.ChatReplyTargetUnavailable
	}
	if input.Input.AttachmentOnly && len(input.Input.Attachments) == 0 {
		return ThreadResult{}, status_error.MessageAttachmentInvalid
	}
	if input.Input.Actor != constants.AutomaticTaskActor {
		input.Input.Actor = string(constants.ChannelMessageActorUser)
	}
	if input.ChannelID == "" || input.ClientRequestID == "" ||
		(strings.TrimSpace(input.Input.Content) == "" && input.Input.Workflow == nil) ||
		(input.Input.CodeReferenceOnly && len(input.Input.CodeReferences) == 0) ||
		(input.Input.Workflow != nil && input.MentionWakerID != "") ||
		(input.ProjectID != "" && input.WorkingDirectory != "") {
		return ThreadResult{}, status_error.BadRequest
	}
	inputContext := ctx
	locked, releaseTeam, lockErr := teamwork.Acquire(ctx, input.ChannelID)
	if lockErr != nil {
		return ThreadResult{}, lockErr
	}
	ctx = locked
	release := sync.OnceFunc(releaseTeam)
	defer release()
	normalizedConfigs, err := normalizeMemberExecutionConfigs(input.MemberConfigs)
	if err != nil {
		return ThreadResult{}, err
	}
	sort.SliceStable(normalizedConfigs, func(left int, right int) bool {
		return normalizedConfigs[left].WakerID < normalizedConfigs[right].WakerID
	})
	input.MemberConfigs = normalizedConfigs
	requestIdentity, err := createThreadRequestIdentity(input)
	if err != nil {
		return ThreadResult{}, err
	}
	existing, exists, err := s.threads.GetByClientRequestID(
		ctx,
		input.ChannelID,
		requestIdentity.ID,
	)
	if err != nil {
		return ThreadResult{}, err
	}
	if exists {
		result, replayed, err := s.replayCreateThread(ctx, existing, requestIdentity)
		if err == nil && replayed && existing.ReadyAt == nil {
			release()
			if reconcileErr := s.ReconcileThread(inputContext, result.Thread.ID); reconcileErr != nil {
				logx.Warnf("reconcile recovered channel thread failed: thread_id=%s error=%v", result.Thread.ID, reconcileErr)
			}
			result.Tasks, err = s.tasks.ListByThread(inputContext, result.Thread.ID)
		}
		if err != nil || replayed {
			return result, err
		}
	}
	if input.Input.SubmissionID != "" && s.executions.IsSubmissionStopped(initialLeadChatID(input.ChannelID, requestIdentity), input.Input.SubmissionID) {
		return ThreadResult{}, status_error.OperationCanceled
	}
	if input.Input.Workflow != nil {
		if s.freezeWorkflowSelection == nil {
			return ThreadResult{}, status_error.WorkflowCapabilityUnsupported
		}
		input.Input.Workflow, err = s.freezeWorkflowSelection(ctx, input.Input.Workflow)
		if err != nil {
			return ThreadResult{}, err
		}
	}
	channel, err := s.channels.Get(ctx, input.ChannelID)
	if err != nil {
		return ThreadResult{}, err
	}
	configured, err := s.channels.ListMembers(ctx, channel.ID)
	if err != nil {
		return ThreadResult{}, err
	}
	if len(configured) < constants.ChannelMinimumMemberCount || len(configured) > constants.ChannelMaximumMemberCount {
		return ThreadResult{}, status_error.ChannelMemberCountInvalid
	}
	configured, err = ApplyMemberConfigOverrides(configured, input.MemberConfigs)
	if err != nil {
		return ThreadResult{}, err
	}
	if input.ConfigurationResolved && len(input.MemberConfigs) != len(configured) {
		return ThreadResult{}, status_error.ChannelMemberCountInvalid
	}
	// 已保存成员及上游已接纳的覆盖保持原生空值；只有新覆盖在此保存边界解析默认。
	if !input.ConfigurationResolved {
		for _, override := range input.MemberConfigs {
			for index := range configured {
				member := &configured[index]
				if member.WakerID != override.WakerID {
					continue
				}
				configuration := execution.ExecutionConfiguration{RuntimeID: member.RuntimeID, AccountID: member.AccountID,
					Model: member.Model, Reasoning: member.Reasoning, Fast: member.Fast, PermissionLevel: member.PermissionLevel}
				if member.WakerID == channel.LeadWakerID {
					configuration.RequiredConnectorTransport = constants.RuntimeConnectorTransportStreamableHTTP
				}
				resolved, resolveErr := s.executions.ResolveConfiguration(ctx, configuration)
				if resolveErr != nil {
					return ThreadResult{}, resolveErr
				}
				member.RuntimeID, member.AccountID = resolved.RuntimeID, resolved.AccountID
				member.Model, member.Reasoning, member.Fast = resolved.Model, resolved.Reasoning, resolved.Fast
				member.PermissionLevel = resolved.PermissionLevel
				break
			}
		}
	}

	if input.MentionWakerID != "" && !configuredMember(configured, input.MentionWakerID) {
		return ThreadResult{}, status_error.ChannelMemberNotFound.WithParams(
			map[string]interface{}{"WakerID": input.MentionWakerID},
		)
	}
	directedToMember := input.MentionWakerID != "" && input.MentionWakerID != channel.LeadWakerID
	leadInput := input.Input
	var messagePlugins []plugins.Snapshot
	if directedToMember && len(input.Input.PluginIDs) != 0 {
		messagePlugins, err = s.directedPluginReferences(ctx, input.MentionWakerID, input.Input.PluginIDs)
		if err != nil {
			return ThreadResult{}, err
		}
		leadInput.PluginIDs = nil
	}
	threadID := util.New()
	provisioningID := util.New()
	now := time.Now().UnixMilli()
	preparedMembers := make([]execution.PrepareChatMember, 0, len(configured))
	members := make([]models.ChannelThreadMember, 0, len(configured))
	plannedChatIDs := make([]string, 0, len(configured))
	var leadConfig models.ChannelMember
	leadChatID := ""
	for _, member := range configured {
		waker, getErr := s.wakers.Get(ctx, member.WakerID)
		if getErr != nil {
			return ThreadResult{}, getErr
		}
		isLead := member.WakerID == channel.LeadWakerID
		chatID := util.New()
		if isLead {
			chatID = initialLeadChatID(channel.ID, requestIdentity)
		}
		plannedChatIDs = append(plannedChatIDs, chatID)
		preparedMembers = append(preparedMembers, execution.PrepareChatMember{
			ChatID:          chatID,
			WakerID:         member.WakerID,
			RuntimeID:       member.RuntimeID,
			AccountID:       member.AccountID,
			Model:           member.Model,
			Reasoning:       member.Reasoning,
			Fast:            member.Fast,
			PermissionLevel: member.PermissionLevel,
		})
		members = append(members, models.ChannelThreadMember{
			ThreadID:        threadID,
			WakerID:         member.WakerID,
			DisplayName:     rosterText(waker.Name, constants.ChannelRosterMaximumNameRunes, "Not provided"),
			Avatar:          waker.Avatar,
			RoleName:        rosterText(waker.RoleName, constants.ChannelRosterMaximumRoleNameRunes, ""),
			Bio:             rosterText(waker.Bio, constants.ChannelRosterMaximumBioRunes, ""),
			Expertise:       snapshotMemberExpertise(waker.Expertise),
			ChatID:          chatID,
			IsLead:          isLead,
			RuntimeID:       member.RuntimeID,
			AccountID:       member.AccountID,
			Model:           member.Model,
			Reasoning:       member.Reasoning,
			Fast:            member.Fast,
			PermissionLevel: member.PermissionLevel,
			Position:        member.Position,
			CreatedAt:       now,
			UpdatedAt:       now,
		})
		if isLead {
			leadConfig = member
			leadChatID = chatID
		}
	}
	if leadChatID == "" || leadConfig.WakerID == "" {
		return ThreadResult{}, status_error.ChannelLeadInvalid
	}
	title := input.Input.Content
	if strings.TrimSpace(title) == "" && input.Input.Workflow != nil {
		title = input.Input.Workflow.Name
	}
	thread := models.ChannelThread{AutomaticRunID: input.AutomaticRunID,
		ID:                           threadID,
		ChannelID:                    channel.ID,
		ClientRequestID:              &requestIdentity.ID,
		ClientRequestHash:            requestIdentity.Hash,
		Title:                        shortSubject(title),
		LeadWakerID:                  channel.LeadWakerID,
		LeadChatID:                   leadChatID,
		ProjectID:                    input.ProjectID,
		AdditionalWorkingDirectories: []string{},
		MaxParallel:                  constants.ChannelDefaultMaximumParallelTasks,
		MaxTasks:                     constants.ChannelDefaultMaximumTasks,
		MaxFailureAttempts:           constants.ChannelDefaultMaximumFailureAttempts,
		ProvisioningID:               provisioningID,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}
	var initialTask *models.ChannelTask
	if directedToMember {
		memberInput := input.Input
		// 首发用户输入已经由负责人 Chat 作为公开事实保存；任务副本仅负责保留
		// 完整附件和能力引用，不在聚合时间线生成第二个用户气泡。
		memberInput.Actor = string(constants.ChannelMessageActorLead)
		initialTask = &models.ChannelTask{
			ID:               util.New(),
			ThreadID:         threadID,
			AssigneeWakerID:  input.MentionWakerID,
			Subject:          thread.Title,
			Description:      input.Input.Content,
			DirectInput:      &memberInput,
			IMReplyLinkID:    memberInput.IMReplyLinkID,
			DirectDelivery:   true,
			DelegationSentAt: now,
			Status:           constants.ChannelTaskStatusPending,
			ChangedPaths:     []string{},
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		thread.TaskCount = 1
	}
	// 先持久化成员和 Chat 标识计划，启动恢复据此定位首轮回执或回收未接纳的工作区。
	releaseReferences, err := chatstorage.AcquireReferences(s.managedFilesDirectory, plannedChatIDs...)
	if err != nil {
		return ThreadResult{}, err
	}
	err = s.transaction(ctx, func(transactionContext context.Context) error {
		if err := s.threads.Create(transactionContext, thread, members); err != nil {
			return err
		}
		if initialTask != nil {
			return s.tasks.Create(transactionContext, *initialTask, nil)
		}
		return nil
	})
	releaseReferences()
	if err != nil {
		existing, exists, lookupErr := s.threads.GetByClientRequestID(
			ctx,
			input.ChannelID,
			requestIdentity.ID,
		)
		if lookupErr != nil {
			return ThreadResult{}, lookupErr
		}
		if exists {
			result, replayed, replayErr := s.replayCreateThread(ctx, existing, requestIdentity)
			if replayErr != nil || replayed {
				return result, replayErr
			}
		}
		return ThreadResult{}, err
	}
	prepared, err := s.chatGroups.PrepareChatGroup(ctx, execution.PrepareChatGroupIntent{
		ThreadID:       threadID,
		ProvisioningID: provisioningID,
		Scope: execution.CreateChatSpec{
			WakerID:          channel.LeadWakerID,
			ProjectID:        input.ProjectID,
			WorkingDirectory: input.WorkingDirectory,
		},
		Title:   title,
		Members: preparedMembers,
	})
	if err != nil {
		s.rollbackInitializingThread(ctx, thread, plannedChatIDs)
		return ThreadResult{}, err
	}
	if len(prepared.Chats) != len(preparedMembers) {
		s.rollbackInitializingThread(ctx, thread, plannedChatIDs)
		return ThreadResult{}, status_error.ChatLogError
	}
	var leadChat models.Chat
	for index, chat := range prepared.Chats {
		if chat.ID != preparedMembers[index].ChatID || chat.WakerID != preparedMembers[index].WakerID {
			s.rollbackInitializingThread(ctx, thread, plannedChatIDs)
			return ThreadResult{}, status_error.ChatLogError
		}
		if chat.ID == leadChatID {
			leadChat = chat
		}
	}
	if leadChat.ID == "" {
		s.rollbackInitializingThread(ctx, thread, plannedChatIDs)
		return ThreadResult{}, status_error.ChannelLeadInvalid
	}
	thread.Title = leadChat.Title
	thread.ProjectID = leadChat.ProjectID
	thread.ProjectName = leadChat.ProjectName
	thread.WorkspaceDirectory = leadChat.WorkspaceDirectory
	thread.AdditionalWorkingDirectories = append([]string(nil), leadChat.AdditionalWorkingDirectories...)
	thread.UpdatedAt = time.Now().UnixMilli()
	if initialTask != nil {
		initialTask.Subject = leadChat.Title
		initialTask.UpdatedAt = thread.UpdatedAt
	}
	if err := s.transaction(ctx, func(transactionContext context.Context) error {
		if err := s.threads.UpdateProvisioned(transactionContext, thread); err != nil {
			return err
		}
		if initialTask == nil {
			return nil
		}
		return s.tasks.UpdateFields(transactionContext, initialTask.ID, map[string]interface{}{
			"subject":    initialTask.Subject,
			"updated_at": initialTask.UpdatedAt,
		})
	}); err != nil {
		s.rollbackInitializingThread(ctx, thread, plannedChatIDs)
		return ThreadResult{}, err
	}
	leadExecutionID := util.New()
	if input.ExecutionID != "" {
		leadExecutionID = strings.TrimSpace(input.ExecutionID)
	}
	capability, capabilityToken, err := s.mintLeadCapability(ctx, thread, leadExecutionID, true)
	if err != nil {
		s.rollbackInitializingThread(ctx, thread, plannedChatIDs)
		return ThreadResult{}, err
	}
	teamInstruction, err := s.teamInstruction(ctx, thread, thread.LeadWakerID)
	if err != nil {
		s.capabilities.Revoke(capabilityToken)
		s.rollbackInitializingThread(ctx, thread, plannedChatIDs)
		return ThreadResult{}, err
	}
	workOptions := leaderWorkOptions(
		leadConfig.RuntimeID,
		leadConfig.Model,
		leadConfig.Reasoning,
		leadConfig.Fast,
		leadConfig.PermissionLevel,
		capability,
		teamInstruction,
		input.MentionWakerID == thread.LeadWakerID || input.Input.Workflow != nil,
	)
	workOptions.AccountID = leadConfig.AccountID
	leadInput.ContextInstructions = append(leadInput.ContextInstructions, leaderTurnInstruction(input.MentionWakerID == thread.LeadWakerID || leadInput.Workflow != nil))

	accepted, err := s.executions.Start(ctx, execution.WorkIntent{
		AutomaticRunID: thread.AutomaticRunID, ExecutionID: leadExecutionID,
		Chat:              execution.ChatTarget{ChatID: leadChat.ID},
		Input:             leadInput,
		MessagePlugins:    messagePlugins,
		Options:           workOptions,
		Admission:         constants.ExecutionAdmissionCommitBeforeDispatch,
		AdmissionBoundary: input.AdmissionBoundary,
	})
	completionContext := context.WithoutCancel(ctx)
	if err != nil {
		s.capabilities.Revoke(capabilityToken)
		receipt, readErr := s.readInitialThreadExecution(completionContext, thread)
		if readErr != nil || receipt != nil {
			// 日志同步错误可能留下完整接纳记录，保留初始化锚点供恢复核对。
			return ThreadResult{}, status_error.CreateRequestInProgress
		}
		s.rollbackInitializingThread(completionContext, thread, plannedChatIDs)
		return ThreadResult{}, err
	}
	initialTasks := []models.ChannelTask{}
	if initialTask != nil {
		initialTasks = append(initialTasks, *initialTask)
	}
	if err := s.publishInitialThread(completionContext, &thread, accepted.Execution.ID, initialTasks); err != nil {
		logx.Warnf("publish accepted channel thread pending: thread_id=%s execution_id=%s error=%v", thread.ID, accepted.Execution.ID, err)
		return ThreadResult{}, status_error.CreateRequestInProgress
	}
	release()
	s.watchLeadExecution(thread, accepted.Execution.ID, nil, nil, capabilityToken)
	result := ThreadResult{
		Thread:        thread,
		Members:       members,
		LeadExecution: &accepted.Execution,
		Tasks:         initialTasks,
	}
	if initialTask != nil {
		if _, scheduleErr := s.schedule(inputContext, thread.ID); scheduleErr != nil {
			if _, eventErr := s.appendEvent(ctx, channel.ID, thread.ID, constants.ChannelEventTaskDispatchDeferred, map[string]interface{}{
				"taskId": initialTask.ID,
			}); eventErr != nil {
				logx.Errorf(
					"append deferred channel dispatch event failed: thread_id=%s task_id=%s execution_id=%s error=%v",
					thread.ID,
					initialTask.ID,
					accepted.Execution.ID,
					eventErr,
				)
			}
		} else {
			current, getErr := s.tasks.Get(ctx, initialTask.ID)
			if getErr != nil {
				logx.Errorf(
					"reload initial channel task failed: thread_id=%s task_id=%s error=%v",
					thread.ID,
					initialTask.ID,
					getErr,
				)
			} else {
				result.Tasks[0] = current
			}
		}
	}
	return result, nil
}

// initialLeadChatID 允许首发停止在成员 Chat 持久计划生成之前定位负责人占位。
func initialLeadChatID(channelID string, request requestidempotency.Identity) string {
	return request.ResourceID("channel_lead_chat", channelID)
}

// readInitialThreadExecution 从预分配的负责人 Chat 核验原团队首轮接纳。
func (s *service) readInitialThreadExecution(ctx context.Context, thread models.ChannelThread) (*models.Execution, error) {
	state, err := s.chats.ReadInitialExecution(ctx, thread.LeadChatID)
	if hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if state.ThreadWorkspaceID != thread.ID || state.Chat.ID != thread.LeadChatID || state.Chat.WakerID != thread.LeadWakerID {
		return nil, status_error.ChatDirectoryOwnershipInvalid
	}
	receipt := state.InitialExecution
	if receipt != nil && (receipt.ChatID != thread.LeadChatID || receipt.AutomaticRunID != thread.AutomaticRunID) {
		return nil, status_error.ChatLogCorrupted
	}
	return receipt, nil
}

// publishInitialThread 在同一事务公开首发和预建任务，沿用已接纳执行的通知边界。
func (s *service) publishInitialThread(ctx context.Context, thread *models.ChannelThread, executionID string, tasks []models.ChannelTask) error {
	readyAt := time.Now().UnixMilli()
	if err := s.transaction(ctx, func(transactionContext context.Context) error {
		if err := s.threads.MarkReady(transactionContext, thread.ID, thread.ProvisioningID, executionID, readyAt); err != nil {
			return err
		}
		if _, err := s.appendEvent(transactionContext, thread.ChannelID, thread.ID, constants.ChannelEventThreadCreated, map[string]string{
			"threadId": thread.ID, "leadChatId": thread.LeadChatID,
		}); err != nil {
			return err
		}
		for _, task := range tasks {
			if _, err := s.appendEvent(transactionContext, thread.ChannelID, thread.ID, constants.ChannelEventTaskCreated, task); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	thread.ReadyAt, thread.UpdatedAt = &readyAt, readyAt
	return nil
}

// createThreadRequestIdentity 为规范化团队首发内容建立共享幂等身份。
func createThreadRequestIdentity(input CreateThreadInput) (requestidempotency.Identity, error) {
	return requestidempotency.New(input.ClientRequestID, []interface{}{
		input.ChannelID,
		input.ProjectID,
		input.WorkingDirectory,
		input.MentionWakerID,
		input.MemberConfigs,
		input.ConfigurationResolved,
		input.Input,
	})
}

// replayCreateThread 返回同键的既有首发。明确未接纳的计划回收后允许同一请求继续接纳。
func (s *service) replayCreateThread(
	ctx context.Context,
	thread models.ChannelThread,
	requestIdentity requestidempotency.Identity,
) (ThreadResult, bool, error) {
	storedRequestID := ""
	if thread.ClientRequestID != nil {
		storedRequestID = *thread.ClientRequestID
	}
	if err := requestIdentity.ValidateReplay(storedRequestID, thread.ClientRequestHash, true); err != nil {
		return ThreadResult{}, false, err
	}
	if thread.ReadyAt == nil {
		var err error
		thread, err = s.recoverInitializingThread(ctx, thread, true)
		if err != nil || thread.ID == "" {
			return ThreadResult{}, false, err
		}
	}
	members, err := s.threads.ListMembers(ctx, thread.ID)
	if err != nil {
		return ThreadResult{}, false, err
	}
	tasks, err := s.tasks.ListByThread(ctx, thread.ID)
	if err != nil {
		return ThreadResult{}, false, err
	}
	return ThreadResult{
		Thread:   thread,
		Members:  members,
		Replayed: true,
		Tasks:    tasks,
	}, true, nil
}

// ApplyMemberConfigOverrides 将完整成员覆盖应用到团队默认配置，并校验成员身份及重复项。
func ApplyMemberConfigOverrides(
	members []models.ChannelMember,
	overrides []MemberExecutionConfigInput,
) ([]models.ChannelMember, error) {
	configs, err := normalizeMemberExecutionConfigs(overrides)
	if err != nil {
		return nil, err
	}
	byWakerID := make(map[string]MemberExecutionConfigInput, len(configs))
	for _, config := range configs {
		byWakerID[config.WakerID] = config
	}
	result := append([]models.ChannelMember(nil), members...)
	for index := range result {
		config, exists := byWakerID[result[index].WakerID]
		if !exists {
			continue
		}
		result[index].RuntimeID = config.RuntimeID
		result[index].AccountID = config.AccountID
		result[index].Model = config.Model
		result[index].Reasoning = config.Reasoning
		result[index].Fast = config.Fast
		result[index].PermissionLevel = config.PermissionLevel
		delete(byWakerID, result[index].WakerID)
	}
	if len(byWakerID) > 0 {
		for wakerID := range byWakerID {
			return nil, status_error.ChannelMemberNotFound.WithParams(
				map[string]interface{}{"WakerID": wakerID},
			)
		}
	}
	return result, nil
}

func normalizeMemberExecutionConfigs(
	configs []MemberExecutionConfigInput,
) ([]MemberExecutionConfigInput, error) {
	if len(configs) > constants.ChannelMaximumMemberCount {
		return nil, status_error.ChannelMemberCountInvalid
	}
	normalized := append([]MemberExecutionConfigInput(nil), configs...)
	seen := make(map[string]struct{}, len(normalized))
	for index := range normalized {
		config := &normalized[index]
		config.WakerID = strings.TrimSpace(config.WakerID)
		config.RuntimeID = strings.TrimSpace(config.RuntimeID)
		config.AccountID = strings.TrimSpace(config.AccountID)
		config.Model = strings.TrimSpace(config.Model)
		config.Reasoning = strings.TrimSpace(config.Reasoning)
		config.PermissionLevel = constants.PermissionLevel(
			strings.TrimSpace(string(config.PermissionLevel)),
		)
		if config.WakerID == "" || config.RuntimeID == "" ||
			!constants.SelectablePermissionLevel(config.PermissionLevel) {
			return nil, status_error.BadRequest
		}
		if _, duplicated := seen[config.WakerID]; duplicated {
			return nil, status_error.ChannelMemberCountInvalid
		}
		seen[config.WakerID] = struct{}{}
	}
	return normalized, nil
}

// sendMessage 在既有团队对话中按显式提及或负责人默认路由输入。
func (s *service) sendMessage(
	ctx context.Context,
	input SendMessageInput,
) (SendMessageResult, error) {
	input.ThreadID = strings.TrimSpace(input.ThreadID)
	input.MentionWakerID = strings.TrimSpace(input.MentionWakerID)
	input.Input.ReplyToMessageID = strings.TrimSpace(input.Input.ReplyToMessageID)
	if input.Input.ReplyToMessageID != "" {
		// 主对话引用的是历史消息，仍由负责人协调当前用户输入。
		input.MentionWakerID = ""
	}
	if input.Input.AttachmentOnly && len(input.Input.Attachments) == 0 {
		return SendMessageResult{}, status_error.MessageAttachmentInvalid
	}
	input.Input.Actor = string(constants.ChannelMessageActorUser)
	if input.ThreadID == "" || (strings.TrimSpace(input.Input.Content) == "" && input.Input.Workflow == nil) ||
		(input.Input.CodeReferenceOnly && len(input.Input.CodeReferences) == 0) ||
		(input.Input.Workflow != nil && input.MentionWakerID != "") {
		return SendMessageResult{}, status_error.BadRequest
	}
	inputContext := ctx
	locked, releaseTeam, lockErr := s.acquireThreadOperation(ctx, input.ThreadID)
	if lockErr != nil {
		return SendMessageResult{}, lockErr
	}
	ctx = locked
	release := sync.OnceFunc(releaseTeam)
	defer release()
	if err := s.freezeBeforeUserInput(ctx, input.ThreadID); err != nil {
		return SendMessageResult{}, err
	}
	thread, err := s.threads.Get(ctx, input.ThreadID)
	if err != nil {
		return SendMessageResult{}, err
	}
	if _, err := s.channels.Get(ctx, thread.ChannelID); err != nil {
		return SendMessageResult{}, err
	}
	if input.MentionWakerID != "" {
		if _, err := s.threads.GetMember(ctx, thread.ID, input.MentionWakerID); err != nil {
			return SendMessageResult{}, err
		}
	}
	if input.MentionWakerID != "" && input.MentionWakerID != thread.LeadWakerID {
		var releaseAttachments func(bool)
		attachmentsAccepted := false
		if input.ClaimAttachments != nil {
			releaseAttachments, err = input.ClaimAttachments(&input.Input)
			if err != nil {
				return SendMessageResult{}, err
			}
			defer func() { releaseAttachments(attachmentsAccepted) }()
		}
		active, activeErr := s.activeTaskForAssignee(ctx, thread.ID, input.MentionWakerID)
		if activeErr != nil {
			return SendMessageResult{}, activeErr
		}
		if active != nil {
			if err := s.FollowUpTask(ctx, active.ID, input.Input); err != nil {
				return SendMessageResult{}, err
			}
			attachmentsAccepted = true
			return SendMessageResult{Task: active}, nil
		}
		release()
		created, err := s.CreateTask(inputContext, CreateTaskInput{
			ThreadID:        thread.ID,
			AssigneeWakerID: input.MentionWakerID,
			Subject:         shortSubject(input.Input.Content),
			Description:     input.Input.Content,
			DirectInput:     &input.Input,
			DirectDelivery:  true,
		})
		if err != nil {
			return SendMessageResult{}, err
		}
		attachmentsAccepted = true
		return SendMessageResult{Task: &created.Task}, nil
	}
	queued, err := s.enqueueLeadInput(ctx, thread, input)
	if err != nil {
		return SendMessageResult{}, err
	}
	release()
	ctx = inputContext
	var accepted *models.Execution
	if queued.Status == constants.ChannelInputStatusPending {
		accepted, err = s.dispatchLeadInputs(ctx, thread.ID, &queued)
		if accepted == nil {
			s.watchRecoveryDrain(thread.ID)
		}
	}
	if queued.Status == constants.ChannelInputStatusFailed && queued.Payload.IMReplyLinkID == "" {
		return SendMessageResult{}, leadInputFailureError(queued.ErrorCode)
	}
	if err != nil && queued.Status != constants.ChannelInputStatusFailed {
		return SendMessageResult{}, err
	}
	return SendMessageResult{
		Execution: accepted,
		QueuedInput: &QueuedLeadInput{
			ID:        queued.ID,
			Status:    queued.Status,
			CreatedAt: queued.CreatedAt,
		},
	}, nil
}

func configuredMember(members []models.ChannelMember, wakerID string) bool {
	for _, member := range members {
		if member.WakerID == wakerID {
			return true
		}
	}
	return false
}

func (s *service) rollbackInitializingThread(
	ctx context.Context,
	thread models.ChannelThread,
	chatIDs []string,
) {
	if err := s.deleteInitializingThread(ctx, thread, chatIDs); err != nil {
		logx.Errorf(
			"rollback initializing channel thread failed: provisioning_id=%s thread_id=%s error=%v",
			thread.ProvisioningID,
			thread.ID,
			err,
		)
	}
}

// deleteInitializingThread 先删除可恢复文件聚合，再删除仍未 ready 的数据库锚点。
func (s *service) deleteInitializingThread(
	ctx context.Context,
	thread models.ChannelThread,
	chatIDs []string,
) error {
	if err := s.chats.DeleteProvisionalTeam(
		ctx,
		thread.ID,
		thread.ProvisioningID,
		chatIDs,
	); err != nil {
		return err
	}
	return s.transaction(ctx, func(transactionContext context.Context) error {
		return s.threads.DeleteInitializing(transactionContext, thread.ID)
	})
}

func (s *service) appendEvent(
	ctx context.Context,
	channelID string,
	threadID string,
	eventType constants.ChannelEventType,
	payload interface{},
) (models.ChannelEvent, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		logx.Errorf(
			"marshal channel event payload failed: channel_id=%s thread_id=%s event_type=%s error=%v",
			channelID,
			threadID,
			eventType,
			err,
		)
		return models.ChannelEvent{}, status_error.InternalServerError
	}
	return s.appendPreparedEvent(ctx, models.ChannelEvent{
		ID:        util.New(),
		ChannelID: channelID,
		ThreadID:  threadID,
		Type:      eventType,
		Payload:   data,
		CreatedAt: time.Now().UnixMilli(),
	})
}

func shortSubject(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	runes := []rune(text)
	if len(runes) <= constants.ChannelAutomaticTaskSubjectRunes {
		return text
	}
	return string(runes[:constants.ChannelAutomaticTaskSubjectRunes]) + "..."
}

func isChatBusy(err error) bool {
	return errors.Is(err, status_error.ChatHasActiveExecution)
}
