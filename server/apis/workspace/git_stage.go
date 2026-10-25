package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 暂存文件请求
type GitStageRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 授权根目录
	Root string `json:"root"`
	// 仓库目录
	Repository string `json:"repository"`
	// 相对路径
	RelativePath string `json:"relativePath" validate:"required"`
}

// 暂存文件
type GitStage struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body GitStageRequest `in:"body"`
}

// Path 返回仓库修改地址。
func (*GitStage) Path() string { return "/git/stage" }

// Output 按当前工作树状态执行一次明确修改。
func (g *GitStage) Output(ctx *gin.Context) (interface{}, error) {
	if err := files.ChangeIndex(ctx, middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Root, g.Body.Repository, g.Body.RelativePath, "stage"); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
