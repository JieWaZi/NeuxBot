package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 列出仓库分支
type GitBranches struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 授权根目录
	Root string `in:"query" name:"root"`
	// 仓库目录
	Repository string `in:"query" name:"repository"`
}

// Path 返回仓库操作地址。
func (*GitBranches) Path() string { return "/git/branches" }

// Output 在认证主体的冻结工作范围内执行操作。
func (g *GitBranches) Output(ctx *gin.Context) (interface{}, error) {
	value, err := files.Branches(ctx, middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.Repository)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
