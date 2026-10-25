package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowinput "github.com/JieWaZi/NeuxBot/server/internal/workflow/input"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 工作流触发方式
type WorkflowTrigger struct {
	// 参数来源
	InputBindings map[string]workflowinput.Binding `json:"inputBindings"`
	// 任务标识
	TaskID string `json:"taskId" validate:"max=32"`
	// 触发类型
	Trigger constants.AutomaticTaskTrigger `json:"trigger"`
	// 定时规则
	Schedule models.AutomaticTaskSchedule `json:"schedule"`
}

// 触发配置请求
type SaveWorkflowTriggersCommand struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=120"`
	// 配置指纹
	ExpectedFingerprint string `json:"expectedFingerprint" validate:"required,max=64"`
	// 公共配置
	Configuration models.AutomaticTaskConfiguration `json:"configuration"`
	// 触发方式
	Triggers []WorkflowTrigger `json:"triggers" validate:"max=5,dive"`
}

// 工作流触发配置
type WorkflowTriggersView struct {
	// 自动任务
	Tasks []models.AutomaticTask `json:"tasks"`
	// 配置指纹
	Fingerprint string `json:"fingerprint"`
}

// 触发配置回执
type SavedWorkflowTriggers struct {
	// 配置指纹
	Fingerprint string `json:"fingerprint"`
	// 任务回执
	Tasks []schedule.SavedTask `json:"tasks"`
	// 请求重放
	Replayed bool `json:"replayed"`
}

func triggersFingerprint(tasks []models.AutomaticTask) string {
	identities := make([]string, 0, len(tasks))
	for _, task := range tasks {
		data, _ := json.Marshal([]any{task.ID, task.ConfigurationVersion})
		identities = append(identities, string(data))
	}
	sort.Strings(identities)
	digest := sha256.Sum256([]byte(strings.Join(identities, "\n")))
	return hex.EncodeToString(digest[:])
}

func readWorkflowTriggers(ctx context.Context, tasks controllers.AutomaticTaskController, workflowID string) (WorkflowTriggersView, error) {
	values, _, err := tasks.List(ctx, controllers.AutomaticTaskFilter{WorkflowID: workflowID})
	if err != nil {
		return WorkflowTriggersView{}, err
	}
	return WorkflowTriggersView{Tasks: values, Fingerprint: triggersFingerprint(values)}, nil
}

// 保存工作流触发配置
type SaveWorkflowTriggers struct {
	ginx.MethodPut
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 请求体
	Body SaveWorkflowTriggersCommand `in:"body"`
}

// Path 返回整份触发配置替换路径。
func (*SaveWorkflowTriggers) Path() string { return "/:id/triggers" }

// Output 校验整份配置并原子替换触发方式、参数绑定与幂等收据。
func (o *SaveWorkflowTriggers) Output(ginContext *gin.Context) (interface{}, error) {
	if _, err := global.WorkflowControllerManager.GetDefinition(ginContext, o.ID); err != nil {
		return nil, ginx.WithStack(err)
	}
	workflowID, command := o.ID, o.Body
	scheduler, tasks, transactions := global.ScheduleService, global.AutomaticTaskControllerManager, global.TransactionControllerManager
	ctx, release := scheduler.AcquireMutation(ginContext)
	defer release()
	if transactions == nil || command.RequestID == "" || len(command.RequestID) > 120 || len(command.Triggers) > 5 || command.Configuration.WorkflowID != workflowID || command.Configuration.TargetKind != constants.AutomaticTaskTargetKindWorkflow {
		return SavedWorkflowTriggers{}, status_error.BadRequest
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return SavedWorkflowTriggers{}, status_error.BadRequest
	}
	hash := sha256.Sum256(payload)
	digest := hex.EncodeToString(hash[:])
	result := SavedWorkflowTriggers{Tasks: []schedule.SavedTask{}}
	err = transactions.Transaction(ctx, func(ctx context.Context) error {
		receipt, exists, err := tasks.FindWorkflowTriggerReceipt(ctx, workflowID, command.RequestID)
		if err != nil {
			return err
		}
		if exists {
			if receipt.Digest != digest {
				return status_error.AutomaticTaskRequestConflict
			}
			result.Fingerprint, result.Replayed = receipt.Fingerprint, true
			for _, id := range receipt.TaskIDs {
				result.Tasks = append(result.Tasks, schedule.SavedTask{TaskID: id})
			}
			return nil
		}
		current, err := readWorkflowTriggers(ctx, tasks, workflowID)
		if err != nil {
			return err
		}
		if current.Fingerprint != command.ExpectedFingerprint {
			return status_error.AutomaticTaskRevisionConflict
		}
		byID := map[string]models.AutomaticTask{}
		for _, task := range current.Tasks {
			byID[task.ID] = task
		}
		seen := map[string]bool{}
		ids := []string{}
		for _, trigger := range command.Triggers {
			config := command.Configuration
			config.Trigger, config.Schedule = trigger.Trigger, trigger.Schedule
			config.WorkflowInputBindings = trigger.InputBindings
			var version int64
			if trigger.TaskID != "" {
				task, found := byID[trigger.TaskID]
				if !found || seen[trigger.TaskID] {
					return status_error.BadRequest
				}
				version = task.ConfigurationVersion
				seen[trigger.TaskID] = true
			}
			saved, err := scheduler.SaveConfiguration(ctx, "", trigger.TaskID, version, config, true)
			if err != nil {
				return err
			}
			result.Tasks = append(result.Tasks, saved)
			ids = append(ids, saved.TaskID)
		}
		for id, task := range byID {
			if seen[id] {
				continue
			}
			stored := task.Revision
			task.Enabled, task.WorkflowTriggerDetached, task.NextRunAt = false, true, nil
			task.Revision++
			task.ConfigurationVersion++
			task.UpdatedAt = time.Now().UnixMilli()
			if err := tasks.UpdateConfiguration(ctx, task, stored); err != nil {
				return err
			}
		}
		updated, err := readWorkflowTriggers(ctx, tasks, workflowID)
		if err != nil {
			return err
		}
		result.Fingerprint = updated.Fingerprint
		return tasks.CreateWorkflowTriggerReceipt(ctx, models.WorkflowTriggerReceipt{WorkflowID: workflowID, RequestID: command.RequestID, Digest: digest, Fingerprint: result.Fingerprint, TaskIDs: ids})
	})
	if err != nil {
		return SavedWorkflowTriggers{}, err
	}
	return result, nil
}
