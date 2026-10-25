package ai_accounts

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetAIAccountCredential 获取账号密钥。
type GetAIAccountCredential struct {
	ginx.MethodGet
	// AccountID 是账号标识。
	AccountID string `in:"path" name:"accountId" validate:"required,len=32"`
}

// GetAIAccountCredentialResponse 账号密钥响应。
type GetAIAccountCredentialResponse struct {
	// APIKey 是已解密密钥。
	APIKey string `json:"apiKey"`
}

func (*GetAIAccountCredential) Path() string { return "/:accountId/credential" }

func (a *GetAIAccountCredential) Output(ctx *gin.Context) (interface{}, error) {
	ctx.Header("Cache-Control", "no-store")
	ctx.Header("Pragma", "no-cache")
	ctx.Header("Vary", "Authorization")
	account, err := global.AIAccountControllerManager.Get(ctx, a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if account.AuthMode != constants.AccountAuthModeAPIKey {
		return nil, ginx.WithStack(status_error.AIAccountConfigurationInvalid)
	}
	key, err := global.AIAccountControllerManager.Credential(ctx, a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return GetAIAccountCredentialResponse{APIKey: key}, nil
}
