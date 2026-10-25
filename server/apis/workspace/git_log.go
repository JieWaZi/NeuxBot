package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 列出提交历史
type GitLog struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 授权根目录
	Root string `in:"query" name:"root"`
	// 仓库目录
	Repository string `in:"query" name:"repository"`
	// 条数上限
	Count int `in:"query" name:"count"`
	// 跳过条数
	Skip int `in:"query" name:"skip"`
}

// Path 返回仓库操作地址。
func (*GitLog) Path() string { return "/git/log" }

// Output 在认证主体的冻结工作范围内执行操作。
func (g *GitLog) Output(ctx *gin.Context) (interface{}, error) {
	value, err := files.Log(ctx, middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.Repository, g.Count, g.Skip)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
