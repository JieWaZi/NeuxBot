package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelThreadController 定义团队对话快照、成员 Chat 映射和持久事件操作。
type ChannelThreadController interface {
	// Create 创建尚处于首发补偿窗口的团队对话及全部成员快照。
	Create(ctx context.Context, thread models.ChannelThread, members []models.ChannelThreadMember) error
	// Get 按标识返回团队对话，包含尚未就绪的内部记录。
	Get(ctx context.Context, id string) (models.ChannelThread, error)
	// FindByChatID 按成员 Chat 反查所属团队对话；普通 Chat 不匹配。
	FindByChatID(context.Context, string) (models.ChannelThread, bool, error)
	// GetByClientRequestID 按团队和首发幂等标识返回内部记录；不存在时 exists 为 false。
	GetByClientRequestID(ctx context.Context, channelID string, clientRequestID string) (thread models.ChannelThread, exists bool, err error)
	// ListByChannel 按不透明游标、搜索词和活动状态返回已就绪对话。
	ListByChannel(ctx context.Context, query ChannelThreadListQuery) (ChannelThreadListResult, error)
	// ListByChannelUpdatedRange 按搜索词和最近活动时间范围分页返回团队的已就绪对话。
	ListByChannelUpdatedRange(ctx context.Context, channelID string, search string, updatedAtStart int64, updatedAtEnd int64, limit int, offset int) ([]models.ChannelThread, error)
	// CountByChannelUpdatedRange 返回团队符合搜索词和最近活动时间筛选的已就绪对话总数。
	CountByChannelUpdatedRange(ctx context.Context, channelID string, search string, updatedAtStart int64, updatedAtEnd int64) (int64, error)
	// ListIDsByChannel 返回团队下包括初始化残留在内的全部对话标识。
	ListIDsByChannel(ctx context.Context, channelID string) ([]string, error)
	// ListMembers 按快照顺序返回团队对话的成员 Chat 映射。
	ListMembers(ctx context.Context, threadID string) ([]models.ChannelThreadMember, error)
	// ListChatIDs 返回团队对话拥有的去重 Chat 标识。
	ListChatIDs(ctx context.Context, threadID string) ([]string, error)
	// GetMember 按伙伴标识返回团队对话中的成员 Chat 映射。
	GetMember(ctx context.Context, threadID string, wakerID string) (models.ChannelThreadMember, error)
	// ReserveTaskSlot 原子占用一个尚未达到对话上限的任务名额。
	ReserveTaskSlot(ctx context.Context, threadID string) (bool, error)
	// UpdateMemberExecutionConfigs 更新成员后续新执行使用的完整配置。
	UpdateMemberExecutionConfigs(ctx context.Context, members []models.ChannelThreadMember) error
	// UpdateProvisioned 保存 staging 发布后由成员 Chat 冻结的工作区信息。
	UpdateProvisioned(ctx context.Context, thread models.ChannelThread) error
	// MarkReady 标记全部成员 Chat 和团队对话首发事实已经完成。
	MarkReady(ctx context.Context, threadID string, provisioningID string, readyAt int64) error
	// BeginCanceling 原子写入停止意图并冻结新的任务名额与派发。
	BeginCanceling(ctx context.Context, threadID string, cancelingAt int64) (bool, error)
	// FinishCanceling 在停止收敛事务末尾按原意图清除冻结状态。
	FinishCanceling(ctx context.Context, threadID string, cancelingAt int64, updatedAt int64) (bool, error)
	// ListCanceling 返回服务启动时必须继续收敛的停止意图。
	ListCanceling(ctx context.Context) ([]models.ChannelThread, error)
	// ListInitializing 返回服务启动时必须回滚的未就绪首发锚点。
	ListInitializing(ctx context.Context) ([]models.ChannelThread, error)
	// Touch 更新团队对话最近活动时间。
	Touch(ctx context.Context, threadID string, updatedAt int64) error
	// DeleteInitializing 删除仍处于首发补偿窗口的团队对话及成员快照。
	DeleteInitializing(ctx context.Context, threadID string) error
	// CountActiveWork 统计阻止删除的待执行任务、运行任务、负责人输入和活跃 Chat Execution。
	CountActiveWork(ctx context.Context, threadID string) (int64, error)
	// Delete 删除一个团队对话的全部结构化编排事实。
	Delete(ctx context.Context, threadID string) error
	// DeleteByChannel 删除一个团队下全部团队对话的结构化编排事实。
	DeleteByChannel(ctx context.Context, channelID string) error
	// AppendEvent 在当前事务中追加一条团队持久事件。
	AppendEvent(ctx context.Context, event models.ChannelEvent) (models.ChannelEvent, error)
	// ListEvents 返回指定全局游标之后的团队对话持久事件。
	ListEvents(ctx context.Context, threadID string, after string, limit int) ([]models.ChannelEvent, error)
	// LatestEventCursor 返回团队对话最新持久事件游标；没有事件时返回空字符串。
	LatestEventCursor(ctx context.Context, threadID string) (string, error)
	// GetLeadInput 根据稳定输入身份对账团队续发收据。
	GetLeadInput(context.Context, string) (models.ChannelLeadInput, bool, error)
	// CreateLeadInput 持久化一条等待进入负责人 Chat 的完整用户输入。
	CreateLeadInput(ctx context.Context, input models.ChannelLeadInput) error
	// ListLeadInputs 返回指定状态的负责人输入队列，按创建顺序排列。
	ListLeadInputs(ctx context.Context, threadID string, statuses []constants.ChannelInputStatus, limit int) ([]models.ChannelLeadInput, error)
	// UpdateLeadInputs 推进一批负责人输入的投递状态和 Execution 映射。
	UpdateLeadInputs(ctx context.Context, ids []string, status constants.ChannelInputStatus, leadExecutionID string, updatedAt int64) error
	// CancelOpenLeadInputs 取消团队对话尚未消费的负责人用户输入。
	CancelOpenLeadInputs(ctx context.Context, threadID string, updatedAt int64) error
	// RecoverLeadInputs 收尾中断投递并返回仍有待处理输入的团队对话标识。
	RecoverLeadInputs(ctx context.Context, updatedAt int64) ([]string, error)
}

