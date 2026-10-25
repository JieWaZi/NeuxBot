package controllers

import (
	"context"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// WorkBoardCursor 使用时间、种类和原身份建立全局唯一排序。
type WorkBoardCursor struct {
	// At 是上一页最后一行的排序时间。
	At int64 `json:"at"`
	// Kind 是上一页最后一行的原容器种类。
	Kind constants.WorkResultOwnerKind `json:"kind"`
	// ID 是上一页最后一行的原容器标识。
	ID string `json:"id"`
}

// WorkBoardQuery 描述服务端执行的范围及有界游标窗口。
type WorkBoardQuery struct {
	// ExecutorID 是伙伴、团队或工作流的范围。
	ExecutorID string
	// Source 是可选来源过滤。
	Source string
	// Query 是只搜索标题的文字。
	Query string
	// Activity 是可选当前活动过滤。
	Activity constants.ChannelActivityState
	// States 对当前活动及空闲工作最近终态执行 OR 多选筛选。
	States []string
	// From 是历史活动时间下界，零表示不限。
	From int64
	// To 是历史活动时间上界，零表示不限。
	To int64
	// Cursor 是上一页排序锚点。
	Cursor *WorkBoardCursor
	// Offset 是显式页码跳转跳过的窄身份数量，相邻翻页仍使用 Cursor。
	Offset int
	// Limit 是当前页条数，不用于截断统计。
	Limit int
}

// WorkBoardController 只读取原工作及阅读事实，不维护工作副本。
type WorkBoardController interface {
	// Executors 搜索原配置对象，最多返回五十项，不枚举工作历史。
	Executors(context.Context, string) ([]models.WorkBoardExecutor, error)
	// List 返回各来源过滤后合并的稳定候选窗口。
	List(context.Context, WorkBoardQuery) ([]models.WorkBoardEntry, error)
	// Count 返回当前筛选范围的原工作容器数，不取正文或排序。
	Count(context.Context, WorkBoardQuery) (int64, error)
	// Counts 统计日期范围内的工作容器总数、进行中和已结束数。
	Counts(context.Context, WorkBoardQuery) (models.WorkBoardCounts, error)
	// AttentionOverview 一次遍历返回待办预览、有效请求数与其归属的不同工作数。
	AttentionOverview(context.Context, WorkBoardQuery) ([]models.WorkBoardAttention, int64, int64, error)
	// Page 仅为数字跳页读取有序原身份，再批量补齐这一页，resultMode 区分成果排序。
	Page(context.Context, WorkBoardQuery, bool) ([]models.WorkBoardEntry, error)
	// ResultCount 返回执行对象与来源范围内的未读容器数。
	ResultCount(context.Context, WorkBoardQuery) (int64, error)
	// Attention 返回仍有效的交互窗口及准确总数。
	Attention(context.Context, WorkBoardQuery) ([]models.WorkBoardAttention, int64, error)
	// Results 返回未读部分索引上的有界结果窗口。
	Results(context.Context, WorkBoardQuery) ([]models.WorkBoardEntry, error)
}

// Executors 只查询配置表，名称搜索不关联历史工作。
func (c *workBoardController) Executors(ctx context.Context, query string) ([]models.WorkBoardExecutor, error) {
	db := controllerDB(ctx, c.db)
	base := db.Table("(?) executor", db.Raw(`SELECT id,name,'waker' AS kind FROM wakers UNION ALL SELECT id,name,'team' FROM channels UNION ALL SELECT id,title,'workflow' FROM workflow_definitions`))
	if query != "" {
		base = base.Where("name LIKE ? ESCAPE '\\'", "%"+strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query)+"%")
	}
	var values []models.WorkBoardExecutor
	if err := base.Order("kind,name,id").Limit(50).Find(&values).Error; err != nil {
		return nil, databaseError("list_work_board_executors", "", err)
	}
	return values, nil
}

// workBoardController 将数据查询和方言保留在仓储层。
type workBoardController struct {
	// db 是默认 GORM 连接，事务时从 Context 读取。
	db *gorm.DB
}

// NewWorkBoardController 创建窄查询仓储。
func NewWorkBoardController(db *gorm.DB) WorkBoardController { return &workBoardController{db: db} }

