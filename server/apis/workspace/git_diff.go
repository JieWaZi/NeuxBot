package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取文件差异
type GitDiff struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 授权根目录
	Root string `in:"query" name:"root"`
	// 仓库目录
	Repository string `in:"query" name:"repository"`
	// 相对路径
	RelativePath string `in:"query" name:"relativePath" validate:"required"`
	// 暂存差异
	Staged bool `in:"query" name:"staged"`
}

// Path 返回仓库操作地址。
func (*GitDiff) Path() string { return "/git/diff" }

// Output 在认证主体的冻结工作范围内执行操作。
func (g *GitDiff) Output(ctx *gin.Context) (interface{}, error) {
	value, err := files.Diff(ctx, middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.Repository, g.RelativePath, g.Staged)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return GitContent{Content: value}, nil
}
