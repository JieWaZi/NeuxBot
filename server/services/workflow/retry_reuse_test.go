package workflow

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	dsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"testing"
)

// retryEvidenceStore 提供原运行冻结证据，只测试复用策略。
type retryEvidenceStore struct {
	// WorkflowRunController 保留其余持久化端口。
	controllers.WorkflowRunController
	// original 是原运行的冻结证据。
	original controllers.WorkflowRunState
}

func (r retryEvidenceStore) LoadRunState(context.Context, string) (controllers.WorkflowRunState, error) {
	return r.original, nil
}

func TestRetryReuseEvidenceBoundaries(t *testing.T) {
	for _, kind := range []string{"worker", "revision", "input", "path", "scope", "configuration", "failed", "unknown", "candidate", "file", "action", "select", "confirm", "invalid-selection", "skipped"} {
		t.Run(kind, func(t *testing.T) {
			options := map[string]any{}
			primitive := "worker"
			data := any("validated")
			if kind == "select" || kind == "confirm" || kind == "invalid-selection" || kind == "skipped" {
				primitive = "askUser"
				options = map[string]any{"kind": "select", "choices": []any{map[string]any{"value": "a", "label": "甲"}}}
				value := any("a")
				status := "answered"
				if kind == "confirm" {
					options = map[string]any{"kind": "confirm"}
					value = true
				}
				if kind == "invalid-selection" {
					value = "removed"
				}
				if kind == "skipped" {
					status = "skipped"
				}
				data = map[string]any{"questionId": "old-question", "status": status, "source": "user", "value": value}
			}
			if kind == "file" {
				options = map[string]any{"schema": map[string]any{"type": "string", "format": "file"}}
			}
			if kind == "action" {
				primitive = "action"
			}
			boundary := func() dsl.EffectBoundary {
				return dsl.EffectBoundary{Primitive: primitive, NodeID: "node", Arguments: []dsl.Value{dsl.JSONValue("input"), dsl.JSONValue(options)}}
			}
			result := dsl.JSONValue(data)
			old := controllers.WorkflowRunState{Run: models.WorkflowRun{ID: "old", RevisionID: "rev"}, Instances: []models.WorkflowNodeInstance{{ID: "old-node", Path: "path"}}, Effects: []models.WorkflowEffect{{ID: "old-effect", InstanceID: "old-node", Boundary: boundary(), Status: constants.WorkflowStepStatusSucceeded, Result: &result}}}
			current := controllers.WorkflowRunState{Run: models.WorkflowRun{ID: "new", RevisionID: "rev", Context: map[string]any{"retrySourceRunId": "old"}}, Instances: []models.WorkflowNodeInstance{{ID: "new-node", Path: "path"}}, Effects: []models.WorkflowEffect{{ID: "new-effect", InstanceID: "new-node", Boundary: boundary(), Status: constants.WorkflowStepStatusPrepared}}, Humans: []models.WorkflowHumanRequest{{ID: "new-question", EffectID: "new-effect", Options: map[string]any{}}}}
			switch kind {
			case "revision":
				current.Run.RevisionID = "changed"
			case "input":
				current.Effects[0].Boundary.Arguments[0] = dsl.JSONValue("changed")
			case "path":
				current.Instances[0].Path = "changed"
			case "scope":
				current.Run.ScopeBinding.WorkingDirectory = "/changed"
			case "configuration":
				current.Run.WorkerConfigurations = map[string]models.WorkflowExecutionDefaults{"node": {Model: "changed"}}
			case "failed":
				old.Effects[0].Status = constants.WorkflowStepStatusFailed
			case "unknown":
				old.Effects[0].Status = constants.WorkflowStepStatusUnknown
			case "candidate":
				old.Effects[0].Candidate = old.Effects[0].Result
				old.Effects[0].Result = nil
			}
			service := &Service{runs: retryEvidenceStore{original: old}}
			if err := service.reuseRetryResult(context.Background(), &current); err != nil {
				t.Fatal(err)
			}
			want := kind == "worker" || kind == "select"
			if (current.Effects[0].Result != nil) != want {
				t.Fatalf("unexpected reuse for %s: %+v", kind, current)
			}
			if !want {
				return
			}
			if current.Instances[0].ReusedFrom["instanceId"] != "old-node" {
				t.Fatal("missing provenance")
			}
			if kind == "select" {
				answer := current.Effects[0].Result.Data.(map[string]any)
				if answer["questionId"] != "new-question" || answer["source"] != "cache_replay" || current.Humans[0].Status != constants.WorkflowStepStatusAnswered || current.Humans[0].Value != "a" {
					t.Fatal("answer lost identity or source", answer)
				}
			}
		})
	}
}