const workBoardChatSelect = `SELECT 'chat' AS owner_kind, chat.id AS owner_id, chat.title,
chat.waker_id AS executor_id, '' AS executor_name,
CASE WHEN EXISTS(SELECT 1 FROM automatic_runs a WHERE a.chat_id=chat.id AND a.thread_id='') THEN 'automatic'
 WHEN EXISTS(SELECT 1 FROM im_conversation_links im WHERE im.chat_id=chat.id AND im.thread_id='') THEN 'im' ELSE 'chat' END AS source,
chat.id AS chat_id, '' AS channel_id, '' AS workflow_id, chat.waker_id,
CASE WHEN chat.active_execution_id<>'' AND (chat.waiting_approval=1 OR chat.waiting_elicitation=1) THEN 'waiting'
 WHEN chat.active_execution_id<>'' THEN 'running' ELSE 'idle' END AS activity,
chat.last_ended_execution_status AS last_status, chat.updated_at,
0 AS approval_count,0 AS input_count,0 AS review_count
FROM chats chat
WHERE NOT EXISTS(SELECT 1 FROM channel_thread_members m WHERE m.chat_id=chat.id)
 AND NOT EXISTS(SELECT 1 FROM workflow_effects e WHERE e.chat_id=chat.id)
 AND NOT EXISTS(SELECT 1 FROM workflow_creation_sessions s WHERE s.chat_id=chat.id)
 AND (chat.hidden_from_waker_history=0
 OR EXISTS(SELECT 1 FROM automatic_runs a WHERE a.chat_id=chat.id AND a.thread_id='')
 OR EXISTS(SELECT 1 FROM im_conversation_links im WHERE im.chat_id=chat.id AND im.thread_id='')
 OR EXISTS(SELECT 1 FROM work_result_receipts receipt WHERE receipt.owner_kind='chat' AND receipt.owner_id=chat.id))`

const workBoardThreadSelect = `SELECT 'team_thread' AS owner_kind, thread.id AS owner_id, thread.title,
thread.channel_id AS executor_id, '' AS executor_name,
CASE WHEN EXISTS(SELECT 1 FROM automatic_runs a WHERE a.thread_id=thread.id) THEN 'automatic'
 WHEN EXISTS(SELECT 1 FROM im_conversation_links im WHERE im.thread_id=thread.id) THEN 'im' ELSE 'team' END AS source,
thread.lead_chat_id AS chat_id, thread.channel_id, '' AS workflow_id, thread.lead_waker_id AS waker_id,
CASE WHEN ` + channelThreadWaitingSQL + ` THEN 'waiting' WHEN ` + channelThreadRunningSQL + ` THEN 'running' ELSE 'idle' END AS activity,
COALESCE((SELECT c.last_ended_execution_status FROM chats c WHERE c.id=thread.lead_chat_id),'') AS last_status, thread.updated_at,
0 AS approval_count,0 AS input_count,0 AS review_count
FROM channel_threads thread WHERE thread.ready_at IS NOT NULL`

const workBoardRunSelect = `SELECT 'workflow_run' AS owner_kind, run.id AS owner_id, (SELECT title FROM workflow_definitions WHERE id=run.definition_id) AS title,
run.definition_id AS executor_id, '' AS executor_name,
CASE WHEN EXISTS(SELECT 1 FROM automatic_runs a WHERE a.workflow_run_id=run.id) THEN 'automatic' ELSE 'workflow' END AS source,
'' AS chat_id, '' AS channel_id, run.definition_id AS workflow_id, '' AS waker_id,
CASE WHEN run.status='waiting' THEN 'waiting' WHEN run.status IN ('queued','running','recovering','cancelling') THEN 'running' ELSE 'idle' END AS activity,
run.status AS last_status, run.updated_at,
0 AS approval_count,0 AS input_count,0 AS review_count
FROM workflow_runs run WHERE run.parent_run_id='' AND run.history_deleted_at IS NULL AND EXISTS(SELECT 1 FROM workflow_definitions WHERE id=run.definition_id)`

// workBoardBranches 只返回每条工作一个来源，自动与 IM 不创建第二层包装行。
func workBoardBranches() []string {
	return []string{workBoardChatSelect, workBoardThreadSelect, workBoardRunSelect}
}

