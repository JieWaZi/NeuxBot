package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"strings"
)

// 插件目录
type ListPluginCatalog struct {
	ginx.MethodGet
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 当前页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// Path 返回插件分发目录入口。
func (*ListPluginCatalog) Path() string { return "/catalog" }

// 插件目录响应
type ListPluginCatalogResponse struct {
	// 来源状态
	Source catalog.SourceState `json:"source"`
	// 本地条目
	Entries []catalog.Entry `json:"entries"`
	// 准确总数
	TotalSize int `json:"totalSize"`
	// 当前页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
}

// Output 读取轻量目录元数据筛选分页，不下载插件归档。
func (o *ListPluginCatalog) Output(ctx *gin.Context) (interface{}, error) {
	entries, err := catalog.List(global.PluginCatalogSource())
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	filtered := []catalog.Entry{}
	query := strings.ToLower(strings.TrimSpace(o.Query))
	for _, entry := range entries {
		searchable := entry.Package.Manifest.Name + " " + entry.Package.Manifest.Description
		for _, value := range entry.Package.Presentation.DisplayNames {
			searchable += " " + value
		}
		for _, value := range entry.Package.Presentation.LocalizedDescriptions {
			searchable += " " + value
		}
		if query == "" || strings.Contains(strings.ToLower(searchable), query) {
			filtered = append(filtered, entry)
		}
	}
	page := max(o.PageNum, 1)
	size := o.PageSize
	if size < 1 || size > 100 {
		size = 24
	}
	start := min((page-1)*size, len(filtered))
	end := min(start+size, len(filtered))
	return ListPluginCatalogResponse{Source: catalog.State(global.PluginCatalogSource()), Entries: filtered[start:end], TotalSize: len(filtered), PageNum: page, PageSize: size}, nil
}
