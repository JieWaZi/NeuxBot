package role_presets

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 预设文档列表
type ListRolePresetDocuments struct {
	ginx.MethodGet
	// 预设标识
	RolePresetID string `in:"path" name:"rolePresetId" validate:"required"`
}

// Path 返回角色预设文档接口路径。
func (l *ListRolePresetDocuments) Path() string { return "/:rolePresetId/documents" }

// Output 返回三份完整初始档案。
func (l *ListRolePresetDocuments) Output(ctx *gin.Context) (interface{}, error) {
	preset, err := global.RolePresetControllerManager.Get(ctx, l.RolePresetID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	rows, err := global.ProfileDocumentControllerManager.ListRolePresetDocuments(
		ctx,
		l.RolePresetID,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	documents, err := builtinrolepreset.ResolvedRolePresetDocuments(
		l.RolePresetID,
		rows,
		preset.UpdatedAt,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return documents, nil
}
