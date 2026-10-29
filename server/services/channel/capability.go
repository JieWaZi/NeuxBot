package channel

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// mintLeadCapability 为一次负责人执行创建任务工具连接器和撤销令牌。
func (s *service) mintLeadCapability(
	_ context.Context,
	thread models.ChannelThread,
	executionID string,
	allowMutations bool,
) (execution.ToolCapability, string, error) {
	leaseID, err := s.capabilities.Mint(
		thread.ID,
		thread.LeadWakerID,
		executionID,
		allowMutations,
	)
	if err != nil {
		return execution.ToolCapability{}, "", err
	}
	return execution.ToolCapability{ID: "team.lead", Name: "团队协作", Bindings: s.agentTeamBindings(thread.ID, leaseID)}, leaseID, nil
}

// mintMemberCapability 为一次成员任务尝试创建仅可提交工作报告的执行能力。
func (s *service) mintMemberCapability(
	thread models.ChannelThread,
	attempt models.ChannelTaskAttempt,
) (execution.ToolCapability, string, error) {
	leaseID, err := s.capabilities.MintMember(
		thread.ID,
		attempt.AssigneeWakerID,
		attempt.ExecutionID,
		attempt.TaskID,
		attempt.ID,
	)
	if err != nil {
		return execution.ToolCapability{}, "", err
	}
	return execution.ToolCapability{ID: "team.member", Name: "成员报告", Bindings: s.agentTeamBindings(channeltool.MemberEndpointKey(thread.ID, attempt.AssigneeWakerID), leaseID)}, leaseID, nil
}

// leaderWorkOptions 统一冻结负责人回合的用户权限、Runtime 选择与任务工具能力。
func leaderWorkOptions(
	runtimeID string,
	model string,
	reasoning string,
	permissionLevel constants.PermissionLevel,
	capability execution.ToolCapability,
	teamInstruction runtimecontract.Instruction,
	directExecution bool,
) execution.WorkOptions {
	return execution.WorkOptions{
		RuntimeID:        runtimeID,
		Model:            model,
		Reasoning:        reasoning,
		PermissionLevel:  permissionLevel,
		SessionPolicy:    constants.RuntimeSessionPolicyContinue,
		ToolCapabilities: []execution.ToolCapability{capability},
		SupplementalInstructions: []runtimecontract.Instruction{
			leaderTurnInstruction(teamInstruction, directExecution),
		},
	}
}

