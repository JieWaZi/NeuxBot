package controllers

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// notificationAction 直接保留真实请求身份，验收通知额外绑定当前围栏。
func notificationAction(f boardAttentionFact) models.NotificationItem {
	a := f.Attention
	request := strings.TrimPrefix(a.ID, a.Kind+":")
	if a.ChatID != "" {
		request = strings.TrimPrefix(request, a.ChatID+":")
	}
	id := a.ID
	switch {
	case a.Kind == "review":
		id += ":" + strconv.FormatInt(f.Fence, 10)
	case a.ChatID != "" && a.Kind == "approval":
		id = "approval:" + request
	case a.ChatID != "":
		id = "elicitation:" + request
	case a.Kind == "approval":
		id = "operation:" + request
	default:
		id = "human:" + request
	}
	executorKind := "waker"
	if a.OwnerKind == constants.WorkResultOwnerKindTeamThread {
		executorKind = "team"
	}
	if a.WorkflowID != "" {
		executorKind = "workflow"
	}
	target := models.NotificationTarget{Kind: constants.NotificationKind(a.Kind), ChatID: a.ChatID, WakerID: a.WakerID, ChannelID: a.ChannelID, TaskID: a.TaskID, Fence: f.Fence, DefinitionID: a.WorkflowID, RunID: a.RunID, RequestID: request, OwnerKind: a.OwnerKind, OwnerID: a.OwnerID}
	if a.OwnerKind == constants.WorkResultOwnerKindTeamThread {
		target.ThreadID = a.OwnerID
	}
	return models.NotificationItem{ID: id, Kind: constants.NotificationKind(a.Kind), Bucket: "action", Source: a.Source, Title: a.Title, Description: a.WorkTitle, CreatedAt: a.CreatedAt, Version: max(f.Fence, 1), Executor: models.WorkBoardExecutor{ID: a.ExecutorID, Kind: executorKind}, Target: target}
}

// notificationFailureRow 只承载失败通知的窄定位信息，不读取执行配置正文。
type notificationFailureRow struct {
	// AutomaticRun 保留原运行身份和时间字段。
	models.AutomaticRun
	// FrozenName 是冻结名称，缺失时回退到任务名称。
	FrozenName *string
}

// failureBase 选择每个未删除任务当前或最新失败，范围判断使用索引存在性条件。
func (c *notificationController) failureBase(ctx context.Context, q NotificationQuery, taskIDs []string) *gorm.DB {
	db := controllerDB(ctx, c.db)
	current := db.Table("automatic_tasks task").Select("CASE WHEN active_run_id<>'' THEN active_run_id ELSE (SELECT id FROM automatic_runs WHERE task_id=task.id ORDER BY created_at DESC,id DESC LIMIT 1) END").Where("deleting=0")
	if taskIDs != nil {
		current = current.Where("task.id IN ?", taskIDs)
	}
	query := db.Model(&models.AutomaticRun{}).Where("id IN (?)", current).Where("admission='failed' OR final_state='failed'").
		Where("NOT EXISTS(SELECT 1 FROM attention_notice_acks WHERE notice_id='automatic-run-failed:'||automatic_runs.id AND viewed_at IS NOT NULL)").
		Where("NOT EXISTS(SELECT 1 FROM work_result_receipts WHERE owner_kind='chat' AND owner_id=automatic_runs.chat_id AND automatic_runs.thread_id='' AND automatic_runs.workflow_run_id='')").
		Where("NOT EXISTS(SELECT 1 FROM work_result_receipts WHERE owner_kind='team_thread' AND owner_id=automatic_runs.thread_id AND automatic_runs.thread_id<>'')").
		Where("NOT EXISTS(SELECT 1 FROM work_result_receipts WHERE owner_kind='workflow_run' AND owner_id=automatic_runs.workflow_run_id AND automatic_runs.workflow_run_id<>'')")
	if q.Source != "" && q.Source != "automatic" {
		return query.Where("1=0")
	}
	if q.ExecutorID != "" {
		targets := db.Model(&models.AutomaticTask{}).Select("1").Where("id=automatic_runs.task_id").Where("workflow_id=? OR (workflow_id='' AND channel_id=?) OR (workflow_id='' AND channel_id='' AND automatic_runs.waker_id=?)", q.ExecutorID, q.ExecutorID, q.ExecutorID)
		query = query.Where("EXISTS (?)", targets)
	}
	if q.Query != "" {
		names := db.Model(&models.AutomaticTask{}).Select("id").Where("name LIKE ? ESCAPE '\\'", boardTitlePattern(q.Query))
		query = query.Where("json_extract(configuration,'$.name') LIKE ? ESCAPE '\\' OR (json_extract(configuration,'$.name') IS NULL AND task_id IN (?))", boardTitlePattern(q.Query), names)
	}
	if q.NoticeID != "" {
		query = query.Where("'automatic-run-failed:'||id=?", q.NoticeID)
	}
	return query
}

