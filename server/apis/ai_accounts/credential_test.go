package ai_accounts

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
)

// accountCredentialReadStub 只实现此接口测试需要的账号读取操作。
type accountCredentialReadStub struct {
	// AIAccountController 保留测试未调用的控制器方法。
	controllers.AIAccountController
	// account 是模拟的账号元数据。
	account models.AIAccount
	// secret 是模拟的已解密密钥。
	secret string
}

func (s accountCredentialReadStub) Get(context.Context, string) (models.AIAccount, error) {
	return s.account, nil
}

func (s accountCredentialReadStub) Credential(context.Context, string) (string, error) {
	return s.secret, nil
}

func TestGetAIAccountCredentialOnlyRevealsAPIKeyAccounts(t *testing.T) {
	previous := global.AIAccountControllerManager
	t.Cleanup(func() { global.AIAccountControllerManager = previous })
	accountID := "0123456789abcdef0123456789abcdef"
	for _, mode := range []constants.AccountAuthMode{
		constants.AccountAuthModeAPIKey,
		constants.AccountAuthModeSubscription,
	} {
		global.AIAccountControllerManager = accountCredentialReadStub{
			account: models.AIAccount{ID: accountID, AuthMode: mode}, secret: "saved-secret",
		}
		writer := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(writer)
		response, err := (&GetAIAccountCredential{AccountID: accountID}).Output(ctx)
		if writer.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("credential response is cacheable")
		}
		if mode == constants.AccountAuthModeAPIKey {
			if err != nil || response.(GetAIAccountCredentialResponse).APIKey != "saved-secret" {
				t.Fatal("saved API Key was not returned for editing")
			}
		} else if !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
			t.Fatalf("subscription credential was exposed: %v", err)
		}
	}
}
