package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"strings"
	"unicode/utf8"
)

// 修改插件资料
type UpdatePlugin struct {
	ginx.MethodPut
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
	// 请求体
	Body UpdatePluginRequest `in:"body"`
}

// Path 返回展示资料修改入口。
func (*UpdatePlugin) Path() string { return "/:pluginId" }

// Output 只修改展示资料，不改写上游包。
func (o *UpdatePlugin) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := visiblePlugin(ctx, o.PluginID, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	name := util.NormalizeName(o.Body.Name)
	if name == "" || utf8.RuneCountInString(name) > 120 {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	if err := global.PluginControllerManager.Update(ctx, o.PluginID, name, strings.TrimSpace(o.Body.Description)); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
