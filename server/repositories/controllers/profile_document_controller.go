// Package controllers 定义持久化接口及其 GORM 实现。
package controllers

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// ProfileDocumentController 定义角色预设和伙伴文档的持久化操作。
type ProfileDocumentController interface {
	// CreateRolePresetDocuments 批量创建角色预设的三份初始文档。
	CreateRolePresetDocuments(ctx context.Context, documents []models.RolePresetDocument) error
	// ListRolePresetDocuments 返回角色预设的全部初始文档。
	ListRolePresetDocuments(ctx context.Context, rolePresetID string) ([]models.RolePresetDocument, error)
	// UpdateRolePresetDocument 更新角色预设的一份完整文档。
	UpdateRolePresetDocument(
		ctx context.Context,
		rolePresetID string,
		kind constants.ProfileDocumentKind,
		content string,
		updatedAt int64,
	) error
	// DeleteRolePresetDocuments 删除角色预设拥有的全部初始文档。
	DeleteRolePresetDocuments(ctx context.Context, rolePresetID string) error
	// CreateWakerDocuments 批量创建伙伴的档案和记忆文档。
	CreateWakerDocuments(ctx context.Context, documents []models.WakerDocument) error
	// ListWakerDocuments 返回伙伴拥有的全部档案和记忆文档。
	ListWakerDocuments(ctx context.Context, wakerID string) ([]models.WakerDocument, error)
	// GetWakerDocument 返回伙伴的一份指定文档。
	GetWakerDocument(
		ctx context.Context,
		wakerID string,
		kind constants.ProfileDocumentKind,
	) (models.WakerDocument, error)
	// UpdateWakerDocument 更新伙伴的一份完整档案。
	UpdateWakerDocument(
		ctx context.Context,
		wakerID string,
		kind constants.ProfileDocumentKind,
		content string,
		updatedAt int64,
	) error
	// DeleteWakerDocuments 删除伙伴拥有的全部档案和记忆文档。
	DeleteWakerDocuments(ctx context.Context, wakerID string) error
}

// profileDocumentController 使用 GORM 实现档案文档持久化。
type profileDocumentController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewProfileDocumentController 创建档案文档 Repository Controller。
func NewProfileDocumentController(db *gorm.DB) ProfileDocumentController {
	return &profileDocumentController{db: db}
}

// CreateRolePresetDocuments 批量创建角色预设的三份初始文档。
func (c *profileDocumentController) CreateRolePresetDocuments(
	ctx context.Context,
	documents []models.RolePresetDocument,
) error {
	if len(documents) == 0 {
		return nil
	}
	err := controllerDB(ctx, c.db).Create(&documents).Error
	if err != nil {
		return databaseError("create_role_preset_documents", "role_preset_document", err)
	}
	return nil
}

// ListRolePresetDocuments 返回角色预设的全部初始文档。
func (c *profileDocumentController) ListRolePresetDocuments(
	ctx context.Context,
	rolePresetID string,
) ([]models.RolePresetDocument, error) {
	var documents []models.RolePresetDocument
	err := controllerDB(ctx, c.db).
		Where("role_preset_id = ?", rolePresetID).
		Order("kind ASC").
		Find(&documents).Error
	if err != nil {
		return nil, databaseError(
			"list_role_preset_documents",
			"role_preset_id="+rolePresetID,
			err,
		)
	}
	return documents, nil
}

// UpdateRolePresetDocument 更新角色预设的一份完整文档。
func (c *profileDocumentController) UpdateRolePresetDocument(
	ctx context.Context,
	rolePresetID string,
	kind constants.ProfileDocumentKind,
	content string,
	updatedAt int64,
) error {
	result := controllerDB(ctx, c.db).
		Model(&models.RolePresetDocument{}).
		Where("role_preset_id = ? AND kind = ?", rolePresetID, kind).
		Updates(map[string]any{"content": content, "updated_at": updatedAt})
	if result.Error != nil {
		return databaseError(
			"update_role_preset_document",
			"role_preset_id="+rolePresetID+" kind="+string(kind),
			result.Error,
		)
	}
	return nil
}

// DeleteRolePresetDocuments 删除角色预设拥有的全部初始文档。
func (c *profileDocumentController) DeleteRolePresetDocuments(
	ctx context.Context,
	rolePresetID string,
) error {
	err := controllerDB(ctx, c.db).
		Where("role_preset_id = ?", rolePresetID).
		Delete(&models.RolePresetDocument{}).Error
	if err != nil {
		return databaseError(
			"delete_role_preset_documents",
			"role_preset_id="+rolePresetID,
			err,
		)
	}
	return nil
}

// CreateWakerDocuments 批量创建伙伴的档案和记忆文档。
func (c *profileDocumentController) CreateWakerDocuments(
	ctx context.Context,
	documents []models.WakerDocument,
) error {
	if len(documents) == 0 {
		return nil
	}
	err := controllerDB(ctx, c.db).Create(&documents).Error
	if err != nil {
		return databaseError("create_waker_documents", "waker_document", err)
	}
	return nil
}

// ListWakerDocuments 返回伙伴拥有的全部档案和记忆文档。
func (c *profileDocumentController) ListWakerDocuments(
	ctx context.Context,
	wakerID string,
) ([]models.WakerDocument, error) {
	var documents []models.WakerDocument
	err := controllerDB(ctx, c.db).
		Where("waker_id = ?", wakerID).
		Order("kind ASC").
		Find(&documents).Error
	if err != nil {
		return nil, databaseError("list_waker_documents", "waker_id="+wakerID, err)
	}
	return documents, nil
}

// GetWakerDocument 返回伙伴的一份指定文档。
func (c *profileDocumentController) GetWakerDocument(
	ctx context.Context,
	wakerID string,
	kind constants.ProfileDocumentKind,
) (models.WakerDocument, error) {
	var document models.WakerDocument
	err := controllerDB(ctx, c.db).
		Where("waker_id = ? AND kind = ?", wakerID, kind).
		First(&document).Error
	if err == nil {
		return document, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.WakerDocument{}, status_error.ProfileDocumentNotFound
	}
	return models.WakerDocument{}, databaseError(
		"get_waker_document",
		"waker_id="+wakerID+" kind="+string(kind),
		err,
	)
}

// UpdateWakerDocument 更新伙伴的一份完整档案。
func (c *profileDocumentController) UpdateWakerDocument(
	ctx context.Context,
	wakerID string,
	kind constants.ProfileDocumentKind,
	content string,
	updatedAt int64,
) error {
	result := controllerDB(ctx, c.db).
		Model(&models.WakerDocument{}).
		Where("waker_id = ? AND kind = ?", wakerID, kind).
		Updates(map[string]any{"content": content, "updated_at": updatedAt})
	if result.Error != nil {
		return databaseError(
			"update_waker_document",
			"waker_id="+wakerID+" kind="+string(kind),
			result.Error,
		)
	}
	return nil
}

// DeleteWakerDocuments 删除伙伴拥有的全部档案和记忆文档。
func (c *profileDocumentController) DeleteWakerDocuments(
	ctx context.Context,
	wakerID string,
) error {
	err := controllerDB(ctx, c.db).
		Where("waker_id = ?", wakerID).
		Delete(&models.WakerDocument{}).Error
	if err != nil {
		return databaseError("delete_waker_documents", "waker_id="+wakerID, err)
	}
	return nil
}
