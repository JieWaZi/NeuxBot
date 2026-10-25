package controllers

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm/clause"
)

// RecordLibrarySummary 保存已成功读取的摘要，失败响应与筛选结果不得调用。
func (c *knowledgeController) RecordLibrarySummary(ctx context.Context, v models.KnowledgeLibrarySummary) error {
	if v.Backend == "" || v.ConnectionID == "" || v.KnowledgeBaseID == "" || v.PageTotal < 0 || v.ObservedAt <= 0 {
		return status_error.KnowledgeInvalid
	}
	err := controllerDB(ctx, c.db).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "backend"}, {Name: "connection_id"}, {Name: "connection_revision"}, {Name: "knowledge_base_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"page_total", "observed_at"}),
		Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "excluded.observed_at > knowledge_library_summaries.observed_at"}}},
	}).Create(&v).Error
	if err != nil {
		return databaseError("record_knowledge_summary", v.KnowledgeBaseID, err)
	}
	return nil
}

// LibrarySummaries 单次有界查询当前页摘要，连接版本不匹配视为未知。
func (c *knowledgeController) LibrarySummaries(ctx context.Context, backend, connection string, revision int64, ids []string) ([]models.KnowledgeLibrarySummary, error) {
	rows := []models.KnowledgeLibrarySummary{}
	if len(ids) == 0 {
		return rows, nil
	}
	err := controllerDB(ctx, c.db).Where("backend = ? AND connection_id = ? AND connection_revision = ? AND knowledge_base_id IN ?", backend, connection, revision, ids).Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_knowledge_summaries", connection, err)
	}
	return rows, nil
}

// DeleteLibraryReferences 清理已删除库的宿主关联，保留操作锚点供未知结果对账。
func (c *knowledgeController) DeleteLibraryReferences(ctx context.Context, backend, connection, library string) error {
	if backend == "" || connection == "" || library == "" {
		return status_error.KnowledgeInvalid
	}
	err := c.transactions.Transaction(ctx, func(txctx context.Context) error {
		tx := controllerDB(txctx, c.db)
		for _, model := range []any{&models.KnowledgeTaskConfiguration{}, &models.KnowledgeLibrarySummary{}} {
			if err := tx.Where("backend = ? AND connection_id = ? AND knowledge_base_id = ?", backend, connection, library).Delete(model).Error; err != nil {
				return databaseError("delete_knowledge_library_references", library, err)
			}
		}
		if backend == "local" && connection == "local" {
			if err := removeIntegrationResourceGrants(txctx, c.db, constants.IntegrationResourceKindKnowledge, []string{library}); err != nil {
				return err
			}
			// 删除库时撤销尚未激活的密钥，幂等摘要和配置回执可继续对账。
			if err := tx.Model(&models.LibraryEmbeddingMutation{}).Where("library_id = ?", library).Update("ciphertext", "").Error; err != nil {
				return databaseError("delete_knowledge_library_references", library, err)
			}
			for _, model := range []any{&models.LibraryEmbeddingProfile{}, &models.LibraryEmbeddingSecret{}} {
				if err := tx.Where("library_id = ?", library).Delete(model).Error; err != nil {
					return databaseError("delete_knowledge_library_references", library, err)
				}
			}

			if err := tx.Where("library_id = ?", library).Delete(&models.LibraryExecutionDefaults{}).Error; err != nil {
				return databaseError("delete_knowledge_library_references", library, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	publishCommittedChange(ctx, c.db, "work-results", eventsResource("global", ""))
	return nil
}