// failurePage 只为当前页面的失败身份补齐任务名称和目标，不枚举失败全集。
func (c *notificationController) failurePage(ctx context.Context, ids []string) ([]models.NotificationItem, error) {
	db := controllerDB(ctx, c.db)
	runs, err := readRelatedRows[notificationFailureRow](db.Model(&models.AutomaticRun{}), "id", "id,task_id,waker_id,chat_id,thread_id,workflow_run_id,updated_at,json_extract(configuration,'$.name') AS frozen_name", ids)
	if err != nil {
		return nil, err
	}
	ids = make([]string, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.TaskID)
	}
	tasks, err := readRelatedRows[models.AutomaticTask](db, "id", "id,name,channel_id,workflow_id", ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]models.AutomaticTask{}
	for _, task := range tasks {
		byID[task.ID] = task
	}
	items := make([]models.NotificationItem, 0, len(runs))
	for _, r := range runs {
		task, ok := byID[r.TaskID]
		if !ok {
			continue
		}
		title := task.Name
		if r.FrozenName != nil {
			title = *r.FrozenName
		}
		executor := models.WorkBoardExecutor{ID: r.WakerID, Kind: "waker"}
		if task.ChannelID != "" {
			executor.ID = task.ChannelID
			executor.Kind = "team"
		}
		if task.WorkflowID != "" {
			executor.ID = task.WorkflowID
			executor.Kind = "workflow"
		}
		items = append(items, models.NotificationItem{ID: "automatic-run-failed:" + r.ID, Kind: constants.NotificationKindAutomaticFailure, Bucket: "unread", Source: "automatic", Title: title, CreatedAt: r.UpdatedAt, Version: 1, Executor: executor, Failed: true, Target: models.NotificationTarget{Kind: constants.NotificationKindAutomaticFailure, ChatID: r.ChatID, WakerID: r.WakerID, ChannelID: task.ChannelID, ThreadID: r.ThreadID, DefinitionID: task.WorkflowID, RunID: r.WorkflowRunID, TaskID: task.ID, AutomaticRunID: r.ID}})
	}
	return items, nil
}

func notificationMatches(item models.NotificationItem, q NotificationQuery) bool {
	return (q.ExecutorID == "" || item.Executor.ID == q.ExecutorID) && (q.Source == "" || item.Source == q.Source) && (q.NoticeID == "" || item.ID == q.NoticeID) && (q.Query == "" || boardContains(item.Title, q.Query) || boardContains(item.Description, q.Query))
}

func notificationAfter(item models.NotificationItem, q NotificationQuery) bool {
	if q.CursorID == "" {
		return true
	}
	if q.Bucket == "unread" {
		return item.CreatedAt < q.CursorAt || (item.CreatedAt == q.CursorAt && item.ID < q.CursorID)
	}
	return item.CreatedAt > q.CursorAt || (item.CreatedAt == q.CursorAt && item.ID > q.CursorID)
}

