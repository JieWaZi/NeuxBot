package authorization

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"testing"
)

// summaryDriver 将外部身份读取替换为受控结果，保留真实授权管理器。
type summaryDriver struct {
	// noticeDriver 复用无副作用的配置与退出实现。
	noticeDriver
	// status 控制真实核验结果和返回时序。
	status func() (Account, error)
}

func (d *summaryDriver) Status(context.Context, bool) (Account, error) { return d.status() }

func TestAccountSummaryInvalidationAndScopeIsolation(t *testing.T) {
	driver := &summaryDriver{status: func() (Account, error) {
		return Account{Configured: true, Available: true, ID: "reader", Scopes: []string{"read"}}, nil
	}}
	auth := New(Config{Driver: driver})
	if auth.CachedAccount().CheckedAt != 0 {
		t.Fatal("new identity was presented as checked")
	}
	result, err := auth.Status(context.Background(), true)
	if err != nil || result.CheckedAt == 0 {
		t.Fatal("real check did not timestamp summary", err)
	}
	result.Scopes[0] = "mutated"
	snapshot := auth.CachedAccount()
	snapshot.Scopes[0] = "other"
	if auth.CachedAccount().Scopes[0] != "read" {
		t.Fatal("summary aliases caller scope slice")
	}
	if err := auth.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := auth.CachedAccount(); got.Available || got.CheckedAt == 0 {
		t.Fatal("successful logout must be a known disconnected identity", got)
	}
	if _, err := auth.Status(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := auth.ReplaceDriver(driver); err != nil {
		t.Fatal(err)
	}
	if got := auth.CachedAccount(); got.Available || got.CheckedAt != 0 {
		t.Fatal("new version inherited checked identity", got)
	}
	if _, err := auth.Status(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	driver.status = func() (Account, error) { return Account{}, status_error.PluginAuthorizationFailed }
	if _, err := auth.Status(context.Background(), true); err == nil {
		t.Fatal("failed status swallowed error")
	}
	if got := auth.CachedAccount(); got.Available || got.CheckedAt != 0 {
		t.Fatal("failed check retained usable summary", got)
	}
}

func TestSlowStatusCannotRestoreSummaryAfterLogout(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	driver := &summaryDriver{status: func() (Account, error) { close(started); <-finish; return Account{Available: true, ID: "old"}, nil }}
	auth := New(Config{Driver: driver})
	done := make(chan struct{})
	go func() { defer close(done); _, _ = auth.Status(context.Background(), true) }()
	<-started
	if err := auth.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(finish)
	<-done
	if got := auth.CachedAccount(); got.Available || got.ID != "" {
		t.Fatal("slow status resurrected logged out identity", got)
	}
}

func TestOlderConcurrentStatusCannotOverwriteNewerSummary(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	driver := &summaryDriver{status: func() (Account, error) { close(started); <-finish; return Account{Available: true, ID: "old"}, nil }}
	auth := New(Config{Driver: driver})
	done := make(chan struct{})
	go func() { defer close(done); _, _ = auth.Status(context.Background(), true) }()
	<-started
	// 已取走的旧边界继续等待，新的驱动核验先返回。
	replacement := &summaryDriver{status: func() (Account, error) { return Account{Available: false, ID: "new"}, nil }}
	if err := auth.ReplaceDriver(replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Status(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	close(finish)
	<-done
	if got := auth.CachedAccount(); got.Available || got.ID != "new" {
		t.Fatal("old status overwrote new identity summary", got)
	}
	if err := auth.Configure(context.Background(), "app", "secret"); err != nil {
		t.Fatal(err)
	}
	if got := auth.CachedAccount(); got.CheckedAt != 0 || got.ID != "" {
		t.Fatal("changed configuration retained identity summary", got)
	}
}
