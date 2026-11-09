package ai_accounts

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestUpdateActiveAccountAllowsLabelsAndDefaultsButProtectsCredentials(t *testing.T) {
	directory := t.TempDir()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(directory, "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := controllers.NewAIAccountController(database.DB)
	environments := controllers.NewRuntimeEnvironmentController(database.DB)
	if err := environments.Ensure(t.Context(), models.RuntimeEnvironment{ID: "claude", RuntimeID: constants.RuntimeIDClaude, BaseRuntimeID: constants.RuntimeIDClaude}); err != nil {
		t.Fatal(err)
	}
	service := aiaccount.New(repo, directory)
	oldRepo, oldService := global.AIAccountControllerManager, global.AIAccountService
	oldEnvironments := global.RuntimeEnvironmentControllerManager
	global.AIAccountControllerManager, global.AIAccountService = repo, service
	global.RuntimeEnvironmentControllerManager = environments
	t.Cleanup(func() {
		global.AIAccountControllerManager, global.AIAccountService = oldRepo, oldService
		global.RuntimeEnvironmentControllerManager = oldEnvironments
	})
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", RuntimeID: constants.RuntimeIDClaude, Name: "before", AuthMode: constants.AccountAuthModeAPIKey, SelectedModels: []string{"model"}, Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: "https://example.invalid"}}
	if err := repo.Create(t.Context(), account, "secret"); err != nil {
		t.Fatal(err)
	}
	selected, err := service.ResolveAccount(t.Context(), agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: account.ID, Model: "model", CWD: t.TempDir()}, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lease.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	closes := 0
	service.SetIdleSessionCloser(func(context.Context, string) error { closes++; return status_error.AIAccountBusy })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("PUT", "/", nil)
	request := UpdateAIAccount{AccountID: account.ID, Body: UpdateAIAccountRequest{RuntimeID: account.RuntimeID, Name: "after", Revision: 1, Endpoint: account.Endpoint, SelectedModels: account.SelectedModels, DefaultModel: "model"}}
	if _, err := request.Output(ctx); err != nil {
		t.Fatalf("ordinary active-account save failed: %v", err)
	}
	saved, err := repo.Get(t.Context(), account.ID)
	if err != nil || saved.Name != "after" || saved.DefaultModel != "model" || saved.AuthGeneration != 1 || saved.Revision != 2 || closes != 0 {
		t.Fatalf("ordinary save changed execution ownership: account=%+v closes=%d err=%v", saved, closes, err)
	}
	request.Body.Revision, request.Body.APIKey = saved.Revision, "replacement"
	if _, err := request.Output(ctx); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("active credential mutation allowed: %v", err)
	}
	if closes != 1 {
		t.Fatalf("credential mutation skipped owner check: %d", closes)
	}
}
