package controllers

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

const unreadBoardChat = "latest_readable_sequence > through_sequence"

func boardChatScope(db *gorm.DB, branch, reader string, q WorkBoardQuery) *gorm.DB {
	base := workBoardScope(db.Table("(?) board", db.Raw(branch)), q)
	if len(q.IsolatedChats) > 0 {
		ids, _ := json.Marshal(q.IsolatedChats)
		base = base.Where("chat_id NOT IN (SELECT value FROM json_each(?))", string(ids))
	}
	return db.Table("(?) reading", base.Select("board.*, (SELECT through_sequence FROM chat_read_cursors WHERE reader_id=? AND chat_id=board.chat_id) AS through_sequence", reader))
}

func boardUnreadRunScope(db *gorm.DB, branch, reader string, kind constants.RunOwnerKind, q WorkBoardQuery) *gorm.DB {
	return workBoardScope(db.Table("(?) board", db.Raw(branch)), q).
		Where("NOT EXISTS (SELECT 1 FROM run_view_marks WHERE reader_id=? AND owner_kind=? AND owner_id=board.owner_id)", reader, kind)
}

const workBoardViewableRunSelect = workBoardRunSelect + " AND " + workflowRunResultAvailableSQL

func boardUnreadRuns(db *gorm.DB, reader string, q WorkBoardQuery) []*gorm.DB {
	return []*gorm.DB{
		boardUnreadRunScope(db, workBoardViewableRunSelect, reader, constants.RunOwnerKindWorkflowRun, q),
		boardUnreadRunScope(db, workBoardAutomaticSelect, reader, constants.RunOwnerKindAutomaticRun, q),
	}
}

func boardUnreadScopes(db *gorm.DB, reader string, q WorkBoardQuery) []*gorm.DB {
	return append([]*gorm.DB{
		boardChatScope(db, workBoardChatSelect, reader, q).Where(unreadBoardChat),
		boardChatScope(db, workBoardThreadSelect, reader, q).Where(unreadBoardChat),
	}, boardUnreadRuns(db, reader, q)...)
}

// MissingReading 检查范围内是否存在尚无阅读基线的公开 Chat。
func (c *workBoardController) MissingReading(ctx context.Context, reader string, q WorkBoardQuery) (bool, error) {
	db := controllerDB(ctx, c.db)
	for _, branch := range []string{workBoardChatSelect, workBoardThreadSelect} {
		var missing bool
		if err := db.Raw("SELECT EXISTS(?)", boardChatScope(db, branch, reader, q).Select("1").Where("through_sequence IS NULL")).Scan(&missing).Error; err != nil {
			return false, databaseError("check_board_reading_baseline", "", err)
		}
		if missing {
			return true, nil
		}
	}
	return false, nil
}

// ReadingChatIDs 只在投影健康异常时定位受到影响的原会话范围。
func (c *workBoardController) ReadingChatIDs(ctx context.Context, q WorkBoardQuery) ([]string, error) {
	db := controllerDB(ctx, c.db)
	ids := []string{}
	for _, branch := range []string{workBoardChatSelect, workBoardThreadSelect} {
		var rows []string
		if err := workBoardScope(db.Table("(?) board", db.Raw(branch)), q).Pluck("chat_id", &rows).Error; err != nil {
			return nil, databaseError("list_board_reading_scope", "", err)
		}
		ids = append(ids, rows...)
	}
	return ids, nil
}

// HasAttention 只定位第一份原待办或未读，不加载消息正文、边界或展示列表。
func (c *workBoardController) HasAttention(ctx context.Context, reader string, q WorkBoardQuery) (models.WorkspaceAttentionSnapshot, error) {
	db := controllerDB(ctx, c.db)
	var value models.WorkspaceAttentionSnapshot
	missing, err := c.MissingReading(ctx, reader, q)
	if err != nil {
		return value, err
	}
	if missing {
		value.IncompleteReason = "isolated"
	}
	for _, source := range boardUnreadScopes(db, reader, q) {
		if err := db.Raw("SELECT EXISTS(?)", source.Select("1")).Scan(&value.HasUnread).Error; err != nil {
			return value, databaseError("check_board_unread", "", err)
		}
		if value.HasUnread {
			break
		}
	}
	err = c.walkAttention(ctx, time.Now().UnixMilli(), boardAttentionScope{}, false, func(a models.WorkBoardAttention) error {
		if boardAttentionMatches(a, q) {
			value.HasAction = true
			return io.EOF
		}
		return nil
	})
	if err == io.EOF {
		err = nil
	}
	return value, err
}

