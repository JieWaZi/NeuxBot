package runtime_options

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 重启智能体
type RestartRuntime struct {
	ginx.MethodPost
	// 智能体标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
}

// Path 返回重启智能体接口路径。
func (*RestartRuntime) Path() string { return "/:runtimeId/restart" }

// Output 通过单一智能体维护所有者重启智能体。
func (o *RestartRuntime) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.RuntimeEnvironmentService.RestartRuntime(ctx, o.RuntimeID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
