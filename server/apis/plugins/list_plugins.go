package plugins

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 插件列表
type ListPlugins struct {
	ginx.MethodGet
	// 仅已安装
	InstalledOnly bool `in:"query" name:"installedOnly"`
	// 伙伴范围
	WakerID string `in:"query" name:"wakerId"`
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 当前页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// Path 返回全局库查询入口。
func (*ListPlugins) Path() string { return "" }

// Output 返回准确分页窗口和原资源派生状态。
func (o *ListPlugins) Output(ctx *gin.Context) (interface{}, error) {
	result, err := listPluginResources(ctx, o.WakerID, o.Query, o.PageNum, o.PageSize, o.InstalledOnly)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
