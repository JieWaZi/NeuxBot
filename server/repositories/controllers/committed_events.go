package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

const eventPublisherSetting = "ally:committed-event-publisher"

func eventsResource(kind, id string) events.Resource { return events.Resource{Kind: kind, ID: id} }

// WithEventPublisher 给这份数据库装配注入窄事件端口，不注册 ORM 广播回调。
func WithEventPublisher(database *gorm.DB, publisher events.Publisher) *gorm.DB {
	// Set 返回可复用 Statement 的实例；再建立会话，避免不同 Model 继承同一可变 Schema。
	return database.Set(eventPublisherSetting, publisher).Session(&gorm.Session{})
}

// publishCommittedChange 由拥有领域写入的方法在成功后明确登记语义变化。
func publishCommittedChange(ctx context.Context, database *gorm.DB, topic string, resource events.Resource, affectedEntities ...events.Resource) {
	value, exists := database.Get(eventPublisherSetting)
	if !exists {
		return
	}
	publisher, ok := value.(events.Publisher)
	if !ok {
		return
	}
	var entityResources, automaticTaskResources []events.Resource
	if topic == "work-board" {
		var err error
		entityResources, automaticTaskResources, err = workBoardActivityResources(ctx, database, resource)
		if err != nil {
			_ = databaseError("capture_work_board_activity_resources", "kind="+resource.Kind+" id="+resource.ID, err)
			// 关联读取故障时要求权威快照恢复，不能静默留下过期活动摘要。
			entityResources = []events.Resource{eventsResource("global", "")}
			automaticTaskResources = []events.Resource{eventsResource("global", "")}
		}
		entityResources = append(entityResources, affectedEntities...)
	}
	events.AfterCommit(ctx, func() {
		_ = events.Emit(publisher, topic, topic+".changed", resource, resource)
		for _, entity := range entityResources {
			_ = events.Emit(publisher, "entities", constants.EntityActivityChangedEventType, entity, resource)
		}
		for _, task := range automaticTaskResources {
			_ = events.Emit(publisher, "automatic-tasks", "automatic-tasks.changed", task, resource)
		}
	})
}

// workBoardActivityResources 只沿原身份的索引读取关联，不把工作变化当作实体目录配置变化。
func workBoardActivityResources(ctx context.Context, database *gorm.DB, resource events.Resource) ([]events.Resource, []events.Resource, error) {
	db := controllerDB(ctx, database)
	entityResources := []events.Resource{}
	var thread models.ChannelThread
	runs := db.Model(&models.AutomaticRun{}).Select("task_id")
	configuredTasks := db.Model(&models.AutomaticTask{}).Select("id", "waker_id")
	switch resource.Kind {
	case "chat":
		var chat models.Chat
		if err := db.Select("id", "waker_id", "hidden_from_waker_history").Where("id = ?", resource.ID).Find(&chat).Error; err != nil {
			return nil, nil, err
		}
		if chat.ID != "" && chat.WakerID != "" && !chat.HiddenFromWakerHistory {
			entityResources = append(entityResources, eventsResource("waker", chat.WakerID))
		}
		// 成员 Chat 的等待与执行也是团队活动事实；普通 Chat 沿唯一 chat_id 索引立即结束。
		memberThread := db.Model(&models.ChannelThreadMember{}).Select("thread_id").Where("chat_id = ?", resource.ID)
		if err := db.Select("id", "channel_id").Where("id IN (?)", memberThread).Find(&thread).Error; err != nil {
			return nil, nil, err
		}
		if thread.ID == "" {
			runs = runs.Where("chat_id = ?", resource.ID)
		} else {
			runs = runs.Where("chat_id = ? OR thread_id = ?", resource.ID, thread.ID)
		}
	case "thread", "task":
		threads := db.Select("id", "channel_id")
		if resource.Kind == "thread" {
			threads = threads.Where("id = ?", resource.ID)
		} else {
			taskThread := db.Model(&models.ChannelTask{}).Select("thread_id").Where("id = ?", resource.ID)
			threads = threads.Where("id IN (?)", taskThread)
		}
		if err := threads.Find(&thread).Error; err != nil {
			return nil, nil, err
		}
		threadID := thread.ID
		if resource.Kind == "thread" {
			threadID = resource.ID
		}
		if threadID == "" {
			return nil, nil, nil
		}
		runs = runs.Where("thread_id = ?", threadID)
	case "workflow_run":
		runs = runs.Where("workflow_run_id = ?", resource.ID)
	case "channel":
		entityResources = append(entityResources, resource)
		// 配置依赖不要求已有运行；团队删除也必须刷新尚未执行的任务详情。
		configuredTasks = configuredTasks.Where("channel_id = ?", resource.ID)
		runs = nil
	case "global", "workflow":
		// 全局工作变化包含自动失败阅读确认；它不改变伙伴或团队的活动计数。
		return nil, []events.Resource{eventsResource("global", "")}, nil
	default:
		return nil, nil, nil
	}
	if thread.ChannelID != "" {
		entityResources = append(entityResources, eventsResource("channel", thread.ChannelID))
	}
	var automaticRuns []models.AutomaticRun
	if runs != nil {
		if err := runs.Find(&automaticRuns).Error; err != nil {
			return nil, nil, err
		}
		if len(automaticRuns) == 0 {
			return entityResources, nil, nil
		}
		configuredTasks = configuredTasks.Where("id IN (?)", runs)
	}
	var automaticTasks []models.AutomaticTask
	if err := configuredTasks.Find(&automaticTasks).Error; err != nil {
		return nil, nil, err
	}
	tasks := make([]events.Resource, 0, len(automaticRuns)+len(automaticTasks))
	seenTasks := map[string]bool{}
	for _, run := range automaticRuns {
		if run.TaskID != "" && !seenTasks[run.TaskID] {
			seenTasks[run.TaskID] = true
			tasks = append(tasks, eventsResource("automatic_task", run.TaskID))
		}
	}
	seenWakers := map[string]bool{}
	for _, task := range automaticTasks {
		if !seenTasks[task.ID] {
			seenTasks[task.ID] = true
			tasks = append(tasks, eventsResource("automatic_task", task.ID))
		}
		// 伙伴的待操作摘要按当前任务归属读取，不能使用历史运行的负责人快照。
		if task.WakerID != "" && !seenWakers[task.WakerID] {
			seenWakers[task.WakerID] = true
			tasks = append(tasks, eventsResource("waker", task.WakerID))
		}
	}
	return entityResources, tasks, nil
}

// publishCommittedNoticeIDs 仅登记真实新增/失效身份，不能由有限预览差集推算。
func publishCommittedNoticeIDs(ctx context.Context, database *gorm.DB, upserts, removals []string) {
	value, exists := database.Get(eventPublisherSetting)
	publisher, ok := value.(events.Publisher)
	if !exists || !ok || len(upserts)+len(removals) == 0 {
		return
	}
	upserts = append([]string{}, upserts...)
	removals = append([]string{}, removals...)
	events.AfterCommit(ctx, func() {
		_ = events.Emit(publisher, "notification-state", "changed", events.Resource{Kind: "global"}, map[string]any{"noticeIds": upserts, "removedIds": removals})
	})
}
