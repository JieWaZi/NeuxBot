package workflow

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"strings"
)

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
	if original.Run.RevisionID != state.Run.RevisionID || workflowstate.Digest(original.Run.ScopeBinding) != workflowstate.Digest(state.Run.ScopeBinding) || workflowstate.Digest(original.Run.WorkerConfigurations) != workflowstate.Digest(state.Run.WorkerConfigurations) {
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
