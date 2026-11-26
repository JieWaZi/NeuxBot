package schedule

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// PartnerEditBindings 按受信聊天身份读取及编辑该对话所建的定时任务。
func (s *Service) PartnerEditBindings(chat models.Chat, executionID string) []capability.Binding {
	if chat.ID == "" || chat.WakerID == "" || executionID == "" || s.config.Commands == nil || s.config.Transactions == nil {
		return nil
	}
	return []capability.Binding{
		{Operation: constants.AutomaticTaskListOperation, Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			return s.listPartnerTasks(ctx, chat, request)
		}},
		{
			Operation: constants.AutomaticTaskUpdateOperation,
			Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
				ctx, publish := events.HoldCommitEvents(ctx)
				defer publish()
				return capability.Execute(ctx, s.config.Commands, executionID, request, func(ctx context.Context) error {
					return s.editPartnerTask(ctx, chat, request)
				})
			},
			Receipt: func(ctx context.Context, requestID string) (capability.Result, error) {
				return capability.Lookup(ctx, s.config.Commands, executionID, constants.AutomaticTaskUpdateOperation, requestID)
			},
		},
	}
}

func (s *Service) listPartnerTasks(ctx context.Context, chat models.Chat, request capability.Request) (capability.Result, error) {
	taskID, _ := request.Params["taskId"].(string)
	offset, _ := request.Params["offset"].(float64)
	const pageSize = 20
	limit := pageSize + 1
	if taskID != "" {
		offset, limit = 0, 1
	}
	tasks, err := s.config.Tasks.ListChatSchedules(ctx, chat.ID, chat.WakerID, taskID, limit, int(offset))
	if err != nil {
		return capability.Result{}, err
	}
	if taskID != "" && len(tasks) == 0 {
		return capability.Result{}, status_error.AutomaticTaskNotFound
	}
	data := map[string]any{}
	if len(tasks) > pageSize {
		data["nextOffset"] = int(offset) + pageSize
		tasks = tasks[:pageSize]
	}
	items := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		item := partnerTaskEditReceipt(task)
		if taskID != "" {
			item["instruction"] = task.Instruction
		}
		items = append(items, item)
	}
	data["items"] = items
	return capability.Result{Data: data}, nil
}

func (s *Service) editPartnerTask(ctx context.Context, chat models.Chat, request capability.Request) error {
	var edit struct {
		// TaskID 来自该聊天的创建回执。
		TaskID string `json:"taskId"`
		// Revision 是读取到的任务配置版本。
		Revision int64 `json:"revision"`
		// Name 是用户要求修改的任务名称。
		Name *string `json:"name"`
		// Instruction 是用户要求保存的完整执行指令。
		Instruction *string `json:"instruction"`
		// Schedule 是用户要求保存的完整定时规则。
		Schedule *models.AutomaticTaskSchedule `json:"schedule"`
	}
	data, err := json.Marshal(request.Params)
	if err != nil || json.Unmarshal(data, &edit) != nil || edit.TaskID == "" || edit.Revision <= 0 ||
		(edit.Name == nil && edit.Instruction == nil && edit.Schedule == nil) {
		return status_error.AgentInputInvalid
	}
	tasks, err := s.config.Tasks.ListChatSchedules(ctx, chat.ID, chat.WakerID, edit.TaskID, 1, 0)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return status_error.AutomaticTaskNotFound
	}
	config := tasks[0].AutomaticTaskConfiguration
	if edit.Name != nil {
		config.Name = *edit.Name
	}
	if edit.Instruction != nil {
		config.Instruction = *edit.Instruction
	}
	if edit.Schedule != nil {
		config.Schedule = *edit.Schedule
	}
	_, err = s.SaveConfiguration(ctx, chat.WakerID, edit.TaskID, edit.Revision, config, true)
	return err
}

func partnerTaskEditReceipt(task models.AutomaticTask) map[string]any {
	return map[string]any{
		"taskId": task.ID, "name": task.Name, "schedule": partnerTaskEditSchedule(task.Schedule),
		"revision": task.ConfigurationVersion, "nextRunAt": task.NextRunAt,
	}
}

// partnerTaskEditSchedule 返回可直接提交给更新命令的定时规则。
func partnerTaskEditSchedule(rule models.AutomaticTaskSchedule) map[string]any {
	schedule := map[string]any{"kind": rule.Kind, "timezone": rule.Timezone}
	switch rule.Kind {
	case constants.AutomaticScheduleKindOnce:
		schedule["onceAt"] = rule.OnceAt
	case constants.AutomaticScheduleKindHourly:
		schedule["minute"] = rule.Minute
	case constants.AutomaticScheduleKindDaily:
		schedule["hour"], schedule["minute"] = rule.Hour, rule.Minute
	case constants.AutomaticScheduleKindWeekly:
		schedule["hour"], schedule["minute"], schedule["weekday"] = rule.Hour, rule.Minute, rule.Weekday
	case constants.AutomaticScheduleKindMonthly:
		schedule["hour"], schedule["minute"], schedule["dayOfMonth"] = rule.Hour, rule.Minute, rule.DayOfMonth
	case constants.AutomaticScheduleKindCron:
		schedule["cron"] = rule.Cron
	}
	return schedule
}
