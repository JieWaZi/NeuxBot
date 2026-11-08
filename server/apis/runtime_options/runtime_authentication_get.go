package runtime_options

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取系统账号原生认证状态
type GetRuntimeAuthentication struct {
	ginx.MethodGet
	// 智能体标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
	// 重新读取原生能力
	Refresh bool `in:"query" name:"refresh"`
}

// Path 返回系统原生认证资源路径。
func (*GetRuntimeAuthentication) Path() string { return "/:runtimeId/authentication" }

// Output 读取真实原生认证能力，不启动交互登录或创建受管账号。
func (o *GetRuntimeAuthentication) Output(ctx *gin.Context) (interface{}, error) {
	view, err := global.RuntimeEnvironmentService.AuthenticationSnapshot(ctx, o.RuntimeID, o.Refresh)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return view, nil
}
