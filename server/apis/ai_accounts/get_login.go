package ai_accounts

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查询账号授权
type GetAIAccountLogin struct {
	// 声明只读授权快照。
	ginx.MethodGet
	// 账号标识
	AccountID string `in:"path" name:"accountId" validate:"required,len=32"`
	// 登录标识
	LoginID string `in:"path" name:"loginId" validate:"required,len=32"`
}

// Path 返回账号授权操作路径。
func (*GetAIAccountLogin) Path() string { return "/:accountId/login/:loginId" }

// Output 读取 Core 作业的安全快照，不推进官方授权。
func (a *GetAIAccountLogin) Output(ctx *gin.Context) (interface{}, error) {
	result, err := global.AIAccountService.GetLogin(ctx, a.AccountID, a.LoginID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
