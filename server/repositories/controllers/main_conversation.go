package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MainConversationStore 是主会话业务复用的身份、来源与发言仓储。
type MainConversationStore interface {
	Ensure(ctx context.Context, kind, id string) (models.MainConversation, error)
	Get(ctx context.Context, id string) (models.MainConversation, error)
	ReserveWork(ctx context.Context, convID, wakerID, scope string) (models.MainConversationWork, error)
	ReserveExactWork(ctx context.Context, convID, wakerID, scope, chatID string) (models.MainConversationWork, error)
	FindWork(ctx context.Context, id string) (models.MainConversationWork, bool, error)
	Work(ctx context.Context, id string) (models.MainConversationWork, error)
	Message(ctx context.Context, convID, msgID string) (models.MainConversationMessage, error)
	FindMessage(ctx context.Context, convID, msgID string) (models.MainConversationMessage, bool, error)
	FindWorkflowInput(ctx context.Context, conversationID, chatID, executionID string) (models.MainConversationMessage, error)
	HasAutomaticTaskReceipt(ctx context.Context, conversationID, chatID, executionID string) (bool, error)
	HasCommittedPrivateInput(ctx context.Context, work models.MainConversationWork, executionID string) (bool, error)
	FindPublishedBody(ctx context.Context, convID, chatID, executionID, content string) (models.MainConversationMessage, error)
	FindAppendReceipt(ctx context.Context, msg models.MainConversationMessage) (models.MainConversationMessage, bool, error)
	HasVisibleMessageAfter(ctx context.Context, conversationID string, sequence int64) (bool, error)
	Append(ctx context.Context, msg models.MainConversationMessage, kind, id string) (models.MainConversationMessage, bool, error)
	Messages(ctx context.Context, id string, before int64, limit int) ([]models.MainConversationMessage, bool, error)
	ConfirmUserAdmission(ctx context.Context, conversationID, messageID string) (models.MainConversationMessage, error)
	PendingUserAdmissions(ctx context.Context, conversationID, after string, limit int) ([]models.MainConversationMessage, error)
	OwnedWorkChatIDs(ctx context.Context, conversationID string, candidates []string) ([]string, error)
	ActiveWork(ctx context.Context, conversationID string) ([]models.MainConversationActivity, error)
	UnansweredFailures(ctx context.Context, conversationID string, page []models.MainConversationMessage) (map[string]MainConversationFailureCandidate, error)
	RemoveMessage(ctx context.Context, conversation models.MainConversation, messageID string) error
	WorkInputTriggers(ctx context.Context, work models.MainConversationWork, inputIDs []string) ([]models.MainConversationMessage, error)
	WorkPublications(ctx context.Context, work models.MainConversationWork, executionIDs []string) ([]models.MainConversationMessage, error)
	InitializeReadingBaseline(ctx context.Context, reader string) error
	ReadingCursor(ctx context.Context, reader, conversationID string) (models.MainConversationReadCursor, error)
	Reading(ctx context.Context, reader, conversationID string) (models.MainConversationReadingState, error)
	MarkRead(ctx context.Context, reader, conversationID, messageID string, generation int64, intent constants.ReadingIntent) error
	MarkReadForBoard(ctx context.Context, reader, conversationID, messageID string, generation int64) (bool, error)
	MarkUnread(ctx context.Context, reader, conversationID, messageID string, generation int64) error
}

// MainConversationController 保存并检索主会话、范围绑定与有序公开消息。
type MainConversationController struct {
	// db 提供当前事务的默认数据库连接。
	db *gorm.DB
	// transaction 保证会话创建与阅读游标更新的原子性。
	transaction TransactionController
}

// NewMainConversationController 建立使用同一业务数据库的主会话仓储。
func NewMainConversationController(db *gorm.DB) *MainConversationController {
	return &MainConversationController{db: db, transaction: NewTransactionController(dbhelper.NewGormTransactionManager(db))}
}

// Ensure 为同一主体建立唯一会话，并返回并发请求共同持有的地址。
func (c *MainConversationController) Ensure(ctx context.Context, kind, id string) (models.MainConversation, error) {
	if ctx.Value(databaseTransactionKey{}) == nil {
		var row models.MainConversation
		err := c.transaction.Transaction(ctx, func(tx context.Context) error {
			var err error
			row, err = c.Ensure(tx, kind, id)
			return err
		})
		return row, err
	}
	db := controllerDB(ctx, c.db)
	filter := models.MainConversation{SubjectType: kind, SubjectID: id}
	var row models.MainConversation
	if err := db.Where(&filter).Limit(1).Find(&row).Error; err != nil {
		return row, databaseError("find_main_conversation", id, err)
	}
	if row.ID != "" {
		return row, nil
	}
	row = filter
	row.ID = util.New()
	res := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "subject_type"}, {Name: "subject_id"}}, DoNothing: true}).Create(&row)
	if res.Error != nil {
		return row, databaseError("ensure_main_conversation", id, res.Error)
	}
	if res.RowsAffected == 0 {
		row = models.MainConversation{}
		if err := db.Where(&filter).Take(&row).Error; err != nil {
			return row, databaseError("resolve_main_conversation", id, err)
		}
	} else if err := c.createReadingCursor(ctx, constants.LocalReaderID, row.ID); err != nil {
		return row, err
	}
	return row, nil
}

// Get 读取一条已存在的公开会话。
func (c *MainConversationController) Get(ctx context.Context, id string) (models.MainConversation, error) {
	var row models.MainConversation
	if err := controllerDB(ctx, c.db).Where("id = ?", id).Limit(1).Find(&row).Error; err != nil {
		return row, databaseError("get_main_conversation", id, err)
	}
	if row.ID == "" {
		return row, status_error.ChatNotFound.WithParams(map[string]interface{}{"ChatID": id})
	}
	return row, nil
}

// publicSubjectConversationID 只读取原有主会话，删除过程不能新建地址。
func publicSubjectConversationID(db *gorm.DB, kind, id string) (string, error) {
	var row models.MainConversation
	if err := db.Select("id").Where("subject_type = ? AND subject_id = ?", kind, id).Limit(1).Find(&row).Error; err != nil {
		return "", databaseError("find_subject_conversation", id, err)
	}
	return row.ID, nil
}

// deleteMainConversation 在主体删除的同一事务内清除公开历史、发布回执和来源资格。
func deleteMainConversation(db *gorm.DB, kind, id string) error {
	convID, err := publicSubjectConversationID(db, kind, id)
	if err != nil || convID == "" {
		return err
	}
	if err = deleteWorkTickets(db, models.WorkTicket{OriginKind: constants.WorkTicketOriginMain, OriginID: convID}, convID); err != nil {
		return err
	}
	if err = db.Where("conversation_id = ?", convID).Delete(&models.MainConversationInput{}).Error; err != nil {
		return databaseError("delete_main_conversation_inputs", convID, err)
	}
	if err = db.Where("conversation_id = ?", convID).Delete(&models.MainConversationMessage{}).Error; err != nil {
		return databaseError("delete_main_messages", convID, err)
	}
	if err = db.Where("conversation_id = ?", convID).Delete(&models.MainConversationWork{}).Error; err != nil {
		return databaseError("delete_main_work", convID, err)
	}
	if err = db.Where("conversation_id = ?", convID).Delete(&models.MainConversationReadCursor{}).Error; err != nil {
		return databaseError("delete_main_read_cursor", convID, err)
	}
	if err = db.Where("id = ?", convID).Delete(&models.MainConversation{}).Error; err != nil {
		return databaseError("delete_main_conversation", convID, err)
	}
	return nil
}
