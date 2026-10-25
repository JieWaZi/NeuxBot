package controllers

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"gorm.io/gorm"
	"strings"
)

// boardCountBranch 计数不读取名称，归属排除使用覆盖索引集合，避免每行三次主键反查。
// 显式过滤空身份同时排除 NULL，保持与原非空容器主键的 NOT EXISTS 语义一致。
func boardCountBranch(branch string, kind constants.WorkResultOwnerKind, q WorkBoardQuery) string {
	if kind == constants.WorkResultOwnerKindChat {
		if q.Source == "" && q.Query == "" && q.Activity == "" {
			index := ""
			if len(q.States) == 0 {
				index = "idx_chat_board_count"
				if q.ExecutorID != "" {
					index = "idx_chat_board_executor_count"
				}
			} else {
				terminal := true
				for _, state := range q.States {
					if state != string(constants.WorkBoardStateCompleted) && state != string(constants.WorkBoardStateFailed) && state != string(constants.WorkBoardStateCanceled) {
						terminal = false
					}
				}
				if terminal {
					index = "idx_chat_board_terminal_count"
				}
			}
			// SQLite 的等值选择率估计会误选旧的宽行索引；只为已验证的计数路径固定覆盖索引。
			if index != "" {
				branch = strings.Replace(branch, "FROM chats chat", "FROM chats chat INDEXED BY "+index, 1)
			}
		}
		for _, table := range []string{"channel_thread_members", "workflow_effects", "workflow_creation_sessions"} {
			alias := "m"
			if table == "workflow_effects" {
				alias = "e"
			}
			if table == "workflow_creation_sessions" {
				alias = "s"
			}
			branch = strings.Replace(branch, "NOT EXISTS(SELECT 1 FROM "+table+" "+alias+" WHERE "+alias+".chat_id=chat.id)", "chat.id NOT IN (SELECT chat_id FROM "+table+" WHERE chat_id<>'')", 1)
		}
	}
	return branch
}

// boardKindMatchesSource 排除不可能匹配来源的分支，不发出无意义查询。
func boardKindMatchesSource(kind constants.WorkResultOwnerKind, source string) bool {
	switch source {
	case "chat":
		return kind == constants.WorkResultOwnerKindChat
	case "team":
		return kind == constants.WorkResultOwnerKindTeamThread
	case "workflow":
		return kind == constants.WorkResultOwnerKindWorkflowRun
	case "im":
		return kind != constants.WorkResultOwnerKindWorkflowRun
	}
	return true
}

// boardNativeCandidates 在原表的索引上先取得正向集合，最终仍由统一展示谓词校验。
func boardNativeCandidates(branch string, kind constants.WorkResultOwnerKind, q WorkBoardQuery) string {
	alias, column := "chat", "chat_id"
	if kind == constants.WorkResultOwnerKindTeamThread {
		alias, column = "thread", "thread_id"
	}
	if kind == constants.WorkResultOwnerKindWorkflowRun {
		alias, column = "run", "workflow_run_id"
	}
	switch q.Source {
	case "automatic":
		branch += " AND " + alias + ".id IN (SELECT " + column + " FROM automatic_runs WHERE " + column + " <> '')"
	case "im":
		if kind != constants.WorkResultOwnerKindWorkflowRun {
			branch += " AND " + alias + ".id IN (SELECT " + column + " FROM im_conversation_links WHERE " + column + " <> '')"
		}
	}
	if q.Activity == "" && len(q.States) == 1 {
		switch constants.WorkBoardState(q.States[0]) {
		case constants.WorkBoardStateRunning:
			q.Activity = constants.ChannelActivityStateRunning
		case constants.WorkBoardStateWaiting:
			q.Activity = constants.ChannelActivityStateWaiting
		}
	}
	if kind == constants.WorkResultOwnerKindChat && len(q.States) > 0 {
		terminalOnly := true
		for _, state := range q.States {
			if state != string(constants.WorkBoardStateCompleted) && state != string(constants.WorkBoardStateFailed) && state != string(constants.WorkBoardStateCanceled) {
				terminalOnly = false
			}
		}
		if terminalOnly {
			branch += " AND chat.active_execution_id='' AND chat.last_ended_execution_status IN ('completed','failed','canceled')"
		}
	}
	switch q.Activity {
	case constants.ChannelActivityStateRunning:
		switch kind {
		case constants.WorkResultOwnerKindChat:
			branch += " AND chat.id IN (SELECT id FROM chats WHERE active_execution_id > '')"
		case constants.WorkResultOwnerKindWorkflowRun:
			branch += " AND run.id IN (SELECT id FROM workflow_runs WHERE status IN ('queued','running','recovering','cancelling'))"
		case constants.WorkResultOwnerKindTeamThread:
			branch += ` AND thread.id IN (
   SELECT thread_id FROM channel_thread_members WHERE chat_id IN (SELECT id FROM chats WHERE active_execution_id>'')
   UNION SELECT thread_id FROM channel_tasks WHERE status IN ('pending','in_progress')
   UNION SELECT thread_id FROM channel_lead_inputs WHERE status IN ('pending','delivering')
   UNION SELECT thread_id FROM channel_lead_notifications WHERE status IN ('pending','delivering'))`
		}
	case constants.ChannelActivityStateWaiting:
		switch kind {
		case constants.WorkResultOwnerKindChat:
			branch += " AND chat.id IN (SELECT id FROM chats WHERE waiting_approval=1 OR waiting_elicitation=1) AND chat.active_execution_id<>''"
		case constants.WorkResultOwnerKindWorkflowRun:
			branch += " AND run.id IN (SELECT id FROM workflow_runs WHERE status='waiting')"
		case constants.WorkResultOwnerKindTeamThread:
			branch += ` AND thread.id IN (SELECT thread_id FROM channel_thread_members WHERE chat_id IN (SELECT id FROM chats WHERE active_execution_id<>'' AND (waiting_approval=1 OR waiting_elicitation=1)) UNION SELECT thread_id FROM channel_tasks WHERE status='in_review')`
		}
	}
	return branch
}

// boardReceiptSource 在关联原容器之前应用正向来源集合，避免逐行过滤大量未读。
func boardReceiptSource(db *gorm.DB, kind constants.WorkResultOwnerKind, source string) *gorm.DB {
	column := "chat_id"
	if kind == constants.WorkResultOwnerKindTeamThread {
		column = "thread_id"
	}
	if kind == constants.WorkResultOwnerKindWorkflowRun {
		column = "workflow_run_id"
	}
	switch source {
	case "automatic":
		return db.Where("receipt.owner_id IN (SELECT " + column + " FROM automatic_runs WHERE " + column + " <> '')")
	case "im":
		if kind != constants.WorkResultOwnerKindWorkflowRun {
			return db.Where("receipt.owner_id IN (SELECT " + column + " FROM im_conversation_links WHERE " + column + " <> '')")
		}
	}
	return db
}