func workBoardScope(db *gorm.DB, query WorkBoardQuery) *gorm.DB {
	if query.ExecutorID != "" {
		db = db.Where("executor_id = ?", query.ExecutorID)
	}
	if query.Source != "" {
		db = db.Where("source = ?", query.Source)
	}
	if query.Query != "" {
		db = db.Where("title LIKE ? ESCAPE '\\'", "%"+strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query.Query)+"%")
	}
	if query.Activity != "" {
		db = db.Where("activity = ?", query.Activity)
	}
	if len(query.States) > 0 {
		predicates := make([]string, 0, len(query.States))
		for _, state := range query.States {
			switch constants.WorkBoardState(state) {
			case constants.WorkBoardStateRunning:
				predicates = append(predicates, "activity = 'running'")
			case constants.WorkBoardStateWaiting:
				predicates = append(predicates, "activity = 'waiting'")
			case constants.WorkBoardStateIdle:
				predicates = append(predicates, "activity = 'idle' AND COALESCE(last_status,'') = ''")
			case constants.WorkBoardStateCompleted:
				predicates = append(predicates, "activity = 'idle' AND last_status IN ('completed','succeeded')")
			case constants.WorkBoardStateFailed:
				predicates = append(predicates, "activity = 'idle' AND last_status = 'failed'")
			case constants.WorkBoardStateCanceled:
				predicates = append(predicates, "activity = 'idle' AND last_status IN ('canceled','cancelled')")
			}
		}
		if len(predicates) > 0 {
			db = db.Where("(" + strings.Join(predicates, ") OR (") + ")")
		}
	}

	if query.From > 0 {
		db = db.Where("updated_at >= ?", query.From)
	}
	if query.To > 0 {
		db = db.Where("updated_at <= ?", query.To)
	}

	return db
}

// Count 只读取覆盖索引上的范围和归属，不为分页加载整页或成果正文。
func (c *workBoardController) Count(ctx context.Context, query WorkBoardQuery) (int64, error) {
	db := controllerDB(ctx, c.db)
	kinds := []constants.WorkResultOwnerKind{constants.WorkResultOwnerKindChat, constants.WorkResultOwnerKindTeamThread, constants.WorkResultOwnerKindWorkflowRun}
	var total int64
	for i, branch := range workBoardBranches() {
		if !boardKindMatchesSource(kinds[i], query.Source) {
			continue
		}
		if query.Source == "automatic" || query.Source == "im" {
			table, column := "automatic_runs", "chat_id"
			if query.Source == "im" {
				table = "im_conversation_links"
			}
			if kinds[i] == constants.WorkResultOwnerKindTeamThread {
				column = "thread_id"
			}
			if kinds[i] == constants.WorkResultOwnerKindWorkflowRun {
				column = "workflow_run_id"
			}
			var exists int64
			if err := db.Raw("SELECT EXISTS(SELECT 1 FROM " + table + " WHERE " + column + " > '')").Find(&exists).Error; err != nil {
				return 0, databaseError("probe_count_source", "", err)
			}
			if exists == 0 {
				continue
			}
		}
		if kinds[i] == constants.WorkResultOwnerKindChat && (query.Activity == constants.ChannelActivityStateRunning || (len(query.States) == 1 && query.States[0] == string(constants.WorkBoardStateRunning))) {
			var exists int64
			if err := db.Raw("SELECT EXISTS(SELECT 1 FROM chats WHERE active_execution_id > '')").Find(&exists).Error; err != nil {
				return 0, databaseError("probe_running_count", "", err)
			}
			if exists == 0 {
				continue
			}
		}
		var count int64
		base := workBoardScope(db.Table("(?) board", db.Raw(boardCountBranch(boardNativeCandidates(branch, kinds[i], query), kinds[i], query))), query)
		if err := base.Count(&count).Error; err != nil {
			return 0, databaseError("count_work_board", "", err)
		}
		total += count
	}
	return total, nil
}

// Counts 让进行中和终态从正向状态集合计数，避免对全部历史反复计算活动状态。
func (c *workBoardController) Counts(ctx context.Context, query WorkBoardQuery) (models.WorkBoardCounts, error) {
	var total models.WorkBoardCounts
	var err error
	total.Total, err = c.Count(ctx, query)
	if err != nil {
		return total, err
	}
	running := query
	running.Activity = constants.ChannelActivityStateRunning
	total.Running, err = c.Count(ctx, running)
	if err != nil {
		return total, err
	}
	ended := query
	ended.States = []string{string(constants.WorkBoardStateCompleted), string(constants.WorkBoardStateFailed), string(constants.WorkBoardStateCanceled)}
	total.Ended, err = c.Count(ctx, ended)
	return total, err
}

