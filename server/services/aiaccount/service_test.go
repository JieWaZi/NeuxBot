package aiaccount

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestAccountsAreIndependentAndOnlyFailAtExecution(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	accounts := controllers.NewAIAccountController(database.DB)
	service := New(accounts)
	service.Start(context.Background())
	t.Cleanup(service.Close)
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", Name: "Kimi Code", AuthMode: constants.AccountAuthModeAPIKey, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: "https://api.kimi.com/coding"}}}
	ctx := context.Background()
	if err := accounts.Create(ctx, account, "one-shared-key"); err != nil {
		t.Fatal(err)
	}
	if err := service.Validate(ctx, account.ID); err != nil {
		t.Fatal("association should only require an existing account")
	}
	claude, releaseClaude, err := service.PrepareQueuedLease(ctx, account.ID, constants.RuntimeIDClaude, t.TempDir(), "model")
	if err != nil {
		t.Fatal(err)
	}
	if claude.CloseAfterRun {
		t.Fatal("managed account forced the native owner to close after every turn")
	}
	if claude.AccountID != account.ID {
		t.Fatal("account identity was lost")
	}
	if _, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatal("mutation allowed during execution")
	}
	closeAccountLease(t, releaseClaude)
	release, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, _, err := service.PrepareQueuedLease(ctx, account.ID, constants.RuntimeIDCursor, t.TempDir(), "model"); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatal("unsupported adapter must fail at execution")
	}
	if _, _, err := service.PrepareQueuedLease(ctx, account.ID, constants.RuntimeIDCodex, t.TempDir(), "model"); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatal("a different protocol must fail at execution")
	}
}

func TestExclusiveReclaimsIdleSessionBeforeAccountMutation(t *testing.T) {
	service := New(nil)
	shared, err := service.acquire("account", false)
	if err != nil {
		t.Fatal(err)
	}
	service.SetIdleSessionCloser(func(context.Context, string) error { shared(); return nil })
	exclusive, err := service.Exclusive("account")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.acquire("account", false); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("mutation did not exclude execution: %v", err)
	}
	exclusive()
	shared, err = service.acquire("account", false)
	if err != nil {
		t.Fatal(err)
	}
	shared()
}

func TestExclusiveKeepsActiveAccountBusy(t *testing.T) {
	service := New(nil)
	shared, err := service.acquire("account", false)
	if err != nil {
		t.Fatal(err)
	}
	defer shared()
	service.SetIdleSessionCloser(func(context.Context, string) error { return status_error.AIAccountBusy })
	if _, err = service.Exclusive("account"); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("active account err=%v", err)
	}
	if service.readers["account"] != 1 {
		t.Fatal("active session reader was discarded")
	}
}
