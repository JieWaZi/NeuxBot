package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RuntimeModelCatalogController 持久化原生模型目录快照。
type RuntimeModelCatalogController interface {
	// Get 读取当前配置下的快照，缺失时返回空值。
	Get(context.Context, string, string, string) (models.RuntimeModelCatalog, error)
	// Store 写入最新的探测结果。
	Store(context.Context, models.RuntimeModelCatalog) error
}

type runtimeModelCatalogController struct {
	// db 是默认数据库连接。
	db *gorm.DB
}

// NewRuntimeModelCatalogController 创建原生模型目录存储。
func NewRuntimeModelCatalogController(db *gorm.DB) RuntimeModelCatalogController {
	return &runtimeModelCatalogController{db: db}
}

func (c *runtimeModelCatalogController) Get(ctx context.Context, runtimeID, accountID, fingerprint string) (models.RuntimeModelCatalog, error) {
	var catalog models.RuntimeModelCatalog
	err := controllerDB(ctx, c.db).
		Where("runtime_id = ? AND account_id = ? AND fingerprint = ?", runtimeID, accountID, fingerprint).
		Limit(1).Find(&catalog).Error
	if err != nil {
		return models.RuntimeModelCatalog{}, databaseError("get_runtime_model_catalog", runtimeID, err)
	}
	return catalog, nil
}

func (c *runtimeModelCatalogController) Store(ctx context.Context, catalog models.RuntimeModelCatalog) error {
	catalog.ID = util.New()
	err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "runtime_id"}, {Name: "account_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"auth_generation", "fingerprint", "probe_started_at", "snapshot"}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "runtime_model_catalogs.probe_started_at <= excluded.probe_started_at"},
		}},
	}).Create(&catalog).Error
	if err != nil {
		return databaseError("store_runtime_model_catalog", catalog.RuntimeID, err)
	}
	return nil
}
