package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 取消暂存请求
type GitUnstageRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 授权根目录
	Root string `json:"root"`
	// 仓库目录
	Repository string `json:"repository"`
	// 相对路径
	RelativePath string `json:"relativePath" validate:"required"`
}

// 取消暂存
type GitUnstage struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body GitUnstageRequest `in:"body"`
}

// Path 返回仓库修改地址。
func (*GitUnstage) Path() string { return "/git/unstage" }

// Output 按当前工作树状态执行一次明确修改。
func (g *GitUnstage) Output(ctx *gin.Context) (interface{}, error) {
	if err := files.ChangeIndex(ctx, middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Root, g.Body.Repository, g.Body.RelativePath, "unstage"); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
