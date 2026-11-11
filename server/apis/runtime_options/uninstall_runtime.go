package runtime_options

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 卸载智能体程序
type UninstallRuntime struct {
	ginx.MethodDelete
	// 智能体标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
}

// Path 返回与安装取消分离的程序资源。
func (*UninstallRuntime) Path() string { return "/:runtimeId/program" }

// Output 接纳程序卸载，保留账号、登录、伙伴和配置。
func (o *UninstallRuntime) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.RuntimeEnvironmentService.UninstallRuntime(ctx, o.RuntimeID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