// toolChannelTasks 执行当前团队角色在短期能力范围内允许的协作操作。
func (s *service) toolChannelTasks(
	ctx context.Context,
	endpointKey string,
	input channeltool.TaskInput,
) (channeltool.TaskOutput, error) {
	capability, exists := s.capabilities.Resolve(endpointKey)
	if !exists {
		return channelToolError(status_error.AgentContextUnauthorized, "The channel task capability is invalid or expired.")
	}
	thread, err := s.threads.Get(ctx, capability.ThreadID)
	if err != nil {
		logChannelToolFailure(capability, input.Action, input.TaskID, err)
		return channelToolError(err, "The current channel thread is unavailable.")
	}
	member, err := s.threads.GetMember(ctx, thread.ID, capability.CallerWakerID)
	if err != nil {
		logChannelToolFailure(capability, input.Action, input.TaskID, err)
		return channelToolError(err, "The current caller is not a member of this channel thread.")
	}
	if capability.Role == constants.ChannelCapabilityRoleLead &&
		thread.LeadWakerID != capability.CallerWakerID {
		return channelToolError(status_error.AgentOperationForbidden, "The current caller is not the lead of this channel thread.")
	}
	if capability.Role == constants.ChannelCapabilityRoleMember &&
		thread.LeadWakerID == capability.CallerWakerID {
		return channelToolError(status_error.AgentOperationForbidden, "The current capability is not a member report capability.")
	}
	summary, err := s.chats.GetChatSummary(ctx, member.ChatID)
	if err != nil {
		logChannelToolFailure(capability, input.Action, input.TaskID, err)
		return channelToolError(err, "The channel collaboration capability does not belong to the active execution.")
	}
	if summary.Chat.ActiveExecutionID != capability.ExecutionID {
		return channelToolError(status_error.AgentContextUnauthorized, "The channel collaboration capability does not belong to the active execution.")
	}
	action := constants.ChannelTaskToolAction(strings.TrimSpace(string(input.Action)))
	if !channeltool.Allows(action, capability.Role, capability.AllowMutations) {
		if capability.Role == constants.ChannelCapabilityRoleMember {
			return channelToolError(status_error.AgentOperationForbidden, "This member execution may only submit its own work report.")
		}
		return channelToolError(status_error.AgentOperationForbidden, "This is a result-notification turn. Read the task board and report status, but do not create or modify tasks.")
	}
	switch action {
	case constants.ChannelTaskToolActionList, constants.ChannelTaskToolActionSearch:
		tasks, listErr := s.tasks.ListByThread(ctx, thread.ID)
		if listErr != nil {
			logChannelToolFailure(capability, action, input.TaskID, listErr)
			return channelToolError(listErr, "The task board is temporarily unavailable.")
		}
		s.capabilities.MarkListed(endpointKey)
		if action == constants.ChannelTaskToolActionSearch {
			query := strings.ToLower(strings.TrimSpace(input.Query))
			if query == "" {
				return channelToolError(status_error.AgentInputInvalid, "query is required for search.")
			}
			filtered := make([]models.ChannelTask, 0, len(tasks))
			for _, task := range tasks {
				haystack := strings.ToLower(task.Subject + "\n" + task.Description)
				if strings.Contains(haystack, query) {
					filtered = append(filtered, task)
				}
			}
			tasks = filtered
		}
		sort.SliceStable(tasks, func(left, right int) bool {
			if tasks[left].Priority != tasks[right].Priority {
				return tasks[left].Priority > tasks[right].Priority
			}
			return tasks[left].CreatedAt < tasks[right].CreatedAt
		})
		for index := range tasks {
			tasks[index] = taskBoardProjection(tasks[index])
		}
		return channeltool.TaskOutput{Tasks: tasks}, nil
	case constants.ChannelTaskToolActionGet:
		task, getErr := s.tasks.Get(ctx, strings.TrimSpace(input.TaskID))
		if getErr != nil {
			logChannelToolFailure(capability, action, input.TaskID, getErr)
			return channelToolError(getErr, "The task was not found in this channel thread.")
		}
		if task.ThreadID != thread.ID {
			return channelToolError(status_error.ChannelTaskNotFound, "The task was not found in this channel thread.")
		}
		return channeltool.TaskOutput{Task: &task}, nil
	case constants.ChannelTaskToolActionCreate:
		if !capability.Listed {
			return channelToolError(status_error.AgentInputInvalid, "Call allycli team tasks list before create to avoid duplicate work.")
		}
		assignee := strings.TrimSpace(input.AssigneeWakerID)
		if assignee == thread.LeadWakerID {
			return channelToolError(status_error.AgentInputInvalid, leadTaskAssigneeError)
		}
		created, createErr := s.CreateTask(ctx, CreateTaskInput{
			ThreadID:            thread.ID,
			SenderWakerID:       capability.CallerWakerID,
			AssigneeWakerID:     assignee,
			Subject:             input.Subject,
			Description:         input.Description,
			Priority:            optionalInt(input.Priority),
			DependencyTaskIDs:   input.DependencyTaskIDs,
			RequiresHumanReview: input.RequiresHumanReview,
			DirectDelivery:      input.DirectDelivery,
			DeferDispatch:       true,
		})
		if createErr != nil {
			logChannelToolFailure(capability, action, created.Task.ID, createErr)
			return channelToolError(createErr, channelToolErrorText(createErr))
		}
		return channeltool.TaskOutput{Task: &created.Task}, nil
	case constants.ChannelTaskToolActionUpdate:
		if !capability.Listed {
			return channelToolError(status_error.AgentInputInvalid, "Call allycli team tasks list before update.")
		}
		if strings.TrimSpace(input.AssigneeWakerID) == thread.LeadWakerID {
			return channelToolError(status_error.AgentInputInvalid, leadTaskAssigneeError)
		}
		updated, updateErr := s.updatePendingTask(ctx, UpdateTaskInput{
			ThreadID:        thread.ID,
			TaskID:          input.TaskID,
			AssigneeWakerID: strings.TrimSpace(input.AssigneeWakerID),
			Subject:         input.Subject,
			Description:     input.Description,
			Priority:        input.Priority,
		})
		if updateErr != nil {
			logChannelToolFailure(capability, action, input.TaskID, updateErr)
			return channelToolError(updateErr, channelToolErrorText(updateErr))
		}
		return channeltool.TaskOutput{Task: &updated}, nil
	case constants.ChannelTaskToolActionFollowUp:
		task, getErr := s.tasks.Get(ctx, strings.TrimSpace(input.TaskID))
		if getErr != nil {
			logChannelToolFailure(capability, action, input.TaskID, getErr)
			return channelToolError(getErr, "follow_up only works on an in-progress task in this channel thread.")
		}
		if task.ThreadID != thread.ID || task.Status != constants.ChannelTaskStatusInProgress {
			return channelToolError(status_error.ChannelTaskStateConflict, "follow_up only works on an in-progress task in this channel thread.")
		}
		if followErr := s.FollowUpTask(ctx, task.ID, execution.WorkInput{
			Actor:   string(constants.ChannelMessageActorLead),
			Content: input.Text,
		}); followErr != nil {
			logChannelToolFailure(capability, action, task.ID, followErr)
			return channelToolError(followErr, channelToolErrorText(followErr))
		}
		updated, getErr := s.tasks.Get(ctx, task.ID)
		if getErr != nil {
			logChannelToolFailure(capability, action, task.ID, getErr)
			return channelToolError(getErr, "The updated task could not be reloaded. Refresh the task board before continuing.")
		}
		return channeltool.TaskOutput{Task: &updated}, nil
	case constants.ChannelTaskToolActionRetry:
		if !capability.Listed {
			return channelToolError(status_error.AgentInputInvalid, "Call allycli team tasks list before retry.")
		}
		if strings.TrimSpace(input.AssigneeWakerID) == thread.LeadWakerID {
			return channelToolError(status_error.AgentInputInvalid, leadTaskAssigneeError)
		}
		task, getErr := s.tasks.Get(ctx, strings.TrimSpace(input.TaskID))
		if getErr != nil {
			logChannelToolFailure(capability, action, input.TaskID, getErr)
			return channelToolError(getErr, "The task was not found in this channel thread.")
		}
		if task.ThreadID != thread.ID {
			return channelToolError(status_error.ChannelTaskNotFound, "The task was not found in this channel thread.")
		}
		if retryErr := s.retryTask(ctx, task.ID, strings.TrimSpace(input.AssigneeWakerID)); retryErr != nil {
			logChannelToolFailure(capability, action, task.ID, retryErr)
			return channelToolError(retryErr, channelToolErrorText(retryErr))
		}
		updated, getErr := s.tasks.Get(ctx, task.ID)
		if getErr != nil {
			logChannelToolFailure(capability, action, task.ID, getErr)
			return channelToolError(getErr, "The retried task could not be reloaded. Refresh the task board before continuing.")
		}
		return channeltool.TaskOutput{Task: &updated}, nil
	case constants.ChannelTaskToolActionCancel:
		task, getErr := s.tasks.Get(ctx, strings.TrimSpace(input.TaskID))
		if getErr != nil {
			logChannelToolFailure(capability, action, input.TaskID, getErr)
			return channelToolError(getErr, "The task was not found in this channel thread.")
		}
		if task.ThreadID != thread.ID {
			return channelToolError(status_error.ChannelTaskNotFound, "The task was not found in this channel thread.")
		}
		if task.Status == constants.ChannelTaskStatusInReview {
			return channelToolError(status_error.AgentOperationForbidden, "Tasks awaiting review can only be approved, rejected, or cancelled by the user.")
		}
		if cancelErr := s.StopTask(ctx, task.ID); cancelErr != nil {
			logChannelToolFailure(capability, action, task.ID, cancelErr)
			return channelToolError(cancelErr, channelToolErrorText(cancelErr))
		}
		updated, getErr := s.tasks.Get(ctx, task.ID)
		if getErr != nil {
			logChannelToolFailure(capability, action, task.ID, getErr)
			return channelToolError(getErr, "The cancelled task could not be reloaded. Refresh the task board before continuing.")
		}
		return channeltool.TaskOutput{Task: &updated}, nil
	case constants.ChannelTaskToolActionReport:
		return s.submitMemberReport(ctx, thread, member, capability, input.Text, input.ReadyForUser)
	default:
		return channelToolError(status_error.AgentInputInvalid, "Unknown action. Use list, search, get, create, update, follow_up, retry, cancel, or report.")
	}
}

