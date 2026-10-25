package plugins

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 伙伴插件列表
type ListWakerPlugins struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 当前页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// Path 返回伙伴资源入口。
func (*ListWakerPlugins) Path() string { return "/:wakerId/plugins" }

// Output 只返回当前伙伴自有或已经安装的插件。
func (o *ListWakerPlugins) Output(ctx *gin.Context) (interface{}, error) {
	result, err := listPluginResources(ctx, o.WakerID, o.Query, o.PageNum, o.PageSize, false)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
