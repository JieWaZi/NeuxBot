package controllers

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// agentCommandController 复用主业务数据库与事务连接。
type agentCommandController struct {
	// db 是应用主数据库连接。
	db *gorm.DB
}

// NewAgentCommandController 构造无后台任务的命令回执仓储。
func NewAgentCommandController(db *gorm.DB) agentcommand.Store {
	return &agentCommandController{db: db}
}

func (c *agentCommandController) Find(ctx context.Context, id string) (agentcommand.Receipt, bool, error) {
	var value models.AgentCommandReceipt
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentcommand.Receipt{}, false, nil
	}
	if err != nil {
		return agentcommand.Receipt{}, false, databaseError("get_agent_command", id, err)
	}
	return agentcommand.Receipt{ID: value.ID, Digest: value.Digest, Data: value.Data}, true, nil
}

func (c *agentCommandController) Create(ctx context.Context, value agentcommand.Receipt) error {
	if err := controllerDB(ctx, c.db).Create(&models.AgentCommandReceipt{ID: value.ID, Digest: value.Digest, Data: value.Data}).Error; err != nil {
		return databaseError("create_agent_command", value.ID, err)
	}
	return nil
}