// unreadPage 合并各来源的窄身份后统一在 SQL 分页；计数与候选使用同一条件。
func (c *notificationController) unreadPage(ctx context.Context, q NotificationQuery) ([]models.NotificationItem, int64, int64, error) {
	db := controllerDB(ctx, c.db)
	board := &workBoardController{db: c.db}
	parts := []string{}
	args := []any{}
	var total, failures int64
	kinds := []constants.WorkResultOwnerKind{constants.WorkResultOwnerKindChat, constants.WorkResultOwnerKindTeamThread, constants.WorkResultOwnerKindWorkflowRun}
	for i, branch := range workBoardBranches() {
		kind := kinds[i]
		if !boardKindMatchesSource(kind, q.Source) {
			continue
		}
		base := board.resultBranch(ctx, WorkBoardQuery{ExecutorID: q.ExecutorID, Source: q.Source, Query: q.Query}, branch, kind)
		identity := "'result:'||board.owner_kind||':'||board.owner_id||':'||board.result_version"
		if q.NoticeID != "" {
			base = base.Where(identity+"=?", q.NoticeID)
		}
		var count int64
		if err := base.Count(&count).Error; err != nil {
			return nil, 0, 0, databaseError("count_notification_results", "", err)
		}
		total += count
		if count == 0 {
			continue
		}
		if q.CursorID != "" {
			base = base.Where("(board.result_available_at,"+identity+") < (?,?)", q.CursorAt, q.CursorID)
		}
		parts = append(parts, "SELECT * FROM (?)")
		args = append(args, base.Select("board.owner_kind,board.owner_id,board.result_available_at AS updated_at,"+identity+" AS notice_id"))
	}
	failed := c.failureBase(ctx, q, nil)
	if err := failed.Count(&failures).Error; err != nil {
		return nil, 0, 0, databaseError("count_notification_failures", "", err)
	}
	total += failures
	if failures > 0 {
		if q.CursorID != "" {
			failed = failed.Where("(updated_at,'automatic-run-failed:'||id) < (?,?)", q.CursorAt, q.CursorID)
		}
		parts = append(parts, "SELECT * FROM (?)")
		args = append(args, failed.Select("'' AS owner_kind,id AS owner_id,updated_at,'automatic-run-failed:'||id AS notice_id"))
	}
	items := []models.NotificationItem{}
	if len(parts) == 0 {
		return items, total, failures, nil
	}
	var keys []models.WorkBoardEntry
	if err := db.Table("(?) candidates", db.Raw(strings.Join(parts, " UNION ALL "), args...)).Order("updated_at DESC,notice_id DESC").Offset(q.Offset).Limit(q.Limit + 1).Find(&keys).Error; err != nil {
		return nil, 0, 0, databaseError("page_notification_keys", "", err)
	}
	entries, err := board.attachWorkEntries(ctx, keys)
	if err != nil {
		return nil, 0, 0, err
	}
	if err := board.attachResults(ctx, entries); err != nil {
		return nil, 0, 0, err
	}
	byOwner := map[string]models.NotificationItem{}
	for _, e := range entries {
		kind := e.OwnerKind
		r := e.Result
		if r == nil {
			continue
		}
		executorKind := "waker"
		if kind == constants.WorkResultOwnerKindTeamThread {
			executorKind = "team"
		}
		if kind == constants.WorkResultOwnerKindWorkflowRun {
			executorKind = "workflow"
		}
		target := models.NotificationTarget{Kind: constants.NotificationKindResult, ChatID: e.ChatID, WakerID: e.WakerID, ChannelID: e.ChannelID, DefinitionID: e.WorkflowID, OwnerKind: kind, OwnerID: e.OwnerID, ResultVersion: r.LatestVersion, MessageID: r.Reference.MessageID, Reference: &r.Reference}
		if kind == constants.WorkResultOwnerKindTeamThread {
			target.ThreadID = e.OwnerID
		}
		if kind == constants.WorkResultOwnerKindWorkflowRun {
			target.RunID = e.OwnerID
		}
		byOwner[string(kind)+":"+e.OwnerID] = models.NotificationItem{ID: "result:" + string(kind) + ":" + e.OwnerID + ":" + strconv.FormatInt(r.LatestVersion, 10), Kind: constants.NotificationKindResult, Bucket: "unread", Source: e.Source, Title: e.Title, CreatedAt: r.AvailableAt, Version: r.LatestVersion, Executor: models.WorkBoardExecutor{ID: e.ExecutorID, Kind: executorKind}, Target: target, Failed: e.LastStatus == "failed"}
	}
	failureIDs := []string{}
	for _, key := range keys {
		if key.OwnerKind == "" {
			failureIDs = append(failureIDs, key.OwnerID)
		}
	}
	failureItems, err := c.failurePage(ctx, failureIDs)
	if err != nil {
		return nil, 0, 0, err
	}
	for _, item := range failureItems {
		byOwner[":"+item.Target.AutomaticRunID] = item
	}
	for _, key := range keys {
		if item, ok := byOwner[string(key.OwnerKind)+":"+key.OwnerID]; ok {
			items = append(items, item)
		}
	}
	return items, total, failures, nil
}