// logChannelToolFailure 只记录服务端诊断上下文，不把数据库或 Execution 错误泄露给 CLI 调用方。
func logChannelToolFailure(
	capability channeltool.Scope,
	action constants.ChannelTaskToolAction,
	taskID string,
	err error,
) {
	if err == nil {
		return
	}
	logx.Errorf(
		"channel task tool action failed: thread_id=%s execution_id=%s waker_id=%s task_id=%s action=%s error=%v",
		capability.ThreadID,
		capability.ExecutionID,
		capability.CallerWakerID,
		strings.TrimSpace(taskID),
		strings.TrimSpace(string(action)),
		err,
	)
}

// taskBoardProjection 从 list/search 结果移除完整执行正文，只保留结构化任务板字段。
func taskBoardProjection(task models.ChannelTask) models.ChannelTask {
	task.DirectInput = nil
	task.Result = ""
	task.ErrorMessage = ""
	task.ChangedPaths = []string{}
	return task
}

// optionalInt 把可选任务参数转换为创建任务使用的默认优先级。
func optionalInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// channelToolError 创建由能力适配统一映射为安全命令错误的中立结果。
func channelToolError(cause error, message string) (channeltool.TaskOutput, error) {
	return channeltool.TaskOutput{Message: message}, agentcapability.Reject(cause, message)
}

const leadTaskAssigneeError = "The team lead cannot be a task assignee. Complete final summaries in the coordination turn; explicit user requests to the lead run directly in the lead Chat without a task."

// channelToolErrorText 把内部稳定错误收敛为负责人可修正的工具提示。
func channelToolErrorText(err error) string {
	var commonErr statuserror.CommonError
	if errors.As(err, &commonErr) {
		switch commonErr.Code() {
		case status_error.ChannelTaskLimitReached.Code():
			return "The channel thread has reached its task limit. Reuse or finish existing tasks."
		case status_error.ChannelMemberNotFound.Code():
			return "The assignee is not a member of this channel thread. Use the Waker ID in backticks from the Members list, not display names or aliases like you."
		case status_error.BadRequest.Code():
			return "The task input is invalid. Provide one concise subject, a complete description, and valid dependencies."
		}
	}
	return "The task could not be created. Refresh the task board and retry if appropriate."
}
