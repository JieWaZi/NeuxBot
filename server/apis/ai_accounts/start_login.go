package ai_accounts

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 开始账号登录
type StartAIAccountLogin struct {
	// 声明该账号操作使用 POST 提交。
	ginx.MethodPost
	// 账号标识
	AccountID string `in:"path" name:"accountId" validate:"required,len=32"`
}

// Path 返回账号授权操作路径。
func (*StartAIAccountLogin) Path() string { return "/:accountId/login" }

// Output 执行账号授权操作，仅向页面返回安全状态。
func (a *StartAIAccountLogin) Output(ctx *gin.Context) (interface{}, error) {
	result, err := global.AIAccountService.StartLogin(ctx, a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
