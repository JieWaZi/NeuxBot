package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 搜索工作文件
type SearchFiles struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 搜索文本
	Query string `in:"query" name:"query"`
	// 条数上限
	Limit int `in:"query" name:"limit"`
	// 搜索目录
	ScopePath string `in:"query" name:"scopePath"`
	// 授权根目录
	Root string `in:"query" name:"root"`
}

// Path 返回工作文件搜索地址。
func (*SearchFiles) Path() string { return "/search" }

// Output 使用有界遍历搜索冻结范围。
func (g *SearchFiles) Output(ctx *gin.Context) (interface{}, error) {
	matches, err := files.Search(ctx, middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.Query, g.ScopePath, g.Limit)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return matches, nil
}
