package plugins

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/resourcecategory"
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
	if o.Body.Categories != nil {
		names, err := resourcecategory.Normalize(*o.Body.Categories)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		o.Body.Categories = &names
	}
	if err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		if o.Body.Categories != nil {
			if err := global.ResourceCategoryControllerManager.Ensure(txctx, constants.ResourceCategoryTypePlugin, *o.Body.Categories); err != nil {
				return err
			}
		}
		return global.PluginControllerManager.Update(txctx, o.PluginID, name, strings.TrimSpace(o.Body.Description), o.Body.Categories)
	}); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
