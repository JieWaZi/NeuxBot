package workflow

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
)

// RetryConflictInstances 将原运行和实际 joined 分支中的未确认效果关联到画布节点。
func RetryConflictInstances(state controllers.WorkflowRunState, blockedBranches []string) map[string]bool {
	unknown := make(map[string]bool)
	for _, instance := range state.Instances {
		if instance.Status == constants.WorkflowStepStatusUnknown {
			unknown[instance.ID] = true
		}
	}
	effectInstances := make(map[string]string, len(state.Effects))
	for _, effect := range state.Effects {
		effectInstances[effect.ID] = effect.InstanceID
		// 等待子流程的效果也可能保存子流程失败证据，涵盖已不存在关联行的历史。
		propagated := false
		if effect.ChildRunID != "" && effect.Failure != nil {
			failure, _ := effect.Failure.Data.(map[string]any)
			propagated = failure["code"] == constants.WorkflowFailureEffectOutcomeUnknown
		}
		if effect.Status == constants.WorkflowStepStatusUnknown || propagated || slices.Contains(blockedBranches, effect.ChildRunID) {
			unknown[effect.InstanceID] = true
		}
	}
	for _, call := range state.ActionCalls {
		if call.Status == constants.WorkflowStepStatusUnknown {
			unknown[effectInstances[call.EffectID]] = true
		}
	}
	if len(blockedBranches) != 0 && len(unknown) == 0 {
		// 子流程关系仍能阻止重试，即使根运行缺少关联画布节点。
		unknown[""] = true
	}
	return unknown
}

func (s *Service) validateReexecution(ctx context.Context, command StartRunCommand, revisionID string) error {
	sourceID := command.RetrySourceRunID
	if sourceID == "" {
		sourceID = command.ReplaySourceRunID
	}
	if sourceID == "" {
		return nil
	}
	state, err := s.runs.LoadRunState(ctx, sourceID)
	if err != nil {
		return err
	}
	if state.Run.DefinitionID != command.DefinitionID || state.Run.RevisionID != revisionID ||
		state.Run.Version != command.ExpectedSourceVersion {
		return status_error.WorkflowStateConflict
	}
	if command.ReplaySourceRunID != "" {
		if state.Run.Status != constants.WorkflowRunStatusSucceeded {
			return status_error.WorkflowStateConflict
		}
	} else if state.Run.Status != constants.WorkflowRunStatusFailed && state.Run.Status != constants.WorkflowRunStatusCancelled {
		return status_error.WorkflowStateConflict
	}
	branches, err := s.runs.ListJoinedRetryConflicts(ctx, state.Run)
	if err != nil {
		return err
	}
	if len(RetryConflictInstances(state, branches)) != 0 {
		return status_error.WorkflowStateConflict.WithField("reason", "optimize_uncertain_run")
	}
	return nil
}

// reuseRetryResult 只在同版本、输入及配置完全相同时复用成功伙伴结果或业务答案。
func (s *Service) reuseRetryResult(ctx context.Context, state *controllers.WorkflowRunState) error {
	sourceID, _ := state.Run.Context["retrySourceRunId"].(string)
	if sourceID == "" || len(state.Effects) == 0 {
		return nil
	}
	current := &state.Effects[len(state.Effects)-1]
	if current.Boundary.Primitive != "worker" && current.Boundary.Primitive != "askUser" {
		return nil
	}
	original, err := s.runs.LoadRunState(ctx, sourceID)
	if err != nil {
		return err
	}
	if original.Run.RevisionID != state.Run.RevisionID || original.Run.ScopeBinding != state.Run.ScopeBinding ||
		!slices.Equal(original.Run.AdditionalWorkingDirectories, state.Run.AdditionalWorkingDirectories) ||
		workflowstate.Digest(original.Run.WorkerConfigurations) != workflowstate.Digest(state.Run.WorkerConfigurations) {
		return nil
	}
	for _, previous := range original.Effects {
		if previous.Status != constants.WorkflowStepStatusSucceeded || previous.Result == nil || workflowstate.Digest(previous.Boundary) != workflowstate.Digest(current.Boundary) {
			continue
		}
		// 文件生命周期不能仅由字符串结果判断；此类节点继续真实执行并验证文件。
		encoded, _ := json.Marshal(workflowstate.BoundaryOptions(previous.Boundary))
		if strings.Contains(string(encoded), `"format":"file"`) {
			continue
		}
		previousPath := ""
		currentPath := ""
		for _, instance := range original.Instances {
			if instance.ID == previous.InstanceID {
				previousPath = instance.Path
			}
		}
		for _, instance := range state.Instances {
			if instance.ID == current.InstanceID {
				currentPath = instance.Path
			}
		}
		if previousPath != currentPath {
			continue
		}
		result := *previous.Result
		if current.Boundary.Primitive == "askUser" {
			options := workflowstate.BoundaryOptions(current.Boundary)
			if options["kind"] == "confirm" {
				continue
			}
			for i := range state.Humans {
				human := &state.Humans[i]
				if human.EffectID != current.ID {
					continue
				}
				data, _ := result.Data.(map[string]any)
				if data["status"] != "answered" || ValidateHumanAnswer(options, data["value"]) != nil {
					return nil
				}
				human.Status = constants.WorkflowStepStatusAnswered
				human.Source = "cache_replay"
				human.Value = data["value"]
				human.Options["reusedFrom"] = map[string]any{"runId": sourceID, "questionId": data["questionId"]}
				result = workflowdsl.JSONValue(map[string]any{"questionId": human.ID, "status": "answered", "source": "cache_replay", "value": human.Value, "reusedFrom": human.Options["reusedFrom"]})
			}
		}
		current.Result = &result
		current.Status = constants.WorkflowStepStatusSucceeded
		for i := range state.Instances {
			if state.Instances[i].ID == current.InstanceID {
				state.Instances[i].ReusedFrom = map[string]any{"runId": sourceID, "instanceId": previous.InstanceID, "attemptId": previous.ID}
			}
		}
		return nil
	}
	return nil
}
