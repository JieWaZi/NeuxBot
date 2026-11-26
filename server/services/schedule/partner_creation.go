package schedule

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// PartnerCreationBindings 只接收已核验的伙伴聊天和本轮执行配置。
func (s *Service) PartnerCreationBindings(chat models.Chat, executionID string, config execution.ExecutionConfiguration) []capability.Binding {
	if chat.WakerID == "" || executionID == "" || s.config.Commands == nil || s.config.Transactions == nil {
		return nil
	}
	workingDirectory := ""
	if chat.ProjectID == "" && len(chat.AdditionalWorkingDirectories) > 0 &&
		chat.AdditionalWorkingDirectories[0] != chat.WorkspaceDirectory {
		workingDirectory = chat.AdditionalWorkingDirectories[0]
	}
	operation := constants.AutomaticTaskCreateOperation
	return []capability.Binding{{
		Operation: operation,
		Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			// 等任务保存释放调度锁后，再发布包括聊天回执在内的变化。
			ctx, publish := events.HoldCommitEvents(ctx)
			defer publish()
			return capability.Execute(ctx, s.config.Commands, executionID, request, func(ctx context.Context) error {
				name, _ := request.Params["name"].(string)
				instruction, _ := request.Params["instruction"].(string)
				encoded, err := json.Marshal(request.Params["schedule"])
				var rule models.AutomaticTaskSchedule
				if err != nil || json.Unmarshal(encoded, &rule) != nil {
					return status_error.AgentInputInvalid
				}
				_, err = s.SaveConfiguration(ctx, chat.WakerID, "", 0, models.AutomaticTaskConfiguration{
					Name: name, Instruction: instruction, Trigger: constants.AutomaticTaskTriggerSchedule, Schedule: rule, Enabled: true,
					ProjectID: chat.ProjectID, WorkingDirectory: workingDirectory,
					RuntimeID: config.RuntimeID, AccountID: config.AccountID, Model: config.Model, Reasoning: config.Reasoning,
					Fast: config.Fast, PermissionLevel: config.PermissionLevel,
				}, true)
				return err
			})
		},
		Receipt: func(ctx context.Context, requestID string) (capability.Result, error) {
			return capability.Lookup(ctx, s.config.Commands, executionID, operation, requestID)
		},
	}}
}

func (s *Service) commitPartnerCreation(ctx context.Context, task models.AutomaticTask) error {
	if task.NextRunAt == nil {
		return status_error.AutomaticScheduleInvalid
	}
	waker, err := s.config.Wakers.Get(ctx, task.WakerID)
	if err != nil {
		return err
	}
	return s.config.Transactions.Transaction(ctx, func(ctx context.Context) error {
		if err := s.config.Tasks.Create(ctx, task); err != nil {
			return err
		}
		return capability.Commit(ctx, map[string]any{
			"taskId": task.ID, "name": task.Name, "wakerName": waker.Name,
			"instructionPreview": automaticInstructionPreview(task.Instruction),
			"schedule":           task.Schedule, "firstRunAt": *task.NextRunAt,
		})
	})
}

// automaticInstructionPreview 为原对话保留有界预览，任务详情持有完整指令。
func automaticInstructionPreview(instruction string) string {
	const maximumRunes = 300
	count := 0
	for position := range instruction {
		if count == maximumRunes {
			return strings.TrimSpace(instruction[:position]) + "…"
		}
		count++
	}
	return instruction
}