// ChannelThreadListCursor 是 Repository 使用的稳定更新时间与 ID 复合游标。
type ChannelThreadListCursor struct {
	// UpdatedAt 是上一页最后一项的最近活动时间。
	UpdatedAt int64
	// ID 是上一页最后一项的团队对话标识。
	ID string
}

// ChannelThreadListQuery 描述团队会话列表的服务端筛选与游标窗口。
type ChannelThreadListQuery struct {
	// ChannelID 是团队标识。
	ChannelID string
	// Search 是会话标题、项目和工作区的搜索词。
	Search string
	// Status 是可选聚合活动状态。
	Status constants.ChannelActivityState
	// Cursor 是上一页最后一项；为空表示第一页。
	Cursor *ChannelThreadListCursor
	// Limit 是单页返回上限。
	Limit int
}

// ChannelThreadListResult 是 Repository 返回的会话窗口和下一页游标。
type ChannelThreadListResult struct {
	// Threads 是当前页团队会话。
	Threads []models.ChannelThread
	// NextCursor 是存在下一页时当前页最后一项的复合游标。
	NextCursor *ChannelThreadListCursor
}

// channelThreadController 使用 GORM 实现团队对话持久化。
type channelThreadController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewChannelThreadController 创建团队对话 Repository Controller。
func NewChannelThreadController(db *gorm.DB) ChannelThreadController {
	return &channelThreadController{db: db}
}

// Create 创建尚处于首发补偿窗口的团队对话及全部成员快照。
func (c *channelThreadController) Create(
	ctx context.Context,
	thread models.ChannelThread,
	members []models.ChannelThreadMember,
) error {
	db := controllerDB(ctx, c.db)
	if err := db.Create(&thread).Error; err != nil {
		return databaseError("create_channel_thread", "thread_id="+thread.ID, err)
	}
	if err := db.Create(&members).Error; err != nil {
		return databaseError("create_channel_thread_members", "thread_id="+thread.ID, err)
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("thread", thread.ID))
	return nil
}

// Get 按标识返回团队对话，包含尚未就绪的内部记录。
func (c *channelThreadController) Get(
	ctx context.Context,
	id string,
) (models.ChannelThread, error) {
	var thread models.ChannelThread
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&thread).Error
	if err == nil {
		return thread, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ChannelThread{}, status_error.ChannelThreadNotFound.WithParams(
			map[string]interface{}{"ThreadID": id},
		)
	}
	return models.ChannelThread{}, databaseError(
		"get_channel_thread",
		"thread_id="+id,
		err,
	)
}

// GetByClientRequestID 按团队和首发幂等标识返回内部记录；不存在时 exists 为 false。
func (c *channelThreadController) GetByClientRequestID(
	ctx context.Context,
	channelID string,
	clientRequestID string,
) (models.ChannelThread, bool, error) {
	var thread models.ChannelThread
	err := controllerDB(ctx, c.db).
		Where("channel_id = ? AND client_request_id = ?", channelID, clientRequestID).
		First(&thread).Error
	if err == nil {
		return thread, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ChannelThread{}, false, nil
	}
	return models.ChannelThread{}, false, databaseError(
		"get_channel_thread_by_client_request",
		"channel_id="+channelID,
		err,
	)
}