// ReadingMetrics 在 SQLite 统计指标，跳过已读会话的消息范围计数。
func (c *workBoardController) ReadingMetrics(ctx context.Context, reader string, q WorkBoardQuery) (models.AttentionMetrics, bool, error) {
	db := controllerDB(ctx, c.db)
	var value models.AttentionMetrics
	missing := false
	for _, branch := range []string{workBoardChatSelect, workBoardThreadSelect} {
		var row struct {
			// UnreadChatCount 是范围内有未读消息的公开会话数。
			UnreadChatCount int64
			// UnreadMessageCount 是公开会话游标之后的合格消息数。
			UnreadMessageCount int64
			// RunningChatCount 是正在运行的公开会话数。
			RunningChatCount int64
			// Missing 表示范围内有公开会话缺少阅读基线。
			Missing bool
		}
		columns := `COALESCE(SUM(` + unreadBoardChat + `),0) AS unread_chat_count,
COALESCE(SUM(CASE WHEN ` + unreadBoardChat + ` THEN (SELECT COUNT(*) FROM chat_readable_messages WHERE chat_id=reading.chat_id AND order_sequence>reading.through_sequence) ELSE 0 END),0) AS unread_message_count,
COALESCE(SUM(activity='running'),0) AS running_chat_count,
COALESCE(MAX(through_sequence IS NULL),0) AS missing`
		if err := boardChatScope(db, branch, reader, q).Select(columns).Scan(&row).Error; err != nil {
			return value, missing, databaseError("count_board_reading", "", err)
		}
		value.UnreadChatCount += row.UnreadChatCount
		value.UnreadMessageCount += row.UnreadMessageCount
		value.RunningChatCount += row.RunningChatCount
		missing = missing || row.Missing
	}
	value.AwaitingViewWorkCount = value.UnreadChatCount
	for _, source := range boardUnreadRuns(db, reader, q) {
		var count int64
		if err := source.Count(&count).Error; err != nil {
			return value, missing, databaseError("count_board_unviewed_runs", "", err)
		}
		value.AwaitingViewWorkCount += count
	}
	return value, missing, nil
}

// AwaitingView 先选择跨来源的窄身份窗口，再补齐本页展示信息。
func (c *workBoardController) AwaitingView(ctx context.Context, reader string, q WorkBoardQuery) ([]models.WorkBoardEntry, error) {
	db := controllerDB(ctx, c.db)
	sources := boardUnreadScopes(db, reader, q)
	kinds := []constants.WorkOwnerKind{constants.WorkOwnerKindChat, constants.WorkOwnerKindTeamThread, constants.WorkOwnerKindWorkflowRun, constants.WorkOwnerKindAutomaticRun}
	parts := make([]string, 0, len(sources))
	args := make([]any, 0, len(sources))
	for i, source := range sources {
		if !boardKindMatchesSource(kinds[i], q.Source) {
			continue
		}
		source = boardSeek(source, q, kinds[i], "updated_at", "owner_id").Select("owner_kind,owner_id,updated_at")
		if q.Offset == 0 {
			source = source.Order("updated_at DESC,owner_id DESC").Limit(q.Limit + 1)
		}
		parts = append(parts, "SELECT * FROM (?)")
		args = append(args, source)
	}
	var keys []models.WorkBoardEntry
	if err := db.Table("(?) candidates", db.Raw(strings.Join(parts, " UNION ALL "), args...)).Order("updated_at DESC,owner_kind DESC,owner_id DESC").Offset(q.Offset).Limit(q.Limit + 1).Find(&keys).Error; err != nil {
		return nil, databaseError("seek_board_awaiting_view", "", err)
	}
	return c.attachWorkEntries(ctx, keys)
}

