package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查看目录插件
type GetCatalogPlugin struct {
	ginx.MethodGet
	// 展示语言
	Locale string `in:"query" name:"locale"`
	// 目录条目
	Key string `in:"path" name:"key" validate:"required"`
}

// Path 返回只读目录详情。
func (*GetCatalogPlugin) Path() string { return "/catalog/:key" }

// Output 按需核验固定来源的公开资料，不下载归档、创建资源或安装。
func (o *GetCatalogPlugin) Output(ctx *gin.Context) (interface{}, error) {
	entry, _, err := catalog.ResolveDocumentation(global.PluginCatalogSource(), o.Key)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	details, err := catalog.ReadDetails(entry)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	entry.Package = details.ViewPackage(o.Locale)
	return entry, nil
}
