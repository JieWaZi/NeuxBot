package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取提交差异
type GitCommitDiff struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 授权根目录
	Root string `in:"query" name:"root"`
	// 仓库目录
	Repository string `in:"query" name:"repository"`
	// 提交标识
	Hash string `in:"query" name:"hash" validate:"required"`
}

// Path 返回仓库操作地址。
func (*GitCommitDiff) Path() string { return "/git/commit-diff" }

// Output 在认证主体的冻结工作范围内执行操作。
func (g *GitCommitDiff) Output(ctx *gin.Context) (interface{}, error) {
	value, err := files.CommitDiff(ctx, middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.Repository, g.Hash)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return GitContent{Content: value}, nil
}
