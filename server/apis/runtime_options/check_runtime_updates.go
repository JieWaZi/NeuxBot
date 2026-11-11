package runtime_options

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 检查智能体更新
type CheckRuntimeUpdates struct {
	ginx.MethodPost
	// 智能体标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
}

// Path 返回检查智能体更新接口路径。
func (*CheckRuntimeUpdates) Path() string { return "/:runtimeId/check-updates" }

// Output 通过单一智能体维护所有者检查智能体更新。
func (o *CheckRuntimeUpdates) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.RuntimeEnvironmentService.CheckRuntimeUpdates(ctx, o.RuntimeID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