// AttachReadings 只为传入的工作窗口补齐公开消息的阅读边界。
func (c *workBoardController) AttachReadings(ctx context.Context, reader string, entries []models.WorkBoardEntry, isolated []string) (map[string]models.ChatReadingState, error) {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.ChatID != "" && !slices.Contains(isolated, entry.ChatID) {
			ids = append(ids, entry.ChatID)
		}
	}
	rows, err := readRelatedRows[models.Chat](controllerDB(ctx, c.db), "id", "id,latest_readable_sequence", ids)
	if err != nil {
		return nil, err
	}
	readings, err := NewChatReadingController(c.db).Readings(ctx, reader, rows)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		reading := readings[entries[i].ChatID]
		entries[i].UnreadMessageCount = reading.UnreadMessageCount
		entries[i].FirstUnreadMessageID = reading.FirstUnreadMessageID
		entries[i].LatestReadableMessageID = reading.LatestReadableMessageID
	}
	return readings, nil
}

// ExecutorAttentionFacts 保存指定执行对象的公开会话与原有效请求。
type ExecutorAttentionFacts struct {
	// Chats 是该执行对象范围内可见的会话容器。
	Chats []models.WorkBoardEntry
	// Readings 以公开 Chat 标识映射可验证的阅读状态。
	Readings map[string]models.ChatReadingState
	// Requests 保留原有效请求及其精确定位。
	Requests []models.WorkBoardAttention
}

// ExecutorAttentionFacts 读取指定执行对象的关注摘要所需原事实。
func (c *workBoardController) ExecutorAttentionFacts(ctx context.Context, reader string, executors, isolated []string) (ExecutorAttentionFacts, error) {
	var value ExecutorAttentionFacts
	if len(executors) == 0 {
		return value, nil
	}
	db := controllerDB(ctx, c.db)
	for _, branch := range []string{workBoardChatSelect, workBoardThreadSelect} {
		var entries []models.WorkBoardEntry
		if err := db.Table("(?) board", db.Raw(branch)).Where("executor_id IN ?", executors).Find(&entries).Error; err != nil {
			return value, databaseError("list_executor_attention_chats", "", err)
		}
		value.Chats = append(value.Chats, entries...)
	}
	var err error
	value.Readings, err = c.AttachReadings(ctx, reader, value.Chats, isolated)
	if err != nil {
		return value, err
	}
	ids := make(map[string]bool, len(executors))
	for _, id := range executors {
		ids[id] = true
	}
	scope := WorkBoardQuery{IsolatedChats: isolated}
	err = c.walkAttention(ctx, time.Now().UnixMilli(), boardAttentionScope{executors: executors}, true, func(a models.WorkBoardAttention) error {
		if ids[a.ExecutorID] && boardAttentionMatches(a, scope) {
			value.Requests = append(value.Requests, a)
		}
		return nil
	})
	return value, err
}

// ResolveWorkRef 按统一可见性校验原工作身份。
func (c *workBoardController) ResolveWorkRef(ctx context.Context, ref models.WorkRef) (models.WorkBoardEntry, error) {
	var branch string
	switch ref.Kind {
	case constants.WorkOwnerKindChat:
		branch = workBoardChatSelect
	case constants.WorkOwnerKindTeamThread:
		branch = workBoardThreadSelect
	case constants.WorkOwnerKindWorkflowRun:
		branch = workBoardRunSelect
	case constants.WorkOwnerKindAutomaticRun:
		branch = workBoardAutomaticSelect
	default:
		return models.WorkBoardEntry{}, status_error.BadRequest
	}
	var value models.WorkBoardEntry
	db := controllerDB(ctx, c.db)
	res := db.Table("(?) board", db.Raw(branch)).Where("owner_id=?", ref.ID).Limit(1).Find(&value)
	if res.Error != nil {
		return value, databaseError("resolve_work_ref", ref.ID, res.Error)
	}
	return value, nil
}

// ResolveChatOutcome 只从公开容器解析会话结果，团队成员与工作流 worker 的消息归原工作呈现。
func (c *workBoardController) ResolveChatOutcome(ctx context.Context, chatID string) (models.WorkBoardEntry, error) {
	db := controllerDB(ctx, c.db)
	const columns = "owner_kind,owner_id,title,chat_id,waker_id,channel_id,workflow_id"
	team := db.Table("(?) board", db.Raw(workBoardThreadSelect)).Select(columns).Where("chat_id = ?", chatID)
	chat := db.Table("(?) board", db.Raw(workBoardChatSelect)).Select(columns).Where("owner_id = ?", chatID)
	var value models.WorkBoardEntry
	if err := db.Raw("SELECT * FROM (?) UNION ALL SELECT * FROM (?) LIMIT 1", team, chat).Scan(&value).Error; err != nil {
		return value, databaseError("resolve_chat_outcome", chatID, err)
	}
	return value, nil
}

