package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取提交文件
type GitShow struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 授权根目录
	Root string `in:"query" name:"root"`
	// 仓库目录
	Repository string `in:"query" name:"repository"`
	// 提交版本
	Revision string `in:"query" name:"revision" validate:"required"`
	// 相对路径
	RelativePath string `in:"query" name:"relativePath" validate:"required"`
}

// Path 返回仓库操作地址。
func (*GitShow) Path() string { return "/git/show" }

// Output 在认证主体的冻结工作范围内执行操作。
func (g *GitShow) Output(ctx *gin.Context) (interface{}, error) {
	value, err := files.Show(ctx, middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.Repository, g.Revision, g.RelativePath)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return GitContent{Content: value}, nil
}
