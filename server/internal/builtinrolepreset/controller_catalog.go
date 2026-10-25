package builtinrolepreset

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ControllerCatalog 使用 Repository Controller 实现内置角色预设目录端口。
type ControllerCatalog struct {
	// Presets 提供角色预设持久化能力。
	Presets controllers.RolePresetController
	// Documents 提供角色预设档案持久化能力。
	Documents controllers.ProfileDocumentController
}

// Create 创建角色预设。
func (catalog ControllerCatalog) Create(
	ctx context.Context,
	preset models.RolePreset,
) error {
	return catalog.Presets.Create(ctx, preset)
}

// Get 按固定标识读取角色预设。
func (catalog ControllerCatalog) Get(
	ctx context.Context,
	rolePresetID string,
) (models.RolePreset, error) {
	return catalog.Presets.Get(ctx, rolePresetID)
}

// Update 更新角色预设的元数据和展示投影。
func (catalog ControllerCatalog) Update(
	ctx context.Context,
	rolePresetID string,
	preset models.RolePreset,
) error {
	return catalog.Presets.Update(ctx, rolePresetID, preset)
}

// DeleteDocuments 删除角色预设落库的三份档案。
func (catalog ControllerCatalog) DeleteDocuments(
	ctx context.Context,
	rolePresetID string,
) error {
	return catalog.Documents.DeleteRolePresetDocuments(ctx, rolePresetID)
}