// List 在每个来源先筛选并 seek，再合并有界候选；不查询历史总数。
func (c *workBoardController) List(ctx context.Context, query WorkBoardQuery) ([]models.WorkBoardEntry, error) {
	values := make([]models.WorkBoardEntry, 0, 3*(query.Limit+1))
	kinds := []constants.WorkResultOwnerKind{constants.WorkResultOwnerKindChat, constants.WorkResultOwnerKindTeamThread, constants.WorkResultOwnerKindWorkflowRun}
	for i, branch := range workBoardBranches() {
		if !boardKindMatchesSource(kinds[i], query.Source) {
			continue
		}
		branch = boardNativeCandidates(branch, kinds[i], query)
		page, err := c.listBranchPage(ctx, query, branch, kinds[i])
		if err != nil {
			return nil, err
		}
		values = append(values, page...)
	}
	sort.Slice(values, func(i, j int) bool {
		a, b := values[i], values[j]
		if a.UpdatedAt != b.UpdatedAt {
			return a.UpdatedAt > b.UpdatedAt
		}
		if a.OwnerKind != b.OwnerKind {
			return a.OwnerKind > b.OwnerKind
		}
		return a.OwnerID > b.OwnerID
	})
	if len(values) > query.Limit+1 {
		values = values[:query.Limit+1]
	}
	if err := c.attachAttentionCounts(ctx, values); err != nil {
		return nil, err
	}
	if err := c.attachExecutorNames(ctx, values); err != nil {
		return nil, err
	}
	if err := c.attachResults(ctx, values); err != nil {
		return nil, err
	}
	return values, nil
}

// attachResults 用一条查询补齐当前窗口的阅读引用。
func (c *workBoardController) attachResults(ctx context.Context, values []models.WorkBoardEntry) error {
	if len(values) == 0 {
		return nil
	}
	keys := make([][]any, 0, len(values))
	for _, v := range values {
		keys = append(keys, []any{v.OwnerKind, v.OwnerID})
	}
	var receipts []models.WorkResultReceipt
	if err := controllerDB(ctx, c.db).Where("(owner_kind,owner_id) IN ?", keys).Find(&receipts).Error; err != nil {
		return databaseError("attach_work_results", "", err)
	}
	byID := make(map[string]*models.WorkResultReceipt, len(receipts))
	for i := range receipts {
		r := &receipts[i]
		byID[string(r.OwnerKind)+":"+r.OwnerID] = r
	}
	for i := range values {
		values[i].Result = byID[string(values[i].OwnerKind)+":"+values[i].OwnerID]
	}
	return nil
}

// resultScope 只使用执行对象、来源及标题；待查看不受历史日期限制。
func resultScope(db *gorm.DB, query WorkBoardQuery) *gorm.DB {
	if query.ExecutorID != "" {
		db = db.Where("board.executor_id = ?", query.ExecutorID)
	}
	if query.Source != "" {
		db = db.Where("board.source = ?", query.Source)
	}
	if query.Query != "" {
		db = db.Where("board.title LIKE ? ESCAPE '\\'", "%"+strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query.Query)+"%")
	}
	return db
}

// resultBranch 从未读部分索引读取身份与时间；原容器只作存在性过滤，保留索引排序。
func (c *workBoardController) resultBranch(ctx context.Context, query WorkBoardQuery, branch string, kind constants.WorkResultOwnerKind) *gorm.DB {
	db := controllerDB(ctx, c.db)
	owners := resultScope(db.Table("(?) board", boardReadBranch(db, boardNativeCandidates(branch, kind, WorkBoardQuery{Source: query.Source}), kind, query.Query)), query).Where("board.owner_id=receipt.owner_id").Select("1")
	receipts := boardReceiptSource(db.Table("work_result_receipts receipt").Where("receipt.owner_kind=? AND receipt.latest_version>receipt.viewed_version", kind), kind, query.Source).Where("EXISTS (?)", owners)
	return db.Table("(?) board", receipts.Select("receipt.owner_kind,receipt.owner_id,receipt.available_at AS updated_at,receipt.available_at AS result_available_at,receipt.latest_version AS result_version"))
}

