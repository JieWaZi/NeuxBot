package ai_accounts

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 取消账号登录
type CancelAIAccountLogin struct {
	// 声明账号或登录任务通过 DELETE 移除。
	ginx.MethodDelete
	// 账号标识
	AccountID string `in:"path" name:"accountId" validate:"required,len=32"`
	// 登录标识
	LoginID string `in:"path" name:"loginId" validate:"required,len=32"`
}

// Path 返回账号授权操作路径。
func (*CancelAIAccountLogin) Path() string { return "/:accountId/login/:loginId" }

// Output 执行账号授权操作，仅向页面返回安全状态。
func (a *CancelAIAccountLogin) Output(ctx *gin.Context) (interface{}, error) {
	global.AIAccountService.CancelLogin(a.AccountID, a.LoginID)
	return ginx.CommonSuccessResponse(), nil
}
