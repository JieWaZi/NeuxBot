package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 预检分支切换
type GitCheckoutPreview struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 授权根目录
	Root string `in:"query" name:"root"`
	// 仓库目录
	Repository string `in:"query" name:"repository"`
	// 分支名称
	Branch string `in:"query" name:"branch" validate:"required"`
}

// Path 返回仓库操作地址。
func (*GitCheckoutPreview) Path() string { return "/git/checkout-preview" }

// Output 在认证主体的冻结工作范围内执行操作。
func (g *GitCheckoutPreview) Output(ctx *gin.Context) (interface{}, error) {
	value, err := files.PreviewCheckout(ctx, middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.Repository, g.Branch)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
