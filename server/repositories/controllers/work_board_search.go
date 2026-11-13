package controllers

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// workBoardSearchProbeLimit 限制标题查询快路径可读取的有序候选前缀。
const workBoardSearchProbeLimit = 512

// boardTitlePattern 按 SQLite LIKE 规则转义用户文字，统计和列表保持一致。
func boardTitlePattern(query string) string {
	return "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query) + "%"
}

// boardCandidateIDs 让有界原身份作为驱动表，避免优化器为排序改扫全部历史。
func boardCandidateIDs(db *gorm.DB, ids []string) *gorm.DB {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "(?)"
		args[i] = id
	}
	return db.Raw("SELECT column1 AS id FROM (VALUES "+strings.Join(placeholders, ",")+")", args...)
}

// boardReadBranch 用 CASE 明确先判断标题，再计算归属和活动关联，避免低匹配率时重复反查。
func boardReadBranch(db *gorm.DB, branch string, kind constants.WorkOwnerKind, query string) *gorm.DB {
	if query == "" {
		return db.Raw(branch)
	}
	marker, title := "WHERE NOT EXISTS", "chat.title"
	if kind == constants.WorkOwnerKindTeamThread {
		marker, title = "WHERE thread.ready_at", "thread.title"
	}
	if kind == constants.WorkOwnerKindWorkflowRun {
		marker, title = "WHERE run.parent_run_id", "(SELECT title FROM workflow_definitions WHERE id=run.definition_id)"
	}
	index := strings.LastIndex(branch, marker)
	if index < 0 {
		return db.Raw(branch)
	}
	return db.Raw(branch[:index]+"WHERE CASE WHEN "+title+" LIKE ? ESCAPE '\\' THEN ("+branch[index+len("WHERE "):]+") ELSE 0 END", boardTitlePattern(query))
}

// titleOwners 只扫描原标题的窄覆盖索引；它不是新增业务副本或正文全文索引。
func (c *workBoardController) titleOwners(ctx context.Context, kind constants.WorkOwnerKind, query string) *gorm.DB {
	db := controllerDB(ctx, c.db)
	switch kind {
	case constants.WorkOwnerKindAutomaticRun:
		return db.Model(&models.AutomaticRun{}).Select("id").Where("task_id IN (?)", db.Model(&models.AutomaticTask{}).Select("id").Where("name LIKE ? ESCAPE '\\'", boardTitlePattern(query)))
	case constants.WorkOwnerKindTeamThread:
		return db.Model(&models.ChannelThread{}).Select("id").Where("title LIKE ? ESCAPE '\\'", boardTitlePattern(query))
	case constants.WorkOwnerKindWorkflowRun:
		return db.Model(&models.WorkflowRun{}).Select("id").Where("definition_id IN (?)", db.Model(&models.WorkflowDefinition{}).Select("id").Where("title LIKE ? ESCAPE '\\'", boardTitlePattern(query)))
	default:
		return db.Model(&models.Chat{}).Select("id").Where("title LIKE ? ESCAPE '\\'", boardTitlePattern(query))
	}
}

// boardSeek 把固定种类比较折叠掉，让时间与身份元组匹配原表索引。
func boardSeek(db *gorm.DB, query WorkBoardQuery, kind constants.WorkOwnerKind, at, id string) *gorm.DB {
	if v := query.Cursor; v != nil {
		if kind < v.Kind {
			return db.Where(at+" <= ?", v.At)
		}
		if kind > v.Kind {
			return db.Where(at+" < ?", v.At)
		}
		return db.Where("("+at+","+id+") < (?,?)", v.At, v.ID)
	}
	return db
}

