package global

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"

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
	previousProviders, previousDB := AgentProviders, DB
	allowRelease, released := false, 0
	providers := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{
		AcquireAccount: func(context.Context, agent.AccountRequest) (agent.AccountLease, error) {
			return agent.AccountLease{Close: func(context.Context) error {
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
		AgentProviders, DB = previousProviders, previousDB
		_ = pool.Close()
	})
	AgentProviders, DB = providers, database.DB
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
	if AgentProviders != providers || DB != database.DB || released != 0 {
		t.Fatal("failed native cleanup discarded the owner or its dependency")
	}
	allowRelease = true
	if err := Close(); err != nil {
		t.Fatal(err)
	}
	if AgentProviders != nil || DB != nil || released != 1 {
		t.Fatal("native cleanup retry did not release exactly once")
	}
}

// TestCloseRetainsRuntimeDependenciesUntilManagementCleanupConfirms 证明关闭失败可重试且依赖按实际所有权释放。
func TestCloseRetainsRuntimeDependenciesUntilManagementCleanupConfirms(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "shutdown.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	accounts := aiaccount.New(controllers.NewAIAccountController(database.DB))
	accounts.Start(context.Background())
	registry, err := agentapi.NewCatalog(agentapi.CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	previousCatalog, previousAccounts, previousDB := RuntimeCatalog, AIAccountService, DB
	t.Cleanup(func() {
		RuntimeCatalog, AIAccountService, DB = previousCatalog, previousAccounts, previousDB
		accounts.Close()
		_ = sqlDB.Close()
	})
	RuntimeCatalog, AIAccountService, DB = registry, accounts, database.DB

	// 非普通认证文件阻止原生启动；释放端口的失败独立模拟尚未完成的资源回收。
	home := t.TempDir()
	authPath := filepath.Join(home, "auth.json")
	if err := os.Mkdir(authPath, 0o700); err != nil {
		t.Fatal(err)
	}
	released := 0
	allowRelease := false
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	registry.Detect(ctx, agentapi.DetectRequest{
		RuntimeID: agentapi.RuntimeIDCodex, BaseEnvironment: os.Environ(), UseNativeAuth: true,
		LaunchOverride: agentapi.LaunchOverride{Binary: os.Args[0], Environment: map[string]string{"CODEX_HOME": home}},
		ReleaseResources: func(context.Context) error {
			if _, err := accounts.StartLogin(context.Background(), "missing-shutdown-account"); !errors.Is(err, status_error.AIAccountNotFound) {
				t.Errorf("account service was closed before runtime resources: %v", err)
			}
			if err := sqlDB.Ping(); err != nil {
				t.Errorf("database was closed before runtime resources: %v", err)
			}
			if !allowRelease {
				return status_error.RuntimeShutdownFailed
			}
			released++
			return nil
		},
	})
	if released != 0 {
		t.Fatal("fixture did not retain a failed management owner")
	}
	if err := Close(); !errors.Is(err, status_error.CoreShutdownFailed) {
		t.Fatalf("unconfirmed management cleanup must fail Core shutdown: %v", err)
	}
	if RuntimeCatalog != registry || AIAccountService != accounts || DB != database.DB || released != 0 {
		t.Fatalf("failed shutdown discarded runtime dependencies: catalog=%p accounts=%p db=%p released=%d", RuntimeCatalog, AIAccountService, DB, released)
	}
	if _, err := accounts.StartLogin(context.Background(), "missing-shutdown-account"); !errors.Is(err, status_error.AIAccountNotFound) {
		t.Fatalf("failed shutdown canceled the retained account service: %v", err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("failed shutdown closed the retained database: %v", err)
	}

	allowRelease = true
	if err := Close(); err != nil {
		t.Fatalf("second shutdown did not retry management cleanup: %v", err)
	}
	if RuntimeCatalog != nil || AIAccountService != nil || DB != nil || released != 1 {
		t.Fatalf("confirmed shutdown did not release once: catalog=%p accounts=%p db=%p released=%d", RuntimeCatalog, AIAccountService, DB, released)
	}
	if _, err := accounts.StartLogin(context.Background(), "missing-shutdown-account"); !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("confirmed shutdown left account service active: %v", err)
	}
	if err := sqlDB.Ping(); err == nil {
		t.Fatal("confirmed shutdown left database open")
	}
}
