package ai_accounts

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
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

func TestLogoutAIAccountRemovesOnlySelectedCodexCredential(t *testing.T) {
	dataDirectory := t.TempDir()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(dataDirectory, "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := controllers.NewAIAccountController(db.DB)
	service := aiaccount.New(repo, dataDirectory)
	previousRepo, previousService := global.AIAccountControllerManager, global.AIAccountService
	global.AIAccountControllerManager, global.AIAccountService = repo, service
	t.Cleanup(func() {
		global.AIAccountControllerManager, global.AIAccountService = previousRepo, previousService
	})

	first := models.AIAccount{
		ID: "0123456789abcdef0123456789abcdef", Name: "first", AuthMode: constants.AccountAuthModeSubscription,
		Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}},
	}
	second := first
	second.ID, second.Name = "fedcba9876543210fedcba9876543210", "second"
	authPath := func(id string) string {
		return filepath.Join(dataDirectory, "ai-accounts", constants.RuntimeIDCodex, id, "1", "auth.json")
	}
	for _, account := range []models.AIAccount{first, second} {
		if err := repo.Create(t.Context(), account, "fixture-only-secret"); err != nil {
			t.Fatal(err)
		}
		path := authPath(account.ID)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var invalidated bool
	service.SetCodexAuthInvalidator(func(ctx context.Context, id string, generation uint64) {
		if id != first.ID || generation != 1 || invalidated {
			t.Fatal("invalidated the wrong account or generation")
		}
		current, err := repo.Get(ctx, id)
		if err != nil || current.HasCredential || current.AuthGeneration != 2 {
			t.Fatal("invalidation preceded the credential commit")
		}
		invalidated = true
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("DELETE", "/", nil)
	if _, err := (&LogoutAIAccount{AccountID: first.ID, Revision: 2}).Output(ctx); !errors.Is(err, status_error.AIAccountRevisionConflict) {
		t.Fatalf("logout accepted a stale revision: %v", err)
	}
	if _, err := os.Stat(authPath(first.ID)); err != nil {
		t.Fatal("stale logout removed the credential projection")
	}
	if _, err := (&LogoutAIAccount{AccountID: first.ID, Revision: 1}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if !invalidated {
		t.Fatal("logout did not invalidate the previous generation")
	}
	if _, err := os.Stat(authPath(first.ID)); !os.IsNotExist(err) {
		t.Fatal("logout retained the selected credential projection")
	}
	if _, err := os.Stat(authPath(second.ID)); err != nil {
		t.Fatal("logout affected another account")
	}
	if secret, err := repo.Credential(t.Context(), second.ID); err != nil || secret != "fixture-only-secret" {
		t.Fatal("logout changed another account's database credential")
	}
}
