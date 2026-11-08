package aiaccount

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestAccountsRequireRuntimeOwnershipAndKeepExecutionLeases(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	accounts := controllers.NewAIAccountController(database.DB)
	service := New(accounts, t.TempDir())
	service.Start(context.Background())
	t.Cleanup(service.Close)
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", RuntimeID: constants.RuntimeIDClaude, Name: "Kimi Code", AuthMode: constants.AccountAuthModeAPIKey, SelectedModels: []string{"model"}, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: "https://api.kimi.com/coding"}}}
	ctx := context.Background()
	if err := accounts.Create(ctx, account, "one-shared-key"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRuntimeAccount(account, models.RuntimeEnvironment{RuntimeID: account.RuntimeID, BaseRuntimeID: constants.RuntimeIDClaude}); err != nil {
		t.Fatal("a compatible account in the selected runtime was rejected")
	}
	if err := ValidateRuntimeAccount(account, models.RuntimeEnvironment{RuntimeID: "claude-profile", BaseRuntimeID: constants.RuntimeIDClaude}); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("another profile accepted the account: %v", err)
	}
	request := agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: account.ID, Model: "model", CWD: t.TempDir()}
	resolved, err := service.ResolveAccount(ctx, request, "")
	if err != nil || resolved.AccountID != account.ID || resolved.Subject == "" {
		t.Fatalf("account identity was lost: %v", err)
	}
	claude, err := service.AcquireAccount(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatal("mutation allowed during execution")
	}
	closeAccountLease(t, claude.Close)
	release, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
	for _, provider := range []string{constants.RuntimeIDCursor, constants.RuntimeIDCodex} {
		request.Provider = provider
		if _, err := service.ResolveAccount(ctx, request, ""); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
			t.Fatalf("unsupported provider %s accepted the account: %v", provider, err)
		}
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
