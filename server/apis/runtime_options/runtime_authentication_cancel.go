package runtime_options

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 取消系统账号原生登录
type CancelRuntimeAuthentication struct {
	ginx.MethodDelete
	// 智能体标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
	// 登录任务标识
	JobID string `in:"query" name:"jobId" validate:"required,max=120"`
}

// Path 返回系统原生认证资源路径。
func (*CancelRuntimeAuthentication) Path() string { return "/:runtimeId/authentication" }

// Output 仅取消匹配任务，避免旧窗口影响其他窗口的新登录。
func (o *CancelRuntimeAuthentication) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.RuntimeEnvironmentService.CancelRuntimeAuthentication(ctx, o.RuntimeID, o.JobID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
