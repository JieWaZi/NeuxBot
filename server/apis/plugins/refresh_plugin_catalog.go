package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 检查插件更新
type RefreshPluginCatalog struct {
	ginx.MethodPost
}

// Path 返回显式目录核验入口。
func (*RefreshPluginCatalog) Path() string { return "/catalog/refresh" }

// Output 接纳与启动阶段相同的后台同步，不将供应商下载绑定到HTTP请求生命周期。
func (*RefreshPluginCatalog) Output(ctx *gin.Context) (interface{}, error) {
	if err := catalog.RefreshInBackground(global.PluginCatalogSource()); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