// ResolveWorkRefs 按来源批量解析仍可见的原容器。
func (c *workBoardController) ResolveWorkRefs(ctx context.Context, refs []models.WorkRef) (map[models.WorkRef]models.WorkBoardEntry, error) {
	keys := make([]models.WorkBoardEntry, 0, len(refs))
	for _, ref := range refs {
		switch ref.Kind {
		case constants.WorkOwnerKindChat, constants.WorkOwnerKindTeamThread, constants.WorkOwnerKindWorkflowRun, constants.WorkOwnerKindAutomaticRun:
			keys = append(keys, models.WorkBoardEntry{OwnerKind: ref.Kind, OwnerID: ref.ID})
		default:
			return nil, status_error.BadRequest
		}
	}
	rows, err := c.attachWorkEntries(ctx, keys)
	if err != nil {
		return nil, err
	}
	byRef := make(map[models.WorkRef]models.WorkBoardEntry, len(rows))
	for _, row := range rows {
		byRef[models.WorkRef{Kind: row.OwnerKind, ID: row.OwnerID}] = row
	}
	return byRef, nil
}

// FindPendingRequest 校验并定位仍有效的原请求。
func (c *workBoardController) FindPendingRequest(ctx context.Context, lookup models.WorkBoardLiveEvent) (models.WorkBoardAttention, error) {
	at := time.Now().UnixMilli()
	db := controllerDB(ctx, c.db)
	chats, tasks, humans, operations := boardPendingQueries(db, at)
	var chatRows []models.Chat
	var taskRows []models.ChannelTask
	var humanRows []models.WorkflowHumanRequest
	var operationRows []models.WorkflowOperationRequest
	var err error
	switch {
	case lookup.RequestKind == "review":
		err = tasks.Where("id = ? AND current_fence = ?", lookup.RequestID, lookup.RequestFence).Find(&taskRows).Error
	case lookup.ChatID != "":
		err = chats.Where("id = ? AND active_execution_id = ?", lookup.ChatID, lookup.ExecutionID).Find(&chatRows).Error
	case lookup.RequestKind == "input":
		err = humans.Where("id = ? AND run_id = ?", lookup.RequestID, lookup.ExecutionID).Find(&humanRows).Error
	case lookup.RequestKind == "approval":
		err = operations.Where("id = ? AND run_id = ?", lookup.RequestID, lookup.ExecutionID).Find(&operationRows).Error
	}
	if err != nil {
		return models.WorkBoardAttention{}, databaseError("find_pending_request", lookup.RequestID, err)
	}
	facts, err := c.attentionBatch(ctx, at, chatRows, taskRows, humanRows, operationRows, true)
	if err != nil {
		return models.WorkBoardAttention{}, err
	}
	originalChat := lookup.ChatID != "" && lookup.Work.Kind == constants.WorkOwnerKindChat && lookup.Work.ID == lookup.ChatID && lookup.RequestKind != "review"
	originalRun := lookup.ChatID == "" && lookup.ExecutionID != "" && lookup.Work.Kind == constants.WorkOwnerKindWorkflowRun && lookup.Work.ID == lookup.ExecutionID && lookup.RequestKind != "review"
	for _, a := range facts {
		ownerMatches := originalChat || originalRun || a.OwnerID == lookup.Work.ID && a.OwnerKind == lookup.Work.Kind
		chatMatches := a.ChatID == lookup.ChatID || lookup.RequestKind == "review" && lookup.ChatID == ""
		if ownerMatches && chatMatches && a.RequestID == lookup.RequestID && a.Kind == lookup.RequestKind && a.ExecutionID == lookup.ExecutionID && a.RequestFence == lookup.RequestFence {
			return a, nil
		}
	}
	return models.WorkBoardAttention{}, nil
}