const channelThreadWaitingSQL = `(EXISTS (
	SELECT 1 FROM channel_tasks AS waiting_task
	WHERE waiting_task.thread_id = thread.id AND waiting_task.status = 'in_review'
) OR EXISTS (
	SELECT 1 FROM channel_thread_members AS waiting_member
	WHERE waiting_member.thread_id = thread.id
	AND EXISTS (SELECT 1 FROM chats WHERE id = waiting_member.chat_id AND (waiting_approval = 1 OR waiting_elicitation = 1))
))`

const channelThreadRunningSQL = `(EXISTS (
	SELECT 1 FROM channel_tasks AS running_task
	WHERE running_task.thread_id = thread.id AND running_task.status IN ('pending', 'in_progress')
) OR EXISTS (
	SELECT 1 FROM channel_thread_members AS running_member
	WHERE running_member.thread_id = thread.id
	AND EXISTS (SELECT 1 FROM chats WHERE id = running_member.chat_id AND active_execution_id <> '')
) OR EXISTS (
	SELECT 1 FROM channel_lead_inputs AS running_input
	WHERE running_input.thread_id = thread.id AND running_input.status IN ('pending', 'delivering')
) OR EXISTS (
	SELECT 1 FROM channel_lead_notifications AS running_notification
	WHERE running_notification.thread_id = thread.id AND running_notification.status IN ('pending', 'delivering')
))`

const channelThreadActivitySelect = `thread.*,
	CASE
		WHEN ` + channelThreadWaitingSQL + ` THEN 'waiting'
		WHEN ` + channelThreadRunningSQL + ` THEN 'running'
		ELSE 'idle'
	END AS activity_state`

// ListByChannel 按不透明游标、搜索词和活动状态返回已就绪对话。
func (c *channelThreadController) ListByChannel(
	ctx context.Context,
	query ChannelThreadListQuery,
) (ChannelThreadListResult, error) {
	threads := []models.ChannelThread{}
	db := filterChannelThreads(
		controllerDB(ctx, c.db).
			Table("channel_threads AS thread").
			Select(channelThreadActivitySelect),
		query.ChannelID,
		query.Search,
		0,
		0,
	)
	switch query.Status {
	case constants.ChannelActivityStateWaiting:
		db = db.Where(channelThreadWaitingSQL)
	case constants.ChannelActivityStateRunning:
		db = db.Where("NOT " + channelThreadWaitingSQL).Where(channelThreadRunningSQL)
	case constants.ChannelActivityStateIdle:
		db = db.Where("NOT " + channelThreadWaitingSQL).Where("NOT " + channelThreadRunningSQL)
	}
	if query.Cursor != nil {
		db = db.Where(
			"thread.updated_at < ? OR (thread.updated_at = ? AND thread.id < ?)",
			query.Cursor.UpdatedAt,
			query.Cursor.UpdatedAt,
			query.Cursor.ID,
		)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = constants.ChannelCollectionDefaultPageSize
	}
	err := db.
		Order("updated_at DESC, id DESC").
		Limit(limit + 1).
		Find(&threads).Error
	if err != nil {
		return ChannelThreadListResult{}, databaseError("list_channel_threads", "channel_id="+query.ChannelID, err)
	}
	result := ChannelThreadListResult{Threads: threads}
	if len(threads) > limit {
		result.Threads = threads[:limit]
		last := result.Threads[len(result.Threads)-1]
		result.NextCursor = &ChannelThreadListCursor{UpdatedAt: last.UpdatedAt, ID: last.ID}
	}
	return result, nil
}

// ListByChannelUpdatedRange 按搜索词和最近活动时间范围分页返回团队的已就绪对话。
func (c *channelThreadController) ListByChannelUpdatedRange(
	ctx context.Context,
	channelID string,
	search string,
	updatedAtStart int64,
	updatedAtEnd int64,
	limit int,
	offset int,
) ([]models.ChannelThread, error) {
	threads := []models.ChannelThread{}
	err := filterChannelThreads(
		controllerDB(ctx, c.db).
			Table("channel_threads AS thread").
			Select(channelThreadActivitySelect),
		channelID,
		search,
		updatedAtStart,
		updatedAtEnd,
	).
		Order("thread.updated_at DESC").
		Order("thread.id DESC").
		Limit(limit).
		Offset(offset).
		Find(&threads).Error
	if err != nil {
		return nil, databaseError("list_channel_threads_by_updated_range", "channel_id="+channelID, err)
	}
	return threads, nil
}

// CountByChannelUpdatedRange 返回团队符合搜索词和最近活动时间筛选的已就绪对话总数。
func (c *channelThreadController) CountByChannelUpdatedRange(
	ctx context.Context,
	channelID string,
	search string,
	updatedAtStart int64,
	updatedAtEnd int64,
) (int64, error) {
	var count int64
	err := filterChannelThreads(
		controllerDB(ctx, c.db).Table("channel_threads AS thread"),
		channelID,
		search,
		updatedAtStart,
		updatedAtEnd,
	).Count(&count).Error
	if err != nil {
		return 0, databaseError("count_channel_threads_by_updated_range", "channel_id="+channelID, err)
	}
	return count, nil
}

