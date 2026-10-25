package role_presets

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 导出预设
type ExportRolePreset struct {
	ginx.MethodGet
	// 预设标识
	RolePresetID string `in:"path" name:"rolePresetId" validate:"required"`
}

// Path 返回角色预设档案导出接口路径。
func (e *ExportRolePreset) Path() string { return "/:rolePresetId/export" }

// Output 返回只包含三份初始档案的 ZIP。
func (e *ExportRolePreset) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.RolePresetControllerManager.Get(ctx, e.RolePresetID); err != nil {
		return nil, ginx.WithStack(err)
	}
	documents, err := global.ProfileDocumentControllerManager.ListRolePresetDocuments(
		ctx,
		e.RolePresetID,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	contents, err := builtinrolepreset.ResolveRolePresetContents(e.RolePresetID, documents)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	content, err := profile.CreateZIP(contents)
	if err != nil {
		return nil, ginx.WithStack(status_error.ProfileArchiveGenerationFailed)
	}
	attachment := ginx.NewAttachment("role-preset.zip", "application/zip")
	_, _ = attachment.Write(content)
	return attachment, nil
}
