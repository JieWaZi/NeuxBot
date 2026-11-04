package ai_accounts

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 退出账号登录
type LogoutAIAccount struct {
	ginx.MethodDelete
	// 账号标识
	AccountID string `in:"path" name:"accountId" validate:"required,len=32"`
	// 配置版本
	Revision int64 `in:"query" name:"revision" validate:"min=1"`
}

// Path 返回退出受管账号登录的路径。
func (*LogoutAIAccount) Path() string { return "/:accountId/credential" }

// Output 清除受管 ChatGPT 登录，并保留账号及其选择。
func (a *LogoutAIAccount) Output(ctx *gin.Context) (interface{}, error) {
	release, err := global.AIAccountService.Exclusive(a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer release()
	account, err := global.AIAccountControllerManager.Get(ctx, a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if account.Revision != a.Revision {
		return nil, ginx.WithStack(status_error.AIAccountRevisionConflict)
	}
	if account.AuthMode != constants.AccountAuthModeSubscription || len(account.Endpoints) != 1 ||
		account.Endpoints[0].Protocol != constants.AccountProtocolOpenAIResponses ||
		account.Endpoints[0].BaseURL != constants.AIAccountOpenAIURL {
		return nil, ginx.WithStack(status_error.AIAccountConfigurationInvalid)
	}
	if account.HasCredential {
		if err := global.AIAccountService.RemoveCodexCredentialFiles(account); err != nil {
			return nil, ginx.WithStack(err)
		}
		if err := global.AIAccountControllerManager.ClearCredential(ctx, account.ID, account.Revision); err != nil {
			return nil, ginx.WithStack(err)
		}
		global.AIAccountService.InvalidateCodexAuth(ctx, account)
	}
	return ginx.CommonSuccessResponse(), nil
}
