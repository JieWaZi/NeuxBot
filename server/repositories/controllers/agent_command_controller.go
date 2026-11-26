package controllers

import (
	"context"
	"errors"

	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// AgentCommandController 管理原子领域回执及其聊天投影确认。
type AgentCommandController interface {
	capability.Store
	// ListPendingManagedChanges 分页读取未确认的资源变更，可限定聊天和操作。
	ListPendingManagedChanges(ctx context.Context, chatID, operation string, after capability.Receipt, limit int) ([]capability.Receipt, error)
	// MarkManagedChangeProjected 确认指定回执的日志投影已完成。
	MarkManagedChangeProjected(ctx context.Context, id string) error
}

// agentCommandController 复用主业务数据库与事务连接。
type agentCommandController struct {
	// db 是应用主数据库连接。
	db *gorm.DB
}

// NewAgentCommandController 构造无后台任务的命令回执仓储。
func NewAgentCommandController(db *gorm.DB) AgentCommandController {
	return &agentCommandController{db: db}
}

func (c *agentCommandController) Find(ctx context.Context, id string) (capability.Receipt, bool, error) {
	var value models.AgentCommandReceipt
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return capability.Receipt{}, false, nil
	}
	if err != nil {
		return capability.Receipt{}, false, databaseError("get_agent_command", id, err)
	}
	return agentCommandReceipt(value), true, nil
}

func (c *agentCommandController) Create(ctx context.Context, value capability.Receipt) error {
	origin := value.Origin
	if err := controllerDB(ctx, c.db).Create(&models.AgentCommandReceipt{
		ID: value.ID, Digest: value.Digest, Data: value.Data,
		ManagedChatID: origin.ChatID, ManagedExecutionID: origin.ExecutionID, ManagedWakerID: origin.WakerID, ManagedOperation: origin.Operation,
	}).Error; err != nil {
		return databaseError("create_agent_command", value.ID, err)
	}
	return nil
}

func (c *agentCommandController) ListPendingManagedChanges(ctx context.Context, chatID, operation string, after capability.Receipt, limit int) ([]capability.Receipt, error) {
	query := controllerDB(ctx, c.db).Where("managed_chat_id <> '' AND managed_projected = 0").
		Where("(managed_chat_id, id) > (?, ?)", after.Origin.ChatID, after.ID)
	if chatID != "" {
		query = query.Where("managed_chat_id = ?", chatID)
	}
	if operation != "" {
		query = query.Where("managed_operation = ?", operation)
	}
	var rows []models.AgentCommandReceipt
	if err := query.Order("managed_chat_id, id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, databaseError("list_pending_managed_changes", chatID, err)
	}
	receipts := make([]capability.Receipt, 0, len(rows))
	for _, row := range rows {
		receipts = append(receipts, agentCommandReceipt(row))
	}
	return receipts, nil
}

func (c *agentCommandController) MarkManagedChangeProjected(ctx context.Context, id string) error {
	err := controllerDB(ctx, c.db).Model(&models.AgentCommandReceipt{}).
		Where("id = ? AND managed_chat_id <> ''", id).
		UpdateColumn("managed_projected", true).Error
	if err != nil {
		return databaseError("mark_managed_change_projected", id, err)
	}
	return nil
}

func agentCommandReceipt(row models.AgentCommandReceipt) capability.Receipt {
	return capability.Receipt{ID: row.ID, Digest: row.Digest, Data: row.Data, Origin: capability.ReceiptOrigin{
		ChatID: row.ManagedChatID, ExecutionID: row.ManagedExecutionID, WakerID: row.ManagedWakerID, Operation: row.ManagedOperation,
	}}
}
