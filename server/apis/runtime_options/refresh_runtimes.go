package runtime_options

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 刷新运行时
type RefreshRuntimes struct {
	ginx.MethodPost
}

// Path 返回全量 Runtime 刷新接口路径。
func (r *RefreshRuntimes) Path() string { return "/refresh" }

// Output 重新探测全部 Runtime 并原子替换各自快照。
func (r *RefreshRuntimes) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.RuntimeEnvironmentService.RefreshAll(ctx); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// 刷新单个运行时
type RefreshRuntime struct {
	ginx.MethodPost
	// 运行时标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
}

// Path 返回单条 Runtime 刷新接口路径。
func (r *RefreshRuntime) Path() string { return "/:runtimeId/refresh" }

// Validate 规范化 Runtime 标识。
func (r *RefreshRuntime) Validate(*gin.Context) error {
	r.RuntimeID = strings.TrimSpace(r.RuntimeID)
	return nil
}

// Output 只重新探测指定 Runtime。
func (r *RefreshRuntime) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.RuntimeEnvironmentService.RefreshOne(ctx, r.RuntimeID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
