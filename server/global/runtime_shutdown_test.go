package global

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecatalog "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/catalog"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

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
	registry, err := runtimecatalog.New(runtimecatalog.Config{})
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

	// 非普通认证文件让准备与退出对账确定性失败，不启动原生进程或网络调用。
	home := t.TempDir()
	authPath := filepath.Join(home, "auth.json")
	if err := os.Mkdir(authPath, 0o700); err != nil {
		t.Fatal(err)
	}
	released := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	registry.Detect(ctx, runtimecontract.DetectRequest{
		RuntimeID: runtimecontract.RuntimeIDCodex, BaseEnvironment: os.Environ(),
		LaunchOverride: runtimecontract.LaunchOverride{Binary: os.Args[0], Environment: map[string]string{"CODEX_HOME": home}},
		ReleaseResources: func() {
			released++
			if _, err := accounts.StartLogin(context.Background(), "missing-shutdown-account"); !errors.Is(err, status_error.AIAccountNotFound) {
				t.Errorf("account service was closed before runtime resources: %v", err)
			}
			if err := sqlDB.Ping(); err != nil {
				t.Errorf("database was closed before runtime resources: %v", err)
			}
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

	if err := os.Remove(authPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
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
