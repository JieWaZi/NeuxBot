package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取仓库比较
type GitComparison struct {
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 授权根目录
	Root string `in:"query" name:"root"`
	// 仓库目录
	Repository string `in:"query" name:"repository"`
	// 比较基准
	Base string `in:"query" name:"base"`
	// 相对路径
	RelativePath string `in:"query" name:"path"`
	// 忽略空白
	IgnoreWhitespace bool `in:"query" name:"ignoreWhitespace"`
}

// Path 返回仓库比较地址。
func (*GitComparison) Path() string { return "/git/comparison" }

// Output 读取授权仓库的比较摘要或单文件补丁。
func (g *GitComparison) Output(ctx *gin.Context) (interface{}, error) {
	value, err := files.Comparison(ctx, middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.Repository, g.Base, g.RelativePath, g.IgnoreWhitespace)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
