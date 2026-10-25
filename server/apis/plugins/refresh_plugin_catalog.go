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

// Output 核验轻量目录，保留失败前的缓存并唤醒已有插件查询。
func (*RefreshPluginCatalog) Output(ctx *gin.Context) (interface{}, error) {
	source := global.PluginCatalogSource()
	err := catalog.Refresh(ctx.Request.Context(), source)
	// 远程来源由目录生命周期唯一通知；本地目录没有后台生命周期，在请求结束时通知。
	if catalog.State(source).State == "local" {
		publishProviderChange("")
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