func filterChannelThreads(
	query *gorm.DB,
	channelID string,
	search string,
	updatedAtStart int64,
	updatedAtEnd int64,
) *gorm.DB {
	query = query.Where("thread.channel_id = ? AND thread.ready_at IS NOT NULL", channelID)
	if normalizedSearch := strings.ToLower(strings.TrimSpace(search)); normalizedSearch != "" {
		pattern := "%" + normalizedSearch + "%"
		query = query.Where(
			"LOWER(thread.title) LIKE ? OR LOWER(thread.project_name) LIKE ? OR LOWER(thread.workspace_directory) LIKE ? OR LOWER(thread.additional_working_directories) LIKE ?",
			pattern,
			pattern,
			pattern,
			pattern,
		)
	}
	if updatedAtStart > 0 {
		query = query.Where("thread.updated_at >= ?", updatedAtStart)
	}
	if updatedAtEnd > 0 {
		query = query.Where("thread.updated_at <= ?", updatedAtEnd)
	}
	return query
}

// ListIDsByChannel 返回团队下包括初始化残留在内的全部对话标识。
func (c *channelThreadController) ListIDsByChannel(
	ctx context.Context,
	channelID string,
) ([]string, error) {
	type threadIDRow struct {
		// ID 是团队对话标识。
		ID string `gorm:"column:id"`
	}
	rows := []threadIDRow{}
	if err := controllerDB(ctx, c.db).Model(&models.ChannelThread{}).
		Select("id").Where("channel_id = ?", channelID).Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, databaseError("list_channel_thread_ids", "channel_id="+channelID, err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// ListMembers 按快照顺序返回团队对话的成员 Chat 映射。
func (c *channelThreadController) ListMembers(
	ctx context.Context,
	threadID string,
) ([]models.ChannelThreadMember, error) {
	members := []models.ChannelThreadMember{}
	err := controllerDB(ctx, c.db).
		Where("thread_id = ?", threadID).
		Order("position ASC, waker_id ASC").
		Find(&members).Error
	if err != nil {
		return nil, databaseError(
			"list_channel_thread_members",
			"thread_id="+threadID,
			err,
		)
	}
	return members, nil
}

// ListChatIDs 返回团队对话拥有的去重 Chat 标识。
func (c *channelThreadController) ListChatIDs(
	ctx context.Context,
	threadID string,
) ([]string, error) {
	thread, err := c.Get(ctx, threadID)
	if err != nil {
		return nil, err
	}
	members, err := c.ListMembers(ctx, threadID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(members)+1)
	ids := make([]string, 0, len(members)+1)
	appendID := func(chatID string) {
		if chatID == "" {
			return
		}
		if _, exists := seen[chatID]; exists {
			return
		}
		seen[chatID] = struct{}{}
		ids = append(ids, chatID)
	}
	appendID(thread.LeadChatID)
	for _, member := range members {
		appendID(member.ChatID)
	}
	return ids, nil
}

// GetMember 按伙伴标识返回团队对话中的成员 Chat 映射。
func (c *channelThreadController) GetMember(
	ctx context.Context,
	threadID string,
	wakerID string,
) (models.ChannelThreadMember, error) {
	var member models.ChannelThreadMember
	err := controllerDB(ctx, c.db).
		Where("thread_id = ? AND waker_id = ?", threadID, wakerID).
		First(&member).Error
	if err == nil {
		return member, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ChannelThreadMember{}, status_error.ChannelMemberNotFound.WithParams(
			map[string]interface{}{"WakerID": wakerID},
		)
	}
	return models.ChannelThreadMember{}, databaseError(
		"get_channel_thread_member",
		"thread_id="+threadID+" waker_id="+wakerID,
		err,
	)
}

// ReserveTaskSlot 原子占用一个尚未达到对话上限的任务名额。
func (c *channelThreadController) ReserveTaskSlot(
	ctx context.Context,
	threadID string,
) (bool, error) {
	result := controllerDB(ctx, c.db).Model(&models.ChannelThread{}).
		Where("id = ? AND ready_at IS NOT NULL AND canceling_at IS NULL AND task_count < max_tasks", threadID).
		UpdateColumn("task_count", gorm.Expr("task_count + ?", 1))
	if result.Error != nil {
		return false, databaseError("reserve_channel_thread_task_slot", "thread_id="+threadID, result.Error)
	}
	return result.RowsAffected == 1, nil
}

// UpdateMemberExecutionConfigs 更新成员后续新执行使用的完整配置。
func (c *channelThreadController) UpdateMemberExecutionConfigs(
	ctx context.Context,
	members []models.ChannelThreadMember,
) error {
	db := controllerDB(ctx, c.db)
	changedThreads := map[string]bool{}
	for _, member := range members {
		result := db.Model(&models.ChannelThreadMember{}).
			Where("thread_id = ? AND waker_id = ?", member.ThreadID, member.WakerID).
			Updates(map[string]interface{}{
				"runtime_id":       member.RuntimeID,
				"account_id":       member.AccountID,
				"model":            member.Model,
				"reasoning":        member.Reasoning,
				"permission_level": member.PermissionLevel,
				"updated_at":       member.UpdatedAt,
			})
		if result.Error != nil {
			return databaseError(
				"update_channel_thread_member_execution_config",
				"thread_id="+member.ThreadID+" waker_id="+member.WakerID,
				result.Error,
			)
		}
		changedThreads[member.ThreadID] = true
	}
	for threadID := range changedThreads {
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("thread", threadID))
	}
	return nil
}

// UpdateProvisioned 保存 staging 发布后由成员 Chat 冻结的工作区信息。
func (c *channelThreadController) UpdateProvisioned(
	ctx context.Context,
	thread models.ChannelThread,
) error {
	result := controllerDB(ctx, c.db).Model(&models.ChannelThread{}).
		Where(
			"id = ? AND provisioning_id = ? AND ready_at IS NULL",
			thread.ID,
			thread.ProvisioningID,
		).
		Select(
			"Title",
			"LeadChatID",
			"ProjectID",
			"ProjectName",
			"WorkspaceDirectory",
			"AdditionalWorkingDirectories",
			"UpdatedAt",
		).
		Updates(&thread)
	if result.Error != nil {
		return databaseError("update_provisioned_channel_thread", "thread_id="+thread.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.ChannelTaskStateConflict
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("thread", thread.ID))
	return nil
}

// MarkReady 标记全部成员 Chat 和团队对话首发事实已经完成。
func (c *channelThreadController) MarkReady(
	ctx context.Context,
	threadID string,
	provisioningID string,
	readyAt int64,
) error {
	result := controllerDB(ctx, c.db).Model(&models.ChannelThread{}).
		Where(
			"id = ? AND provisioning_id = ? AND ready_at IS NULL",
			threadID,
			provisioningID,
		).
		Updates(map[string]interface{}{"ready_at": readyAt, "updated_at": readyAt})
	if result.Error != nil {
		return databaseError("mark_channel_thread_ready", "thread_id="+threadID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.ChannelTaskStateConflict
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("thread", threadID))
	return nil
}

// BeginCanceling 原子写入停止意图并冻结新的任务名额与派发。
func (c *channelThreadController) BeginCanceling(
	ctx context.Context,
	threadID string,
	cancelingAt int64,
) (bool, error) {
	result := controllerDB(ctx, c.db).Model(&models.ChannelThread{}).
		Where("id = ? AND ready_at IS NOT NULL AND canceling_at IS NULL", threadID).
		Updates(map[string]interface{}{"canceling_at": cancelingAt, "updated_at": cancelingAt})
	if result.Error != nil {
		return false, databaseError("begin_channel_thread_canceling", "thread_id="+threadID, result.Error)
	}
	if result.RowsAffected == 1 {
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("thread", threadID))
	}
	return result.RowsAffected == 1, nil
}

// FinishCanceling 在停止收敛事务末尾按原意图清除冻结状态。
func (c *channelThreadController) FinishCanceling(
	ctx context.Context,
	threadID string,
	cancelingAt int64,
	updatedAt int64,
) (bool, error) {
	result := controllerDB(ctx, c.db).Model(&models.ChannelThread{}).
		Where("id = ? AND canceling_at = ?", threadID, cancelingAt).
		Updates(map[string]interface{}{"canceling_at": nil, "updated_at": updatedAt})
	if result.Error != nil {
		return false, databaseError("finish_channel_thread_canceling", "thread_id="+threadID, result.Error)
	}
	if result.RowsAffected == 1 {
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("thread", threadID))
	}
	return result.RowsAffected == 1, nil
}

// ListCanceling 返回服务启动时必须继续收敛的停止意图。
func (c *channelThreadController) ListCanceling(
	ctx context.Context,
) ([]models.ChannelThread, error) {
	threads := []models.ChannelThread{}
	if err := controllerDB(ctx, c.db).
		Where("ready_at IS NOT NULL AND canceling_at IS NOT NULL").
		Order("canceling_at ASC, id ASC").
		Find(&threads).Error; err != nil {
		return nil, databaseError("list_canceling_channel_threads", "channel_thread", err)
	}
	return threads, nil
}

// ListInitializing 返回服务启动时必须回滚的未就绪首发锚点。
func (c *channelThreadController) ListInitializing(
	ctx context.Context,
) ([]models.ChannelThread, error) {
	threads := []models.ChannelThread{}
	if err := controllerDB(ctx, c.db).
		Where("ready_at IS NULL").
		Order("created_at ASC, id ASC").
		Find(&threads).Error; err != nil {
		return nil, databaseError("list_initializing_channel_threads", "channel_thread", err)
	}
	return threads, nil
}

// Touch 更新团队对话最近活动时间。
func (c *channelThreadController) Touch(
	ctx context.Context,
	threadID string,
	updatedAt int64,
) error {
	if err := controllerDB(ctx, c.db).Model(&models.ChannelThread{}).
		Where("id = ?", threadID).
		Update("updated_at", updatedAt).Error; err != nil {
		return databaseError("touch_channel_thread", "thread_id="+threadID, err)
	}
	return nil
}

// DeleteInitializing 删除仍处于首发补偿窗口的团队对话及成员快照。
func (c *channelThreadController) DeleteInitializing(
	ctx context.Context,
	threadID string,
) error {
	db := controllerDB(ctx, c.db)
	var count int64
	if err := db.Model(&models.ChannelThread{}).
		Where("id = ? AND ready_at IS NULL", threadID).
		Count(&count).Error; err != nil {
		return databaseError("check_initializing_channel_thread", "thread_id="+threadID, err)
	}
	if count == 0 {
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("thread", threadID))
		return nil
	}
	return c.deleteThreadFacts(db, threadID, true)
}

