package controllers

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WorkflowConsumerController 保存不可扩大的单回合使用者能力。
type WorkflowConsumerController interface {
	// CreateConsumer 插入签发锚点；重复身份保留首次白名单与凭据。
	CreateConsumer(context.Context, models.WorkflowConsumerLease) error
	// GetConsumer 读取工具请求绑定的真实回合。
	GetConsumer(context.Context, string) (models.WorkflowConsumerLease, error)
}

// workflowConsumerController 始终加入业务层提供的统一事务。
type workflowConsumerController struct {
	// db 是没有上层事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewWorkflowConsumerController 构造单回合能力持久边界。
func NewWorkflowConsumerController(db *gorm.DB) WorkflowConsumerController {
	return &workflowConsumerController{db: db}
}

func (c *workflowConsumerController) CreateConsumer(ctx context.Context, value models.WorkflowConsumerLease) error {
	if err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{DoNothing: true}).Create(&value).Error; err != nil {
		return databaseError("create_workflow_consumer", value.ID, err)
	}
	return nil
}

func (c *workflowConsumerController) GetConsumer(ctx context.Context, id string) (models.WorkflowConsumerLease, error) {
	var value models.WorkflowConsumerLease
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return value, status_error.WorkflowNotFound
	}
	if err != nil {
		return value, databaseError("get_workflow_consumer", id, err)
	}
	return value, nil
}
