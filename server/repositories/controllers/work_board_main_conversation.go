package controllers

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// 工作看板的成果使用公开发言排序，来源 Chat 只负责执行过程与原待办。
const workBoardMainConversationSelect = `SELECT 'main_conversation' AS owner_kind, main.id AS owner_id, main.subject_id AS executor_id,
CASE main.subject_type WHEN 'team' THEN (SELECT name FROM channels WHERE id=main.subject_id)
 ELSE (SELECT name FROM wakers WHERE id=main.subject_id) END AS title,
CASE main.subject_type WHEN 'team' THEN 'team' ELSE 'chat' END AS source,
CASE WHEN main.subject_type='team' THEN main.subject_id ELSE '' END AS channel_id,
CASE WHEN main.subject_type='waker' THEN main.subject_id ELSE '' END AS waker_id,
'idle' AS activity, '' AS last_status,
COALESCE((SELECT created_at FROM main_conversation_messages WHERE conversation_id=main.id AND role='assistant' AND removed_at=0 ORDER BY sequence DESC LIMIT 1),0) AS updated_at
FROM main_conversations main
WHERE (main.subject_type='waker' AND EXISTS (SELECT 1 FROM wakers WHERE id=main.subject_id))
   OR (main.subject_type='team' AND EXISTS (SELECT 1 FROM channels WHERE id=main.subject_id))`

const boardMainUnreadRange = ` FROM main_conversation_messages WHERE role='assistant' AND removed_at=0 AND conversation_id=reading.owner_id AND sequence>reading.through_sequence`
const boardMainUnreadCount = "(SELECT COUNT(*)" + boardMainUnreadRange + ")"

func boardMainReadingScope(db *gorm.DB, reader string, q WorkBoardQuery) *gorm.DB {
	board := workBoardScope(db.Table("(?) board", db.Raw(workBoardMainConversationSelect)), q)
	return db.Table("(?) reading", board.Select(`board.*,
 (SELECT through_sequence FROM main_conversation_read_cursors WHERE reader_id=? AND conversation_id=board.owner_id) AS through_sequence,
 (SELECT unread_generation FROM main_conversation_read_cursors WHERE reader_id=? AND conversation_id=board.owner_id) AS unread_generation`, reader, reader))
}

func boardMainUnreadScope(db *gorm.DB, reader string, q WorkBoardQuery) *gorm.DB {
	return boardMainReadingScope(db, reader, q).
		Where("EXISTS (SELECT 1" + boardMainUnreadRange + ")")
}

func mainBoardMissingReading(db *gorm.DB, reader string, q WorkBoardQuery) (bool, error) {
	var missing bool
	scope := boardMainReadingScope(db, reader, q).Select("1").Where(`through_sequence IS NULL AND EXISTS
 (SELECT 1 FROM main_conversation_messages WHERE role='assistant' AND removed_at=0 AND conversation_id=reading.owner_id)`)
	if err := db.Raw("SELECT EXISTS(?)", scope).Scan(&missing).Error; err != nil {
		return false, databaseError("check_main_board_reading_baseline", "", err)
	}
	return missing, nil
}

// mainBoardReading 保存分页窗口中的公开游标与未读边界。
type mainBoardReading struct {
	// OwnerID 是公开主会话标识。
	OwnerID string
	// Count 是游标之后的助手公开发言数。
	Count int64
	// First 是第一条未读公开消息标识。
	First string
	// Latest 是本次快照中最后一条未读公开消息标识。
	Latest string
	// Generation 是观察到的手动未读代次。
	Generation int64
}

func attachMainBoardReadings(db *gorm.DB, reader string, entries []models.WorkBoardEntry) error {
	mainIDs := []string{}
	for _, entry := range entries {
		if entry.OwnerKind == constants.WorkOwnerKindMainConversation {
			mainIDs = append(mainIDs, entry.OwnerID)
		}
	}
	byID := make(map[string]mainBoardReading, len(mainIDs))
	for start := 0; start < len(mainIDs); start += repositoryReadBatch {
		batch := mainIDs[start:min(start+repositoryReadBatch, len(mainIDs))]
		scope := db.Table("(?) reading", db.Table("main_conversation_read_cursors").
			Select("conversation_id AS owner_id, through_sequence, unread_generation AS generation").
			Where("reader_id=? AND conversation_id IN ?", reader, batch))
		var rows []mainBoardReading
		if err := scope.Select("owner_id, generation, " + boardMainUnreadCount + " AS count, " +
			"COALESCE((SELECT id" + boardMainUnreadRange + " ORDER BY sequence LIMIT 1),'') AS first, " +
			"COALESCE((SELECT id" + boardMainUnreadRange + " ORDER BY sequence DESC LIMIT 1),'') AS latest").Scan(&rows).Error; err != nil {
			return databaseError("read_main_board_boundaries", "", err)
		}
		for _, row := range rows {
			byID[row.OwnerID] = row
		}
	}
	for i := range entries {
		if entries[i].OwnerKind == constants.WorkOwnerKindMainConversation {
			reading := byID[entries[i].OwnerID]
			entries[i].UnreadMessageCount = reading.Count
			entries[i].FirstUnreadMessageID = reading.First
			entries[i].LatestReadableMessageID = reading.Latest
			entries[i].UnreadGeneration = reading.Generation
		}
	}
	return nil
}

// mainBoardUnreadBySubject 聚合指定伙伴和团队的公开主会话阅读，不将其置入普通聊天列表。
func mainBoardUnreadBySubject(db *gorm.DB, reader string, subjects []string) (map[string]MainConversationSubjectUnread, error) {
	var rows []struct {
		// ExecutorID 是主会话的公开主体标识。
		ExecutorID string
		// MainConversationSubjectUnread 来自与未读计数相同的读取快照。
		MainConversationSubjectUnread `gorm:"embedded"`
	}
	err := boardMainUnreadScope(db, reader, WorkBoardQuery{}).Where("executor_id IN ?", subjects).
		Select("executor_id, unread_generation, " + boardMainUnreadCount + " AS count").Scan(&rows).Error
	if err != nil {
		return nil, databaseError("read_main_subject_unread", "", err)
	}
	counts := make(map[string]MainConversationSubjectUnread, len(rows))
	for _, row := range rows {
		counts[row.ExecutorID] = row.MainConversationSubjectUnread
	}
	return counts, nil
}
