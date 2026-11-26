package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/internal/workspacefiles"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 取消暂存
type GitUnstage struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body workspacefiles.GitIndexRequest `in:"body"`
}

// Path 返回仓库修改地址。
func (*GitUnstage) Path() string { return "/git/unstage" }

// Output 按当前工作树状态执行一次明确修改。
func (g *GitUnstage) Output(ctx *gin.Context) (interface{}, error) {
	if err := files.ChangeIndex(ctx.Request.Context(), middleware.AuthSubject(ctx), g.Body, "unstage"); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