// CountActiveWork 统计阻止删除的待执行任务、负责人输入与成员（含负责人）活跃 Chat Execution。
func (c *channelThreadController) CountActiveWork(
	ctx context.Context,
	threadID string,
) (int64, error) {
	db := controllerDB(ctx, c.db)
	var taskCount int64
	if err := db.Model(&models.ChannelTask{}).
		Where("thread_id = ?", threadID).
		Where("status IN ?", []constants.ChannelTaskStatus{
			constants.ChannelTaskStatusPending,
			constants.ChannelTaskStatusInProgress,
		}).Count(&taskCount).Error; err != nil {
		return 0, databaseError("count_channel_thread_active_tasks", "thread_id="+threadID, err)
	}
	var inputCount int64
	if err := db.Model(&models.ChannelLeadInput{}).
		Where("thread_id = ?", threadID).
		Where("status IN ?", []constants.ChannelInputStatus{
			constants.ChannelInputStatusPending,
			constants.ChannelInputStatusDelivering,
		}).Count(&inputCount).Error; err != nil {
		return 0, databaseError("count_channel_thread_active_inputs", "thread_id="+threadID, err)
	}
	chatIDs, err := c.ListChatIDs(ctx, threadID)
	if err != nil {
		return 0, err
	}
	var executionCount int64
	if len(chatIDs) > 0 {
		if err := db.Model(&models.Chat{}).
			Where("id IN ?", chatIDs).
			Where("active_execution_id <> ''").Count(&executionCount).Error; err != nil {
			return 0, databaseError("count_channel_thread_active_executions", "thread_id="+threadID, err)
		}
	}
	return taskCount + inputCount + executionCount, nil
}

