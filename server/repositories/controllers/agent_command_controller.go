package controllers

import (
	"context"
	"errors"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// agentCommandController 复用主业务数据库与事务连接。
type agentCommandController struct {
	// db 是应用主数据库连接。
	db *gorm.DB
}

// NewAgentCommandController 构造无后台任务的命令回执仓储。
func NewAgentCommandController(db *gorm.DB) capability.Store {
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
	return capability.Receipt{ID: value.ID, Digest: value.Digest, Data: value.Data}, true, nil
}

func (c *agentCommandController) Create(ctx context.Context, value capability.Receipt) error {
	if err := controllerDB(ctx, c.db).Create(&models.AgentCommandReceipt{ID: value.ID, Digest: value.Digest, Data: value.Data}).Error; err != nil {
		return databaseError("create_agent_command", value.ID, err)
	}
	return nil
}