// attachWorkEntries 在身份窗口确定后按种类批量读取详情，保留成果时间与原窗口顺序。
func (c *workBoardController) attachWorkEntries(ctx context.Context, keys []models.WorkBoardEntry) ([]models.WorkBoardEntry, error) {
	db := controllerDB(ctx, c.db)
	kinds := []constants.WorkResultOwnerKind{constants.WorkResultOwnerKindChat, constants.WorkResultOwnerKindTeamThread, constants.WorkResultOwnerKindWorkflowRun}
	byID := map[string]models.WorkBoardEntry{}
	for i, branch := range workBoardBranches() {
		ids := []string{}
		for _, key := range keys {
			if key.OwnerKind == kinds[i] {
				ids = append(ids, key.OwnerID)
			}
		}
		for start := 0; start < len(ids); start += repositoryReadBatch {
			var rows []models.WorkBoardEntry
			if err := db.Table("(?) board", db.Raw(branch)).Where("board.owner_id IN ?", ids[start:min(start+repositoryReadBatch, len(ids))]).Find(&rows).Error; err != nil {
				return nil, databaseError("read_work_board_page", "", err)
			}
			for _, row := range rows {
				byID[string(row.OwnerKind)+":"+row.OwnerID] = row
			}
		}
	}
	values := make([]models.WorkBoardEntry, 0, len(keys))
	for _, key := range keys {
		if row, ok := byID[string(key.OwnerKind)+":"+key.OwnerID]; ok {
			row.UpdatedAt = key.UpdatedAt
			values = append(values, row)
		}
	}
	return values, nil
}

// Results 分来源从未读部分索引 seek，再合并三个有界窗口。
func (c *workBoardController) Results(ctx context.Context, query WorkBoardQuery) ([]models.WorkBoardEntry, error) {
	values := make([]models.WorkBoardEntry, 0, 3*(query.Limit+1))
	kinds := []constants.WorkResultOwnerKind{constants.WorkResultOwnerKindChat, constants.WorkResultOwnerKindTeamThread, constants.WorkResultOwnerKindWorkflowRun}
	for i, branch := range workBoardBranches() {
		if !boardKindMatchesSource(kinds[i], query.Source) {
			continue
		}
		page, err := c.resultBranchPage(ctx, query, branch, kinds[i])
		if err != nil {
			return nil, err
		}
		values = append(values, page...)
	}
	sortBoardEntries(values)
	if len(values) > query.Limit+1 {
		values = values[:query.Limit+1]
	}
	if err := c.attachAttentionCounts(ctx, values); err != nil {
		return nil, err
	}
	if err := c.attachExecutorNames(ctx, values); err != nil {
		return nil, err
	}
	if err := c.attachResults(ctx, values); err != nil {
		return nil, err
	}
	return values, nil
}

// sortBoardEntries 为跨来源页使用统一时间、种类和身份排序。
func sortBoardEntries(values []models.WorkBoardEntry) {
	sort.Slice(values, func(i, j int) bool {
		a, b := values[i], values[j]
		if a.UpdatedAt != b.UpdatedAt {
			return a.UpdatedAt > b.UpdatedAt
		}
		if a.OwnerKind != b.OwnerKind {
			return a.OwnerKind > b.OwnerKind
		}
		return a.OwnerID > b.OwnerID
	})
}

// ResultCount 与成果分页使用相同的原容器可见性和范围谓词，计数不补齐展示字段。
func (c *workBoardController) ResultCount(ctx context.Context, query WorkBoardQuery) (int64, error) {
	kinds := []constants.WorkResultOwnerKind{constants.WorkResultOwnerKindChat, constants.WorkResultOwnerKindTeamThread, constants.WorkResultOwnerKindWorkflowRun}
	var total int64
	for i, branch := range workBoardBranches() {
		if !boardKindMatchesSource(kinds[i], query.Source) {
			continue
		}
		var count int64
		if err := c.resultBranch(ctx, query, branch, kinds[i]).Count(&count).Error; err != nil {
			return 0, databaseError("count_work_results", "", err)
		}
		total += count
	}
	return total, nil
}
