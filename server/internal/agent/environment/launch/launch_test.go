package launch

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
)

func TestPrepareManagedCredentialNeverReadsNativeAuthority(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(filepath.Join(home, "missing"), filepath.Join(home, "auth.json")); err != nil {
		t.Fatal(err)
	}
	input := []string{"CODEX_HOME=" + home, "CODEX_REFRESH_TOKEN_URL_OVERRIDE=http://stale.invalid"}
	prepared, err := Prepare(context.Background(), Config{Native: native.Config{RuntimeID: "codex", Environment: input}})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close(context.Background())
	if _, exists := prepared.Credential().Environment["CODEX_REFRESH_TOKEN_URL_OVERRIDE"]; exists {
		t.Fatal("受管 API Key 继承了不属于当前所有者的刷新入口")
	}
	if input[1] != "CODEX_REFRESH_TOKEN_URL_OVERRIDE=http://stale.invalid" {
		t.Fatal("准备修改了调用方的环境快照")
	}
}

func TestPrepareFailureRetainsAccountRelease(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(filepath.Join(home, "missing"), filepath.Join(home, "auth.json")); err != nil {
		t.Fatal(err)
	}
	var releases atomic.Int32
	prepared, err := Prepare(context.Background(), Config{Native: native.Config{RuntimeID: "codex", Environment: []string{"CODEX_HOME=" + home}}, UseNativeAuth: true, Release: func(context.Context) error { releases.Add(1); return nil }})
	if err == nil || prepared == nil {
		t.Fatalf("认证准备失败未保留可关闭所有者: owner=%v error=%v", prepared, err)
	}
	if releases.Load() != 0 {
		t.Fatal("部分失败在所有者接管前释放了账号")
	}
	if err := prepared.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(context.Background()); err != nil || releases.Load() != 1 {
		t.Fatalf("重复关闭没有幂等释放账号: releases=%d error=%v", releases.Load(), err)
	}
}

func TestPreparedAgentCloseDrainsRefreshBeforeReleasingAccount(t *testing.T) {
	started, cancelled, persist := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releases atomic.Int32
	prepared, err := Prepare(context.Background(), Config{Native: native.Config{RuntimeID: "codex"}, Refresh: func(ctx context.Context, _ string) (codexoauth.Tokens, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-persist
		return codexoauth.Tokens{}, ctx.Err()
	}, Release: func(context.Context) error { releases.Add(1); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	url := prepared.Credential().Environment["CODEX_REFRESH_TOKEN_URL_OVERRIDE"]
	go func() {
		response, err := http.Post(url, "application/json", bytes.NewBufferString(`{"grant_type":"refresh_token","refresh_token":"old"}`))
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("刷新没有开始")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := prepared.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("未完成持久化时关闭应超时: %v", err)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("停止刷新入口没有取消已接纳请求")
	}
	if releases.Load() != 0 {
		t.Fatal("刷新尚未持久化就释放了账号")
	}
	close(persist)
	if err := prepared.Close(context.Background()); err != nil || releases.Load() != 1 {
		t.Fatalf("关闭重试未回收账号: releases=%d error=%v", releases.Load(), err)
	}
}

func TestPreparedAgentReleaseFailureCanRetry(t *testing.T) {
	var releases atomic.Int32
	prepared, err := Prepare(context.Background(), Config{Native: native.Config{RuntimeID: "claude"}, Release: func(context.Context) error {
		if releases.Add(1) == 1 {
			return status_error.AIAccountWorkspaceError
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(context.Background()); !errors.Is(err, status_error.AIAccountWorkspaceError) {
		t.Fatalf("回收失败未交回调用方: %v", err)
	}
	if err := prepared.Close(context.Background()); err != nil || releases.Load() != 2 {
		t.Fatalf("回收失败丢失了关闭所有权: releases=%d error=%v", releases.Load(), err)
	}
	if err := prepared.Close(context.Background()); err != nil || releases.Load() != 2 {
		t.Fatalf("回收成功后仍重复执行释放: releases=%d error=%v", releases.Load(), err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := prepared.Close(cancelled); err != nil || releases.Load() != 2 {
		t.Fatalf("已经关闭的资源不应受后续取消影响: releases=%d error=%v", releases.Load(), err)
	}
}

func TestPreparedAgentCredentialCopiesEnvironment(t *testing.T) {
	prepared, err := Prepare(context.Background(), Config{Native: native.Config{RuntimeID: "claude", Environment: []string{"A=first", "A=last", "B=kept"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close(context.Background())
	credential := prepared.Credential()
	credential.Environment["A"] = "changed"
	if values := prepared.Credential().Environment; values["A"] != "last" || values["B"] != "kept" {
		t.Fatal("调用方改写了冻结的启动环境")
	}
	if strings.Join(prepared.Environment(), "\n") != "A=last\nB=kept" {
		t.Fatal("启动环境未按最后覆盖值稳定冻结")
	}
}

func TestNativeFactoryFailureKeepsPreparedReleaseOwner(t *testing.T) {
	var releases atomic.Int32
	prepared, err := Prepare(context.Background(), Config{Native: native.Config{RuntimeID: "unsupported-fixture"}, Release: func(context.Context) error { releases.Add(1); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if agent, err := prepared.New(context.Background(), nil); err == nil || agent != nil {
		t.Fatalf("未知原生工厂没有失败: agent=%v error=%v", agent, err)
	}
	if releases.Load() != 0 {
		t.Fatal("工厂失败丢弃了仍归准备对象持有的资源")
	}
	if err := prepared.Close(context.Background()); err != nil || releases.Load() != 1 {
		t.Fatalf("工厂失败后的账号未回收: releases=%d error=%v", releases.Load(), err)
	}
}