// Delete 删除一个团队对话的全部结构化编排事实。
func (c *channelThreadController) Delete(ctx context.Context, threadID string) error {
	return c.deleteThreadFacts(controllerDB(ctx, c.db), threadID, false)
}

// DeleteByChannel 删除一个团队下全部团队对话的结构化编排事实。
func (c *channelThreadController) DeleteByChannel(ctx context.Context, channelID string) error {
	ids, err := c.ListIDsByChannel(ctx, channelID)
	if err != nil {
		return err
	}
	db := controllerDB(ctx, c.db)
	for _, threadID := range ids {
		if err := c.deleteThreadFacts(db, threadID, false); err != nil {
			return err
		}
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("channel", channelID))
	return nil
}

// deleteThreadFacts 按外键依赖顺序删除一个团队对话的全部结构化事实。
func (c *channelThreadController) deleteThreadFacts(
	db *gorm.DB,
	threadID string,
	initializingOnly bool,
) error {
	if err := db.Where("owner_kind = ? AND owner_id = ?", constants.WorkResultOwnerKindTeamThread, threadID).Delete(&models.WorkResultReceipt{}).Error; err != nil {
		return databaseError("delete_thread_work_result", threadID, err)
	}
	taskIDs := db.Model(&models.ChannelTask{}).Select("id").Where("thread_id = ?", threadID)
	if err := db.Where("task_id IN (?) OR depends_on_task_id IN (?)", taskIDs, taskIDs).
		Delete(&models.ChannelTaskDependency{}).Error; err != nil {
		return databaseError("delete_channel_thread_dependencies", "thread_id="+threadID, err)
	}
	if err := db.Where("task_id IN (?)", taskIDs).
		Delete(&models.ChannelTaskAttempt{}).Error; err != nil {
		return databaseError("delete_channel_thread_attempts", "thread_id="+threadID, err)
	}
	if err := db.Where("task_id IN (?)", taskIDs).
		Delete(&models.ChannelTaskFollowUp{}).Error; err != nil {
		return databaseError("delete_channel_thread_follow_ups", "thread_id="+threadID, err)
	}
	if err := db.Where("thread_id = ?", threadID).
		Delete(&models.ChannelLeadNotification{}).Error; err != nil {
		return databaseError("delete_channel_thread_notifications", "thread_id="+threadID, err)
	}
	if err := db.Where("thread_id = ?", threadID).
		Delete(&models.ChannelLeadInput{}).Error; err != nil {
		return databaseError("delete_channel_thread_inputs", "thread_id="+threadID, err)
	}
	if err := db.Where("thread_id = ?", threadID).
		Delete(&models.ChannelTask{}).Error; err != nil {
		return databaseError("delete_channel_thread_tasks", "thread_id="+threadID, err)
	}
	if err := db.Where("thread_id = ?", threadID).
		Delete(&models.ChannelEvent{}).Error; err != nil {
		return databaseError("delete_channel_thread_events", "thread_id="+threadID, err)
	}
	if err := db.Where("thread_id = ?", threadID).
		Delete(&models.ChannelThreadMember{}).Error; err != nil {
		return databaseError("delete_channel_thread_members", "thread_id="+threadID, err)
	}
	query := db.Where("id = ?", threadID)
	if initializingOnly {
		query = query.Where("ready_at IS NULL")
	}
	if err := query.Delete(&models.ChannelThread{}).Error; err != nil {
		return databaseError("delete_channel_thread", "thread_id="+threadID, err)
	}
	return nil
}