// listBranchPage 命中密集标题时只查有序前缀；不足一页时从原游标完整重查。
func (c *workBoardController) listBranchPage(ctx context.Context, q WorkBoardQuery, branch string, kind constants.WorkOwnerKind) ([]models.WorkBoardEntry, error) {
	db := controllerDB(ctx, c.db)
	base := func(query WorkBoardQuery) *gorm.DB {
		return boardSeek(workBoardScope(db.Table("(?) board", boardReadBranch(db, branch, kind, query.Query)), query), query, kind, "updated_at", "owner_id")
	}
	var values []models.WorkBoardEntry
	if q.Query != "" {
		probe := q
		probe.Query = ""
		var candidates []models.WorkBoardEntry
		if err := base(probe).Select("owner_id").Order("updated_at DESC,owner_id DESC").Limit(workBoardSearchProbeLimit + 1).Find(&candidates).Error; err != nil {
			return nil, databaseError("probe_work_titles", "", err)
		}
		ids := make([]string, 0, min(len(candidates), workBoardSearchProbeLimit))
		for _, v := range candidates[:min(len(candidates), workBoardSearchProbeLimit)] {
			ids = append(ids, v.OwnerID)
		}
		if len(ids) == 0 {
			return values, nil
		}
		window := db.Table("(?) board", boardReadBranch(db, branch, kind, q.Query)).Where("board.owner_id IN ?", ids)
		if err := boardSeek(workBoardScope(window, q), q, kind, "updated_at", "owner_id").Select("board.*").Order("updated_at DESC,owner_id DESC").Limit(q.Limit + 1).Find(&values).Error; err != nil {
			return nil, databaseError("search_work_title_window", "", err)
		}
		if len(values) >= q.Limit+1 || len(candidates) <= workBoardSearchProbeLimit {
			return values, nil
		}
		values = nil
		var matches []models.Chat
		if err := c.titleOwners(ctx, kind, q.Query).Limit(workBoardSearchProbeLimit + 1).Find(&matches).Error; err != nil {
			return nil, databaseError("find_work_title_ids", "", err)
		}
		if len(matches) <= workBoardSearchProbeLimit {
			if len(matches) == 0 {
				return values, nil
			}
			ids = ids[:0]
			for _, v := range matches {
				ids = append(ids, v.ID)
			}
			window = db.Table("(?) board", boardReadBranch(db, branch, kind, q.Query)).Where("board.owner_id IN ?", ids)
			if err := boardSeek(workBoardScope(window, q), q, kind, "updated_at", "owner_id").Select("board.*").Order("updated_at DESC,owner_id DESC").Limit(q.Limit + 1).Find(&values).Error; err != nil {
				return nil, databaseError("search_sparse_work_titles", "", err)
			}
		} else if err := base(q).Order("updated_at DESC,owner_id DESC").Limit(q.Limit + 1).Find(&values).Error; err != nil {
			return nil, databaseError("search_dense_work_titles", "", err)
		}
		return values, nil
	}
	if err := base(q).Order("updated_at DESC,owner_id DESC").Limit(q.Limit + 1).Find(&values).Error; err != nil {
		return nil, databaseError("list_work_board", "", err)
	}
	return values, nil
}

// Page 只在数字跳页时由 SQL 合并有序身份；深页不向 Go 加载已跳过的正文和历史行。
func (c *workBoardController) Page(ctx context.Context, q WorkBoardQuery) ([]models.WorkBoardEntry, error) {
	db := controllerDB(ctx, c.db)
	kinds := []constants.WorkOwnerKind{constants.WorkOwnerKindChat, constants.WorkOwnerKindTeamThread, constants.WorkOwnerKindWorkflowRun, constants.WorkOwnerKindAutomaticRun}
	parts := make([]string, 0, 3)
	args := make([]any, 0, 3)
	for i, branch := range workBoardBranches() {
		if !boardKindMatchesSource(kinds[i], q.Source) {
			continue
		}
		keys := workBoardScope(db.Table("(?) board", db.Raw(boardCountBranch(boardNativeCandidates(branch, kinds[i], q), kinds[i], q))), q).Select("owner_kind,owner_id,updated_at")
		parts = append(parts, "SELECT * FROM (?)")
		args = append(args, keys)
	}
	var keys []models.WorkBoardEntry
	if len(parts) == 0 {
		return keys, nil
	}
	if err := db.Table("(?) candidates", db.Raw(strings.Join(parts, " UNION ALL "), args...)).Select("owner_kind,owner_id,updated_at").Order("updated_at DESC,owner_kind DESC,owner_id DESC").Offset(q.Offset).Limit(q.Limit + 1).Find(&keys).Error; err != nil {
		return nil, databaseError("seek_work_board_page", "", err)
	}
	page, err := c.attachWorkEntries(ctx, keys)
	if err != nil {
		return nil, err
	}
	if err := c.attachAttentionCounts(ctx, page, q.IsolatedChats); err != nil {
		return nil, err
	}
	if err := c.attachExecutorNames(ctx, page); err != nil {
		return nil, err
	}
	return page, nil
}