// readPage 一次读取生成准确计数和当前页；期限使用与待办有效性相同的观察时间。
func (c *notificationController) readPage(ctx context.Context, q NotificationQuery, at int64) ([]models.NotificationItem, int64, int64, int64, error) {
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 50
	}
	q.Offset = max(q.Offset, 0)
	items := []models.NotificationItem{}
	var total, failures, deadline int64
	if q.Bucket == "action" {
		window := newRepositoryPage[boardAttentionPageKey](q.Offset, q.Limit+1, func(a, b boardAttentionPageKey) bool {
			if a.CreatedAt != b.CreatedAt {
				return a.CreatedAt < b.CreatedAt
			}
			return a.OrderID < b.OrderID
		})
		err := (&workBoardController{db: c.db}).walkAttention(ctx, at, nil, func(f boardAttentionFact) error {
			item := notificationAction(f)
			if !notificationMatches(item, q) {
				return nil
			}
			total++
			if notificationAfter(item, q) {
				window.add(attentionPageKey(f.Attention, item.ID))
			}
			if f.Deadline > at && (deadline == 0 || f.Deadline < deadline) {
				deadline = f.Deadline
			}
			return nil
		})
		if err != nil {
			return nil, 0, 0, 0, err
		}
		facts, err := (&workBoardController{db: c.db}).attentionPageFacts(ctx, at, window.page())
		if err != nil {
			return nil, 0, 0, 0, err
		}
		for _, fact := range facts {
			if fact.Attention.ID != "" {
				items = append(items, notificationAction(fact))
			}
		}
	} else {
		var err error
		items, total, failures, err = c.unreadPage(ctx, q)
		if err != nil {
			return nil, 0, 0, 0, err
		}
	}
	ids, executors := []string{}, []string{}
	for _, item := range items {
		ids = append(ids, item.ID)
		executors = append(executors, item.Executor.ID)
	}
	db := controllerDB(ctx, c.db)
	acks, err := readRelatedRows[models.AttentionNoticeAck](db, "notice_id", "notice_id,dismissed_at", ids)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	dismissed := map[string]bool{}
	for _, ack := range acks {
		dismissed[ack.NoticeID] = ack.DismissedAt > 0
	}
	names, err := boardExecutorNames(db, executors)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	for i := range items {
		item := &items[i]
		item.Dismissed = dismissed[item.ID]
		item.Executor.Name = notificationText(names[item.Executor.ID], 200)
		item.Title = notificationText(item.Title, 200)
		item.Description = notificationText(item.Description, 1000)
	}
	return items, total, failures, deadline, nil
}

// List 在同一短读事务内取得计数、分页与当前版本引用。
func (c *notificationController) List(ctx context.Context, q NotificationQuery) ([]models.NotificationItem, int64, error) {
	var items []models.NotificationItem
	var total int64
	err := NewTransactionController(dbhelper.NewGormTransactionManager(c.db)).Transaction(ctx, func(ctx context.Context) error {
		var err error
		items, total, _, _, err = c.readPage(ctx, q, time.Now().UnixMilli())
		return err
	})
	return items, total, err
}

// Snapshot 复用两次分桶读取，同时产出计数和期限，不重复执行展示查询。
func (c *notificationController) Snapshot(ctx context.Context) (models.NotificationFacts, error) {
	value := models.NotificationFacts{ActionPreview: []models.NotificationItem{}, UnreadPreview: []models.NotificationItem{}}
	err := NewTransactionController(dbhelper.NewGormTransactionManager(c.db)).Transaction(ctx, func(ctx context.Context) error {
		at := time.Now().UnixMilli()
		var err error
		var unread int64
		value.ActionPreview, value.ActionCount, _, value.NextDeadline, err = c.readPage(ctx, NotificationQuery{Bucket: "action", Limit: 5}, at)
		if err != nil {
			return err
		}
		value.UnreadPreview, unread, value.UnreadFailureCount, _, err = c.readPage(ctx, NotificationQuery{Bucket: "unread", Limit: 5}, at)
		value.UnreadResultCount = unread - value.UnreadFailureCount
		return err
	})
	value.ActionPreview = value.ActionPreview[:min(5, len(value.ActionPreview))]
	value.UnreadPreview = value.UnreadPreview[:min(5, len(value.UnreadPreview))]
	return value, err
}