// AppendEvent 在当前事务中追加一条团队持久事件。
func (c *channelThreadController) AppendEvent(
	ctx context.Context,
	event models.ChannelEvent,
) (models.ChannelEvent, error) {
	if event.ID == "" || len(event.Payload) == 0 || !json.Valid(event.Payload) {
		return models.ChannelEvent{}, status_error.BadRequest
	}
	if err := controllerDB(ctx, c.db).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&event).Error; err != nil {
		return models.ChannelEvent{}, databaseError(
			"append_channel_event",
			"thread_id="+event.ThreadID,
			err,
		)
	}
	return event, nil
}

// ListEvents 返回指定全局游标之后的团队对话持久事件。
func (c *channelThreadController) ListEvents(
	ctx context.Context,
	threadID string,
	after string,
	limit int,
) ([]models.ChannelEvent, error) {
	events := []models.ChannelEvent{}
	db := controllerDB(ctx, c.db).Where("thread_id = ?", threadID)
	if after != "" {
		var cursor models.ChannelEvent
		if err := controllerDB(ctx, c.db).
			Where("thread_id = ? AND id = ?", threadID, after).
			First(&cursor).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, status_error.BadRequest
			}
			return nil, databaseError("get_channel_event_cursor", "event_id="+after, err)
		}
		db = db.Where(
			"created_at > ? OR (created_at = ? AND id > ?)",
			cursor.CreatedAt,
			cursor.CreatedAt,
			cursor.ID,
		)
	}
	err := db.Order("created_at ASC, id ASC").
		Limit(limit).
		Find(&events).Error
	if err != nil {
		return nil, databaseError("list_channel_events", "thread_id="+threadID, err)
	}
	return events, nil
}

