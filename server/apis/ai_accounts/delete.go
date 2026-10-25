package ai_accounts

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除账号
type DeleteAIAccount struct {
	// 声明账号或登录任务通过 DELETE 移除。
	ginx.MethodDelete
	// 账号标识
	AccountID string `in:"path" name:"accountId" validate:"required,len=32"`
	// 配置版本
	Revision int64 `in:"query" name:"revision" validate:"min=1"`
}

// Path 返回删除账号路径。
func (*DeleteAIAccount) Path() string { return "/:accountId" }

// Output 拒绝删除正在使用或绑定默认值的账号。
func (a *DeleteAIAccount) Output(ctx *gin.Context) (interface{}, error) {
	release, err := global.AIAccountService.Exclusive(a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer release()
	environments, err := global.RuntimeEnvironmentControllerManager.List(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	for _, environment := range environments {
		if environment.DefaultAccountID == a.AccountID {
			return nil, ginx.WithStack(status_error.AIAccountInUse)
		}
	}
	account, err := global.AIAccountControllerManager.Get(ctx, a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.AIAccountControllerManager.Delete(ctx, a.AccountID, a.Revision); err != nil {
		return nil, ginx.WithStack(err)
	}
	if account.AuthMode == constants.AccountAuthModeSubscription && len(account.Endpoints) == 1 && account.Endpoints[0].BaseURL == constants.AIAccountKimiURL {
		global.AIAccountService.RemoveKimiHome(account.ID)
	}
	return ginx.CommonSuccessResponse(), nil
}
