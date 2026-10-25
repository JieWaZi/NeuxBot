package role_presets

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除预设
type DeleteRolePreset struct {
	ginx.MethodDelete
	// 预设标识
	RolePresetID string `in:"path" name:"rolePresetId" validate:"required"`
}

// Path 返回删除角色预设接口路径。
func (d *DeleteRolePreset) Path() string { return "/:rolePresetId" }

// Output 删除用户角色预设及其三份初始档案。
func (d *DeleteRolePreset) Output(ctx *gin.Context) (interface{}, error) {
	err := global.TransactionControllerManager.Transaction(
		ctx,
		func(transactionContext context.Context) error {
			preset, err := global.RolePresetControllerManager.Get(
				transactionContext,
				d.RolePresetID,
			)
			if err != nil {
				return err
			}
			if preset.Source != constants.RolePresetSourceUser {
				return status_error.SystemRolePresetReadOnly
			}
			if err := global.ProfileDocumentControllerManager.DeleteRolePresetDocuments(
				transactionContext,
				d.RolePresetID,
			); err != nil {
				return err
			}
			return global.RolePresetControllerManager.Delete(
				transactionContext,
				d.RolePresetID,
			)
		},
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
