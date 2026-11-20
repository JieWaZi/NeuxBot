package channel

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"

	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (s *service) agentTeamBindings(endpoint, leaseID string) []capability.Binding {
	scope, _ := s.capabilities.Resolve(endpoint)
	operations := []string{"team.members.list", "team.tasks.list", "team.tasks.get"}
	if scope.Role == constants.ChannelCapabilityRoleMember {
		operations = []string{"team.reports.submit"}
	} else if scope.AllowMutations {
		operations = append(operations, "team.tasks.create", "team.tasks.update", "team.tasks.follow-up", "team.tasks.retry", "team.tasks.cancel")
	}
	result := make([]capability.Binding, 0, len(operations))
	for _, id := range operations {
		binding := capability.Binding{Operation: id}
		op, _ := capability.Find(id)
		binding.Call = func(ctx context.Context, request capability.Request) (capability.Result, error) {
			authorized, err := s.authorizeTeamTool(ctx, endpoint, leaseID)
			if err != nil {
				return capability.Result{}, err
			}
			if id == "team.members.list" {
				members, err := s.currentThreadMemberProfiles(ctx, authorized.thread.ID)
				items := make([]map[string]any, 0, len(members))
				for _, member := range members {
					items = append(items, map[string]any{"wakerId": member.WakerID, "name": member.DisplayName, "role": member.RoleName, "bio": member.Bio, "isLead": member.IsLead})
				}
				return capability.Result{Data: map[string]any{"items": items}}, err
			}
			input := teamAgentInput(request)
			if op.Effect != constants.AgentCLIEffectRead {
				return capability.Execute(ctx, s.commands, authorized.commandOwner(), request, func(ctx context.Context) error {
					_, err := s.toolChannelTasks(ctx, authorized, input)
					return err
				})
			}
			output, err := s.toolChannelTasks(ctx, authorized, input)
			if err != nil {
				return capability.Result{}, err
			}
			if id == "team.tasks.list" {
				page, err := capability.Page(output.Tasks, request.Params)
				return capability.Result{Data: page}, err
			}
			return capability.Result{Data: output.Task}, nil
		}
		if op.Effect != constants.AgentCLIEffectRead {
			binding.Receipt = func(ctx context.Context, requestID string) (capability.Result, error) {
				authorized, err := s.authorizeTeamTool(ctx, endpoint, leaseID)
				if err != nil {
					return capability.Result{}, err
				}
				return capability.Lookup(ctx, s.commands, authorized.commandOwner(), id, requestID)
			}
		}
		result = append(result, binding)
	}
	return result
}

func teamAgentInput(request capability.Request) channeltool.TaskInput {
	params := map[string]any{}
	names := map[string]string{"taskId": "task_id", "assigneeWakerId": "assignee_waker_id", "dependencyTaskIds": "dependency_task_ids", "requiresHumanReview": "requires_human_review", "directDelivery": "direct_delivery", "readyForUser": "ready_for_user"}
	for name, value := range request.Params {
		if alternate, exists := names[name]; exists {
			name = alternate
		}
		params[name] = value
	}
	encoded, _ := json.Marshal(params)
	var input channeltool.TaskInput
	_ = json.Unmarshal(encoded, &input)
	action := strings.TrimPrefix(request.Operation, "team.tasks.")
	if action == "follow-up" {
		action = "follow_up"
	}
	if request.Operation == "team.reports.submit" {
		action = "report"
	}
	if action == "list" && input.Query != "" {
		action = "search"
	}
	input.Action = constants.ChannelTaskToolAction(action)
	return input
}

// watchTaskCancellation 推进已接纳的停止意图，结束与重启均遵循现有受管监听生命周期。
func (s *service) watchTaskCancellation(attempt models.ChannelTaskAttempt) {
	key := "cancel:" + attempt.ID
	ctx, registered := s.registerWatcher(key)
	if !registered {
		return
	}
	go func() {
		defer s.unregisterWatcher(key)
		policy := s.effectiveWatcherPolicy()
		delay := policy.initialDelay
		for index := 0; index < policy.maximumFailures; index++ {
			task, err := s.tasks.Get(ctx, attempt.TaskID)
			if err == nil && (task.CurrentFence != attempt.Fence || task.Status != constants.ChannelTaskStatusInProgress || !taskCancellationRequested(task, attempt.Fence)) {
				return
			}
			if err == nil {
				// 原执行占位可能先于 Chat 投影，精确取消由 Execution 匹配。
				err = s.executions.CancelExecutionAndWait(ctx, attempt.ChatID, attempt.ExecutionID)
			}
			if err == nil || ctx.Err() != nil {
				return
			}
			logx.Errorf("cancel accepted team task failed: task_id=%s attempt_id=%s error=%v", attempt.TaskID, attempt.ID, err)
			if !waitAttemptWatcherRetry(ctx, delay) {
				return
			}
			delay = nextAttemptWatcherRetryDelay(delay, policy.maximumDelay)
		}
	}()
}
