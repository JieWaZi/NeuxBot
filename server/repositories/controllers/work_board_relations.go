package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// boardSources 批量判断候选容器来源；自动入口优先于 IM，查询只返回去重身份。
func boardSources(db *gorm.DB, column string, ids []string) (map[string]string, error) {
	ids = uniqueRowIDs(ids)
	result := map[string]string{}
	for _, table := range []string{"im_conversation_links", "automatic_runs"} {
		if column == "workflow_run_id" && table == "im_conversation_links" {
			continue
		}
		for start := 0; start < len(ids); start += repositoryReadBatch {
			var found []string
			ownerTable := map[string]string{"chat_id": "chats", "thread_id": "channel_threads", "workflow_run_id": "workflow_runs"}[column]
			query := db.Table(ownerTable+" owner").Where("owner.id IN ?", ids[start:min(start+repositoryReadBatch, len(ids))]).Where("EXISTS (SELECT 1 FROM " + table + " source WHERE source." + column + "=owner.id)")
			if err := query.Pluck("owner.id", &found).Error; err != nil {
				return nil, databaseError("read_board_sources", table, err)
			}
			source := "im"
			if table == "automatic_runs" {
				source = "automatic"
			}
			for _, id := range found {
				result[id] = source
			}
		}
	}
	return result, nil
}

// boardExecutorNames 只为待返回的窗口补齐配置名称，不参与计数和排序。
func boardExecutorNames(db *gorm.DB, ids []string) (map[string]string, error) {
	ids = uniqueRowIDs(ids)
	result := map[string]string{}
	for _, source := range []struct {
		// Table 是原执行对象配置表。
		Table string
		// Name 是该配置的名称列。
		Name string
	}{{"wakers", "name"}, {"channels", "name"}, {"workflow_definitions", "title"}} {
		for start := 0; start < len(ids); start += repositoryReadBatch {
			var rows []models.WorkBoardExecutor
			if err := db.Table(source.Table).Select("id,"+source.Name+" AS name").Where("id IN ?", ids[start:min(start+repositoryReadBatch, len(ids))]).Find(&rows).Error; err != nil {
				return nil, databaseError("read_board_executor_names", source.Table, err)
			}
			for _, r := range rows {
				result[r.ID] = r.Name
			}
		}
	}
	return result, nil
}

func (c *workBoardController) attachExecutorNames(ctx context.Context, entries []models.WorkBoardEntry) error {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ExecutorID)
	}
	names, err := boardExecutorNames(controllerDB(ctx, c.db), ids)
	if err != nil {
		return err
	}
	for i := range entries {
		entries[i].ExecutorName = names[entries[i].ExecutorID]
	}
	return nil
}

// boardWorkflowRuns 沿当前候选的根运行和 joined 祖先批量读取，已访问集合阻止异常循环。
func boardWorkflowRuns(db *gorm.DB, ids []string) (map[string]models.WorkflowRun, error) {
	result := map[string]models.WorkflowRun{}
	seen := map[string]bool{}
	for len(ids) > 0 {
		next := []string{}
		for _, id := range ids {
			if id != "" && !seen[id] {
				seen[id] = true
				next = append(next, id)
			}
		}
		rows, err := readRelatedRows[models.WorkflowRun](db, "id", "id,definition_id,root_run_id,parent_run_id,joined,status,updated_at", next)
		if err != nil {
			return nil, err
		}
		ids = nil
		for _, r := range rows {
			result[r.ID] = r
			ids = append(ids, r.RootRunID)
			if r.Joined {
				ids = append(ids, r.ParentRunID)
			}
		}
	}
	return result, nil
}

func boardWorkflowAlive(r models.WorkflowRun, runs map[string]models.WorkflowRun) bool {
	seen := map[string]bool{}
	for r.ID != "" {
		if seen[r.ID] {
			return false
		}
		seen[r.ID] = true
		switch r.Status {
		case "queued", "running", "waiting", "recovering":
		default:
			return false
		}
		if !r.Joined || r.ParentRunID == "" {
			return true
		}
		r = runs[r.ParentRunID]
	}
	return true
}
