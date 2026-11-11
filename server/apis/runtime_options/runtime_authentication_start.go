package runtime_options

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 原生登录请求
type AuthenticateRuntimeRequest struct {
	// 原生认证方法
	MethodID string `json:"methodId" validate:"max=200"`
}

// 登录系统原生账号
type AuthenticateRuntime struct {
	ginx.MethodPost
	// 智能体标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
	// 登录方法
	Body AuthenticateRuntimeRequest `in:"body"`
}

// Path 返回系统原生认证资源路径。
func (*AuthenticateRuntime) Path() string { return "/:runtimeId/authentication" }

// Output 接纳原生登录；返回任务身份以围栏后续取消，HTTP 断开不取消登录。
func (o *AuthenticateRuntime) Output(ctx *gin.Context) (interface{}, error) {
	view, err := global.RuntimeEnvironmentService.AuthenticateRuntime(ctx, o.RuntimeID, o.Body.MethodID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return view, nil
}
