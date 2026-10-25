package workflows

import (
	"context"
	"encoding/json"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 回答流程输入
type AnswerWorkflowHuman struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 请求标识
	HumanID string `in:"path" name:"humanId" validate:"required,max=32"`
	// 请求体
	Body AnswerHumanCommand `in:"body"`
}

func (*AnswerWorkflowHuman) Path() string { return "/:id/runs/:runId/humans/:humanId/answer" }
func (o *AnswerWorkflowHuman) Output(ctx *gin.Context) (interface{}, error) {
	run, err := global.WorkflowRunControllerManager.GetRun(ctx, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if run.DefinitionID != o.ID {
		return nil, ginx.WithStack(status_error.WorkflowNotFound)
	}
	o.Body.RunID = o.RunID
	o.Body.HumanID = o.HumanID
	if err := answerHuman(ctx, dependencies(), o.Body); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// 流程输入回答
type AnswerHumanCommand struct {
	// 运行标识
	RunID string `json:"-"`
	// 请求标识
	HumanID string `json:"-"`
	// 提交标识
	RequestID string `json:"requestId"`
	// 真实答案
	Value any `json:"value"`
	// 明确跳过
	Skip bool `json:"skip"`
}

// answerHuman 只接纳首次合法回答，默认值永远不会自动提交。
func answerHuman(ctx context.Context, d *workflowDependencies, command AnswerHumanCommand) error {
	if command.RequestID == "" || len(command.RequestID) > 128 {
		return status_error.BadRequest
	}
	encoded, err := json.Marshal(command.Value)
	if err != nil || len(encoded) > constants.WorkflowOutputLimitBytes {
		return status_error.WorkflowBudgetExceeded
	}
	digest := workflowstate.Digest([]any{command, command.RunID, command.HumanID})
	d.State.Commands.Lock()
	defer d.State.Commands.Unlock()
	return d.Transactions.Transaction(ctx, func(ctx context.Context) error {
		receipt, found, err := d.Store.FindReceipt(ctx, command.HumanID, command.RequestID)
		if err != nil {
			return err
		}
		if found {
			if receipt.Kind != "answer" || receipt.PayloadDigest != digest {
				return status_error.WorkflowRequestConflict
			}
			return nil
		}
		state, err := d.State.OwnedRun(ctx, command.RunID)
		if err != nil {
			return err
		}
		if workflowstate.TerminalRun(state.Run.Status) || state.Run.Status == constants.WorkflowRunStatusCancelling {
			return status_error.WorkflowStateConflict
		}
		index := -1
		for i := range state.Humans {
			if state.Humans[i].ID == command.HumanID {
				index = i
				break
			}
		}
		if index < 0 {
			return status_error.WorkflowNotFound
		}
		human := &state.Humans[index]
		if human.Status != constants.WorkflowStepStatusWaiting {
			return status_error.WorkflowStateConflict
		}
		now := time.Now().UnixMilli()
		if human.DeadlineAt > 0 && human.DeadlineAt <= now {
			return status_error.WorkflowStateConflict
		}
		if command.Skip {
			allowed, _ := human.Options["allowSkip"].(bool)
			if !allowed {
				return status_error.WorkflowStateConflict
			}
			human.Status = constants.WorkflowStepStatusSkipped
			human.Source = "skip"
			human.Value = workflowstate.HumanFallback(human.Options)
		} else {
			if err := validateHumanAnswer(human.Options, command.Value); err != nil {
				return err
			}
			human.Status = constants.WorkflowStepStatusAnswered
			human.Source = "user"
			human.Value = command.Value
		}
		human.AnsweredAt = &now
		if err = d.State.CommitRun(ctx, state); err != nil {
			return err
		}
		return d.Store.CreateReceipt(ctx, models.WorkflowCommandReceipt{AggregateID: command.HumanID, RequestID: command.RequestID, Kind: "answer", PayloadDigest: digest, Result: map[string]any{"accepted": true}})
	})
}

func validateHumanAnswer(options map[string]any, value any) error {
	if schema, structured := options["schema"].(map[string]any); structured {
		return workflowdsl.ValidateJSON(schema, value)
	}
	choices, _ := options["options"].([]any)
	multi, _ := options["multiSelect"].(bool)
	// 单选选项是快速回答，仍允许用户直接填写修改意见或其他自由文本。
	if !multi || len(choices) == 0 {
		if _, ok := value.(string); !ok {
			return status_error.WorkflowSchemaInvalid.WithField("field", "value")
		}
		return nil
	}
	values := []any{value}
	if multi {
		var ok bool
		values, ok = value.([]any)
		if !ok {
			return status_error.WorkflowSchemaInvalid.WithField("field", "value")
		}
	}
	seen := map[string]bool{}
	for _, answer := range values {
		encoded, _ := json.Marshal(answer)
		key := string(encoded)
		if seen[key] {
			return status_error.WorkflowSchemaInvalid.WithField("field", "value")
		}
		seen[key] = true
		found := false
		for _, item := range choices {
			choice, ok := item.(map[string]any)
			if !ok {
				continue
			}
			candidate, _ := json.Marshal(choice["value"])
			if key == string(candidate) {
				found = true
				break
			}
		}
		if !found {
			return status_error.WorkflowSchemaInvalid.WithField("field", "value")
		}
	}
	return nil
}
