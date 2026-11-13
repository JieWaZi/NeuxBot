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
	publishCommittedEvent(ctx, database, topic, resource, resource, affectedEntities...)
}

// PublishReadingChanged 合并同一批阅读写入对派生摘要的失效。
func (c *workBoardController) PublishReadingChanged(ctx context.Context) {
	resource := eventsResource("global", "")
	publishCommittedChange(ctx, c.db, "work-board", resource, resource)
}

func publishCommittedWorkBoardChange(ctx context.Context, database *gorm.DB, resource events.Resource, reason string) {
	payload := struct {
		// Resource 是此次修复覆盖的原资源。
		events.Resource
		// Reason 说明已有客户端提醒需重新核对。
		Reason string `json:"reason"`
	}{Resource: resource, Reason: reason}
	publishCommittedEvent(ctx, database, "work-board", resource, payload)
}

func publishCommittedEvent(ctx context.Context, database *gorm.DB, topic string, resource events.Resource, payload any, affectedEntities ...events.Resource) {
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
		entityResources = boundedWorkResources(entityResources)
		automaticTaskResources = boundedWorkResources(automaticTaskResources)
	}
	events.AfterCommit(ctx, func() {
		_ = events.Emit(publisher, topic, topic+".changed", resource, payload)
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
		var chat struct {
			// ID 是原会话。
			ID string
			// WakerID 是原会话所属伙伴。
			WakerID string
			// HiddenFromWakerHistory 表示该会话从其他产品入口进入。
			HiddenFromWakerHistory bool
			// VisibleExternal 表示外部入口已绑定可见原工作。
			VisibleExternal bool
		}
		projection := `id,waker_id,hidden_from_waker_history,
   CASE WHEN hidden_from_waker_history AND NOT EXISTS(SELECT 1 FROM workflow_effects WHERE chat_id=chats.id)
    AND (EXISTS(SELECT 1 FROM automatic_runs WHERE chat_id=chats.id AND thread_id='' AND admission='accepted')
     OR EXISTS(SELECT 1 FROM im_conversation_links WHERE chat_id=chats.id AND thread_id='')
     OR EXISTS(SELECT 1 FROM workflow_creation_sessions WHERE chat_id=chats.id)) THEN 1 ELSE 0 END AS visible_external`
		if err := db.Model(&models.Chat{}).Select(projection).Where("id=?", resource.ID).Find(&chat).Error; err != nil {
			return nil, nil, err
		}

		// 成员 Chat 的等待与执行也是团队活动事实；普通 Chat 沿唯一 chat_id 索引立即结束。
		memberThread := db.Model(&models.ChannelThreadMember{}).Select("thread_id").Where("chat_id = ?", resource.ID)
		if err := db.Select("id", "channel_id").Where("id IN (?)", memberThread).Find(&thread).Error; err != nil {
			return nil, nil, err
		}
		if thread.ID == "" {
			if chat.ID != "" && chat.WakerID != "" && (!chat.HiddenFromWakerHistory || chat.VisibleExternal) {
				entityResources = append(entityResources, eventsResource("waker", chat.WakerID))
			}
			runs = runs.Where("chat_id=?", resource.ID)
		} else {
			runs = runs.Where("chat_id = ? OR thread_id = ?", resource.ID, thread.ID)
		}
	case "thread", "team_thread", "task":
		threads := db.Select("id", "channel_id")
		if resource.Kind == "thread" || resource.Kind == "team_thread" {
			threads = threads.Where("id = ?", resource.ID)
		} else {
			taskThread := db.Model(&models.ChannelTask{}).Select("thread_id").Where("id = ?", resource.ID)
			threads = threads.Where("id IN (?)", taskThread)
		}
		if err := threads.Find(&thread).Error; err != nil {
			return nil, nil, err
		}
		threadID := thread.ID
		if resource.Kind == "thread" || resource.Kind == "team_thread" {
			threadID = resource.ID
		}
		if threadID == "" {
			return nil, nil, nil
		}
		runs = runs.Where("thread_id = ?", threadID)
	case "workflow_run":
		runs = runs.Where("workflow_run_id = ?", resource.ID)
	case "automatic_run":
		runs = runs.Where("id = ?", resource.ID)
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

// publishWorkLive 发送原身份及不参与补读的实时提醒。
func publishWorkLive(ctx context.Context, db *gorm.DB, value models.WorkBoardLiveEvent) {
	if value.Work.ID == "" {
		return
	}
	configured, _ := db.Get(eventPublisherSetting)
	publisher, ok := configured.(events.Publisher)
	if !ok {
		return
	}
	events.AfterCommit(ctx, func() {
		_ = events.EmitTransient(publisher, "work-board", "work-board.live", eventsResource(string(value.Work.Kind), value.Work.ID), value)
	})
}

func boundedWorkResources(values []events.Resource) []events.Resource {
	if len(values) > events.MaximumSubscriptions {
		return []events.Resource{eventsResource("global", "")}
	}
	return values
}