// LatestEventCursor 返回团队对话最新持久事件游标；没有事件时返回空字符串。
func (c *channelThreadController) LatestEventCursor(
	ctx context.Context,
	threadID string,
) (string, error) {
	var event models.ChannelEvent
	err := controllerDB(ctx, c.db).
		Select("id").
		Where("thread_id = ?", threadID).
		Order("created_at DESC, id DESC").
		First(&event).Error
	if err == nil {
		return event.ID, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return "", databaseError("latest_channel_event_cursor", "thread_id="+threadID, err)
}

// CreateLeadInput 持久化一条等待进入负责人 Chat 的完整用户输入。
func (c *channelThreadController) CreateLeadInput(
	ctx context.Context,
	input models.ChannelLeadInput,
) error {
	if input.Payload.Content == "" {
		return status_error.BadRequest
	}
	if err := controllerDB(ctx, c.db).Create(&input).Error; err != nil {
		return databaseError("create_channel_lead_input", "input_id="+input.ID, err)
	}
	return nil
}

// ListLeadInputs 返回指定状态的负责人输入队列，按创建顺序排列。
func (c *channelThreadController) ListLeadInputs(
	ctx context.Context,
	threadID string,
	statuses []constants.ChannelInputStatus,
	limit int,
) ([]models.ChannelLeadInput, error) {
	inputs := []models.ChannelLeadInput{}
	if len(statuses) == 0 || limit <= 0 {
		return inputs, nil
	}
	err := controllerDB(ctx, c.db).
		Where("thread_id = ? AND status IN ?", threadID, statuses).
		Order("created_at ASC, id ASC").
		Limit(limit).
		Find(&inputs).Error
	if err != nil {
		return nil, databaseError("list_channel_lead_inputs", "thread_id="+threadID, err)
	}
	return inputs, nil
}

// UpdateLeadInputs 推进一批负责人输入的投递状态和 Execution 映射。
func (c *channelThreadController) UpdateLeadInputs(
	ctx context.Context,
	ids []string,
	status constants.ChannelInputStatus,
	leadExecutionID string,
	updatedAt int64,
) error {
	if len(ids) == 0 {
		return nil
	}
	db := controllerDB(ctx, c.db).Model(&models.ChannelLeadInput{}).
		Where("id IN ?", ids)
	if status == constants.ChannelInputStatusDelivering {
		db = db.Where("status = ?", constants.ChannelInputStatusPending)
	} else {
		db = db.Where("status = ?", constants.ChannelInputStatusDelivering)
	}
	if err := db.
		Updates(map[string]interface{}{
			"status":            status,
			"lead_execution_id": leadExecutionID,
			"updated_at":        updatedAt,
		}).Error; err != nil {
		return databaseError("update_channel_lead_inputs", "input_count", err)
	}
	return nil
}

// CancelOpenLeadInputs 取消团队对话尚未消费的负责人用户输入。
func (c *channelThreadController) CancelOpenLeadInputs(
	ctx context.Context,
	threadID string,
	updatedAt int64,
) error {
	if err := controllerDB(ctx, c.db).Model(&models.ChannelLeadInput{}).
		Where("thread_id = ? AND status IN ?", threadID, []constants.ChannelInputStatus{
			constants.ChannelInputStatusPending,
			constants.ChannelInputStatusDelivering,
		}).
		Updates(map[string]interface{}{
			"status":            constants.ChannelInputStatusCancelled,
			"lead_execution_id": "",
			"updated_at":        updatedAt,
		}).Error; err != nil {
		return databaseError("cancel_channel_lead_inputs", "thread_id="+threadID, err)
	}
	return nil
}

// RecoverLeadInputs 收尾中断投递并返回仍有待处理输入的团队对话标识。
func (c *channelThreadController) RecoverLeadInputs(
	ctx context.Context,
	updatedAt int64,
) ([]string, error) {
	db := controllerDB(ctx, c.db)
	writableThreadIDs := db.Model(&models.ChannelThread{}).
		Select("channel_threads.id")
	if err := db.Model(&models.ChannelLeadInput{}).
		Where("thread_id IN (?)", writableThreadIDs).
		Where("status = ?", constants.ChannelInputStatusDelivering).
		Updates(map[string]interface{}{
			"status":     constants.ChannelInputStatusConsumed,
			"updated_at": updatedAt,
		}).Error; err != nil {
		return nil, databaseError("recover_delivering_channel_inputs", "channel_input", err)
	}
	threadIDs := []string{}
	if err := db.Model(&models.ChannelLeadInput{}).
		Distinct("thread_id").
		Where("thread_id IN (?)", writableThreadIDs).
		Where("status = ?", constants.ChannelInputStatusPending).
		Order("thread_id ASC").
		Pluck("thread_id", &threadIDs).Error; err != nil {
		return nil, databaseError("list_pending_channel_input_threads", "channel_input", err)
	}
	return threadIDs, nil
}

// FindByChatID 用成员索引定位接纳操作需要保护的团队聚合。
func (c *channelThreadController) FindByChatID(ctx context.Context, chatID string) (models.ChannelThread, bool, error) {
	var thread models.ChannelThread
	err := controllerDB(ctx, c.db).Table("channel_threads AS thread").Select("thread.*").
		Where("thread.id IN (SELECT thread_id FROM channel_thread_members WHERE chat_id = ?)", chatID).Take(&thread).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return thread, false, nil
	}
	if err != nil {
		return thread, false, databaseError("find_channel_thread_by_chat", "chat_id="+chatID, err)
	}
	return thread, true, nil
}

// GetLeadInput 根据稳定输入身份对账团队续发收据。
func (c *channelThreadController) GetLeadInput(ctx context.Context, id string) (models.ChannelLeadInput, bool, error) {
	var value models.ChannelLeadInput
	if err := controllerDB(ctx, c.db).Where("id = ?", id).First(&value).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return value, false, nil
		}
		return value, false, databaseError("get_channel_lead_input", id, err)
	}
	return value, true, nil
}
