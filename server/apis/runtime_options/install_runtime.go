package runtime_options

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 安装或更新智能体
type InstallRuntime struct {
	ginx.MethodPost
	// 智能体标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
}

// Path 返回智能体受管安装资源路径。
func (*InstallRuntime) Path() string { return "/:runtimeId/installation" }

// Output 安装或更新智能体，后台任务独立于当前请求。
func (o *InstallRuntime) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.RuntimeEnvironmentService.InstallRuntime(ctx, o.RuntimeID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
