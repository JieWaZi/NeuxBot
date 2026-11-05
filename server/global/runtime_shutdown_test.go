package global

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestCloseRetainsNativeAccountUntilProviderCleanupConfirms(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "native-shutdown.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	accounts := aiaccount.New(controllers.NewAIAccountController(database.DB))
	accounts.Start(t.Context())
	t.Cleanup(accounts.Close)
	previousProviders, previousDB, previousAccounts := AgentProviders, DB, AIAccountService
	allowRelease, released := false, 0
	providers := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{
		AcquireAccount: func(context.Context, agent.AccountRequest) (agent.AccountLease, error) {
			return agent.AccountLease{Close: func(context.Context) error {
				if _, err := accounts.StartLogin(context.Background(), "missing-shutdown-account"); !errors.Is(err, status_error.AIAccountNotFound) {
					t.Errorf("account service closed before native cleanup: %v", err)
				}
				if err := pool.Ping(); err != nil {
					t.Errorf("account dependency closed before native cleanup: %v", err)
				}
				if !allowRelease {
					return status_error.RuntimeShutdownFailed
				}
				released++
				return nil
			}}, status_error.RuntimeAuthRequired
		},
	})
	t.Cleanup(func() {
		allowRelease = true
		_ = providers.Close(context.Background())
		AgentProviders, DB, AIAccountService = previousProviders, previousDB, previousAccounts
		_ = pool.Close()
	})
	AgentProviders, DB, AIAccountService = providers, database.DB, accounts
	connection, err := providers.Open(t.Context(), agent.ProviderConnectionRequest{
		Account: agent.AccountRequest{Provider: "codex", AccountID: "fixture", Subject: "fixture-subject", CWD: t.TempDir()},
	}, agent.ProviderConnectionPorts{})
	if !errors.Is(err, status_error.RuntimeAuthRequired) || connection == nil {
		t.Fatalf("partial account acquisition = %v", err)
	}
	if err := connection.WaitOpenCleanup(t.Context()); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("initial cleanup = %v", err)
	}
	if err := Close(); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("failed native cleanup = %v", err)
	}
	if AgentProviders != providers || DB != database.DB || AIAccountService != accounts || released != 0 {
		t.Fatal("failed native cleanup discarded the owner or its dependency")
	}
	allowRelease = true
	if err := Close(); err != nil {
		t.Fatal(err)
	}
	if AgentProviders != nil || DB != nil || AIAccountService != nil || released != 1 {
		t.Fatal("native cleanup retry did not release exactly once")
	}
}
