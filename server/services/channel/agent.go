package channel

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (s *service) agentTeamBindings(endpoint, leaseID string) []agentcapability.Binding {
	scope, _ := s.capabilities.Resolve(endpoint)
	operations := []string{"team.members.list", "team.tasks.list", "team.tasks.get"}
	if scope.Role == constants.ChannelCapabilityRoleMember {
		operations = []string{"team.reports.submit"}
	} else if scope.AllowMutations {
		operations = append(operations, "team.tasks.create", "team.tasks.update", "team.tasks.follow-up", "team.tasks.retry", "team.tasks.cancel")
	}
	validate := func(ctx context.Context) (string, error) {
		current, exists := s.capabilities.Resolve(endpoint)
		if !exists || current.LeaseID != leaseID {
			return "", status_error.AgentContextUnauthorized
		}
		thread, err := s.threads.Get(ctx, current.ThreadID)
		if err != nil {
			return "", err
		}
		member, err := s.threads.GetMember(ctx, thread.ID, current.CallerWakerID)
		if err != nil {
			return "", err
		}
		if (current.Role == constants.ChannelCapabilityRoleLead) != (thread.LeadWakerID == current.CallerWakerID) {
			return "", status_error.AgentOperationForbidden
		}
		summary, err := s.chats.GetChatSummary(ctx, member.ChatID)
		if err != nil {
			return "", err
		}
		if summary.Chat.ActiveExecutionID != current.ExecutionID {
			return "", status_error.AgentContextUnauthorized
		}
		owner := "team:" + thread.ID + ":" + current.CallerWakerID
		if current.Role == constants.ChannelCapabilityRoleMember {
			owner += ":" + current.AttemptID
		}
		return owner, nil
	}
	result := make([]agentcapability.Binding, 0, len(operations))
	for _, id := range operations {
		binding := agentcapability.Binding{Operation: id}
		binding.Call = func(ctx context.Context, request agentcapability.Request) (agentcapability.Result, error) {
			owner, err := validate(ctx)
			if err != nil {
				return agentcapability.Result{}, err
			}
			if id == "team.members.list" {
				members, err := s.threads.ListMembers(ctx, scope.ThreadID)
				items := make([]map[string]any, 0, len(members))
				for _, member := range members {
					items = append(items, map[string]any{"wakerId": member.WakerID, "name": member.DisplayName, "role": member.RoleName, "bio": member.Bio, "expertise": member.Expertise, "isLead": member.IsLead})
				}
				return agentcapability.Result{Data: map[string]any{"items": items}}, err
			}
			input := teamAgentInput(request)
			call := func(ctx context.Context) (channeltool.TaskOutput, error) {
				output, err := s.toolChannelTasks(ctx, endpoint, input)
				return output, err
			}
			op, _ := contract.Find(id)
			if op.Effect != constants.AgentCLIEffectRead {
				return agentcommand.Execute(ctx, s.commands, owner, request, func(ctx context.Context) error { _, err := call(ctx); return err })
			}
			output, err := call(ctx)
			if err != nil {
				return agentcapability.Result{}, err
			}
			if id == "team.tasks.list" {
				page, err := agentcapability.Page(output.Tasks, request.Params)
				return agentcapability.Result{Data: page}, err
			}
			return agentcapability.Result{Data: output.Task}, nil
		}
		op, _ := contract.Find(id)
		if op.Effect != constants.AgentCLIEffectRead {
			binding.Receipt = func(ctx context.Context, requestID string) (agentcapability.Result, error) {
				owner, err := validate(ctx)
				if err != nil {
					return agentcapability.Result{}, err
				}
				return agentcommand.Lookup(ctx, s.commands, owner, id, requestID)
			}
		}
		result = append(result, binding)
	}
	return result
}

func teamAgentInput(request agentcapability.Request) channeltool.TaskInput {
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
			if err == nil && (task.CurrentFence != attempt.Fence || task.Status != constants.ChannelTaskStatusInProgress || task.CancelRequestedAt == nil || task.CancelRequestedFence != attempt.Fence) {
				return
			}
			if err == nil {
				summary, readErr := s.chats.GetChatSummary(ctx, attempt.ChatID)
				if readErr == nil && summary.Chat.ActiveExecutionID != attempt.ExecutionID {
					return
				}
				if readErr != nil {
					err = readErr
				} else {
					err = s.executions.CancelAndWait(ctx, attempt.ChatID)
				}
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
