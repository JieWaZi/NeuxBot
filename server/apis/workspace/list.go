package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 列出工作目录
type ListDirectory struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 相对路径
	RelativePath string `in:"query" name:"relativePath"`
	// 授权根目录
	Root string `in:"query" name:"root"`
}

// Path 返回工作目录列表地址。
func (*ListDirectory) Path() string { return "/list" }

// Output 返回授权目录的有界条目。
func (g *ListDirectory) Output(ctx *gin.Context) (interface{}, error) {
	entries, err := files.List(middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.RelativePath)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return entries, nil
}
