package ai_accounts

import (
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func runtimeAccountFixture(t *testing.T) *gin.Context {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	previousAccounts, previousRuntimes, previousService := global.AIAccountControllerManager, global.RuntimeEnvironmentControllerManager, global.AIAccountService
	t.Cleanup(func() {
		global.AIAccountControllerManager, global.RuntimeEnvironmentControllerManager, global.AIAccountService = previousAccounts, previousRuntimes, previousService
		_ = pool.Close()
	})
	global.AIAccountControllerManager = controllers.NewAIAccountController(database.DB)
	global.RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(database.DB)
	global.AIAccountService = aiaccount.New(global.AIAccountControllerManager)
	for _, runtimeID := range []string{"codex", "codex-profile", "claude"} {
		base := runtimeID
		if runtimeID == "codex-profile" {
			base = constants.RuntimeIDCodex
		}
		if err := global.RuntimeEnvironmentControllerManager.Ensure(t.Context(), models.RuntimeEnvironment{ID: runtimeID, RuntimeID: runtimeID, BaseRuntimeID: base, Binary: base}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/", nil)
	return ctx
}

// TestAIAccountRuntimeOwnership 验证列表、创建与修改使用实际智能体身份，独立配置不会共享账号。
func TestAIAccountRuntimeOwnership(t *testing.T) {
	ctx := runtimeAccountFixture(t)
	var firstID string
	for _, runtimeID := range []string{"codex", "codex-profile"} {
		create := CreateAIAccount{Body: CreateAIAccountRequest{RuntimeID: runtimeID, Name: "OpenAI", AuthMode: constants.AccountAuthModeAPIKey,
			Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}, APIKey: "fixture-key", SelectedModels: []string{"model"}}}
		if err := create.Validate(ctx); err != nil {
			t.Fatal(err)
		}
		result, err := create.Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if firstID == "" {
			firstID = result.(CreateAIAccountResponse).ID
		}
		listed, err := (&ListAIAccounts{RuntimeID: runtimeID}).Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		accounts := listed.(ListAIAccountsResponse).Resources
		if len(accounts) != 1 || accounts[0].RuntimeID != runtimeID || accounts[0].CredentialCiphertext != "" {
			t.Fatalf("runtime list leaked another profile or credentials: %+v", accounts)
		}
	}
	account, err := global.AIAccountControllerManager.Get(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}
	update := UpdateAIAccount{AccountID: account.ID, Body: UpdateAIAccountRequest{RuntimeID: "codex-profile", Name: account.Name, Endpoint: account.Endpoint, Revision: account.Revision}}
	if _, err := update.Output(ctx); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("account can move to a different profile: %v", err)
	}
	create := CreateAIAccount{Body: CreateAIAccountRequest{RuntimeID: "claude", Name: "wrong protocol", AuthMode: account.AuthMode, Endpoint: account.Endpoint, APIKey: "fixture"}}
	if _, err := create.Output(ctx); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("runtime accepted an incompatible account: %v", err)
	}
	create.Body.RuntimeID = "missing-runtime"
	if _, err := create.Output(ctx); !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("account accepted an unknown runtime: %v", err)
	}
	if err := global.RuntimeEnvironmentControllerManager.UpdateFields(ctx, account.RuntimeID, map[string]any{"default_account_id": account.ID, "default_model": "model"}); err != nil {
		t.Fatal(err)
	}
	update.Body.RuntimeID = account.RuntimeID
	update.Body.SelectedModels = []string{"different-model"}
	if _, err := update.Output(ctx); !errors.Is(err, status_error.AIAccountInUse) {
		t.Fatalf("account edit orphaned the runtime default model: %v", err)
	}
	if _, err := (&DeleteAIAccount{AccountID: account.ID, Revision: account.Revision}).Output(ctx); !errors.Is(err, status_error.AIAccountInUse) {
		t.Fatalf("deletion orphaned the runtime default account: %v", err)
	}
}
