package runtime_options

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取安装状态
type GetRuntimeInstallation struct {
	ginx.MethodGet
	// 智能体标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
}

// Path 返回智能体受管安装资源路径。
func (*GetRuntimeInstallation) Path() string { return "/:runtimeId/installation" }

// Output 获取安装状态，后台任务独立于当前请求。
func (o *GetRuntimeInstallation) Output(ctx *gin.Context) (interface{}, error) {
	result, err := global.RuntimeEnvironmentService.InstallationSnapshot(ctx, o.RuntimeID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
