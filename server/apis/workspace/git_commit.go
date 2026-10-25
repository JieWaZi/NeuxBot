package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 提交暂存修改请求
type GitCommitRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 授权根目录
	Root string `json:"root"`
	// 仓库目录
	Repository string `json:"repository"`
	// 提交说明
	Message string `json:"message" validate:"required"`
}

// 提交暂存修改
type GitCommit struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body GitCommitRequest `in:"body"`
}

// Path 返回仓库修改地址。
func (*GitCommit) Path() string { return "/git/commit" }

// Output 按当前工作树状态执行一次明确修改。
func (g *GitCommit) Output(ctx *gin.Context) (interface{}, error) {
	if err := files.Commit(ctx, middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Root, g.Body.Repository, g.Body.Message); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
