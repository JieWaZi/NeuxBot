package role_presets

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取预设
type GetRolePreset struct {
	ginx.MethodGet
	// 预设标识
	RolePresetID string `in:"path" name:"rolePresetId" validate:"required"`
}

// Path 返回角色预设详情接口路径。
func (g *GetRolePreset) Path() string { return "/:rolePresetId" }

// Output 返回角色预设详情和展示投影。
func (g *GetRolePreset) Output(ctx *gin.Context) (interface{}, error) {
	preset, err := global.RolePresetControllerManager.Get(ctx, g.RolePresetID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return preset, nil
}
