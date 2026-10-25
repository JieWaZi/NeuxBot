package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 切换仓库分支请求
type GitCheckoutRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 授权根目录
	Root string `json:"root"`
	// 仓库目录
	Repository string `json:"repository"`
	// 分支名称
	Branch string `json:"branch" validate:"required"`
	// 提交说明
	CommitMessage string `json:"commitMessage"`
}

// 切换仓库分支
type GitCheckout struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body GitCheckoutRequest `in:"body"`
}

// Path 返回仓库修改地址。
func (*GitCheckout) Path() string { return "/git/checkout" }

// Output 按当前工作树状态执行一次明确修改。
func (g *GitCheckout) Output(ctx *gin.Context) (interface{}, error) {
	branch, err := files.Checkout(ctx, middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Root, g.Body.Repository, g.Body.Branch, g.Body.CommitMessage)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return GitCheckoutResponse{Branch: branch}, nil
}

// 分支切换响应
type GitCheckoutResponse struct {
	// 当前分支
	Branch string `json:"branch"`
}
