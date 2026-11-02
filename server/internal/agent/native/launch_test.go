package native

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrepareManagedCredentialNeverReadsNativeAuthority(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(filepath.Join(home, "missing"), filepath.Join(home, "auth.json")); err != nil {
		t.Fatal(err)
	}
	input := []string{"CODEX_HOME=" + home, "CODEX_REFRESH_TOKEN_URL_OVERRIDE=http://stale.invalid"}
	prepared, err := PrepareLaunch(context.Background(), LaunchConfig{Native: AgentConfig{RuntimeID: "codex", Environment: input}})
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
	prepared, err := PrepareLaunch(context.Background(), LaunchConfig{Native: AgentConfig{RuntimeID: "codex", Environment: []string{"CODEX_HOME=" + home}}, UseNativeAuth: true, Release: func(context.Context) error { releases.Add(1); return nil }})
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
	prepared, err := PrepareLaunch(context.Background(), LaunchConfig{Native: AgentConfig{RuntimeID: "codex"}, Refresh: func(ctx context.Context, _ string) (codexoauth.Tokens, error) {
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
	prepared, err := PrepareLaunch(context.Background(), LaunchConfig{Native: AgentConfig{RuntimeID: "claude"}, Release: func(context.Context) error {
		if releases.Add(1) == 1 {
			return errTestRelease
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(context.Background()); !errors.Is(err, errTestRelease) {
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
	prepared, err := PrepareLaunch(context.Background(), LaunchConfig{Native: AgentConfig{RuntimeID: "claude", Environment: []string{"A=first", "A=last", "B=kept"}}})
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
	prepared, err := PrepareLaunch(context.Background(), LaunchConfig{Native: AgentConfig{RuntimeID: "unsupported-fixture"}, Release: func(context.Context) error { releases.Add(1); return nil }})
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

func TestCodexRefreshBrokerRoutesRefreshAndCloses(t *testing.T) {
	var calls atomic.Int32
	broker, err := newCodexRefreshBroker(func(ctx context.Context, previous string) (codexoauth.Tokens, error) {
		calls.Add(1)
		if previous != "old" {
			t.Errorf("错误旧令牌")
		}
		return codexoauth.Tokens{AccessToken: "new-access", RefreshToken: "new-refresh", IDToken: "id"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close(context.Background())
	wrong, err := http.Post(broker.URL()+"wrong", "application/json", bytes.NewBufferString(`{"grant_type":"refresh_token","refresh_token":"old"}`))
	if err != nil {
		t.Fatal(err)
	}
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusForbidden || calls.Load() != 0 {
		t.Fatal("错误能力路径访问了凭据")
	}
	for _, denied := range []bool{true, false} {
		request, _ := http.NewRequest(http.MethodPost, broker.URL(), bytes.NewBufferString(`{"grant_type":"refresh_token","refresh_token":"old"}`))
		request.Header.Set("Content-Type", "application/json")
		if denied {
			request.Header.Set("Origin", "https://example.invalid")
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if denied {
			response.Body.Close()
			if response.StatusCode != http.StatusForbidden || calls.Load() != 0 {
				t.Fatal("浏览器来源触发了刷新")
			}
			continue
		}
		var tokens codexoauth.Tokens
		err = json.NewDecoder(response.Body).Decode(&tokens)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || tokens.RefreshToken != "new-refresh" {
			t.Fatalf("刷新回执错误: status=%d error=%v", response.StatusCode, err)
		}
	}
	broker.Close(context.Background())
	if response, err := http.Post(broker.URL(), "application/json", bytes.NewBufferString(`{}`)); err == nil {
		response.Body.Close()
		t.Fatal("关闭后仍接受刷新请求")
	}
}

func TestCodexRefreshBrokerCancelsActiveCredentialRequest(t *testing.T) {
	started, finished := make(chan struct{}), make(chan struct{})
	broker, err := newCodexRefreshBroker(func(ctx context.Context, _ string) (codexoauth.Tokens, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return codexoauth.Tokens{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, broker.URL(), bytes.NewBufferString(`{"grant_type":"refresh_token","refresh_token":"old"}`))
	done := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(request)
		if response != nil {
			response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("刷新未开始")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("客户端未响应取消: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("客户端未结束")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("取消未传播到凭据权威")
	}
}

func TestCodexRefreshBrokerCloseWaitsForCredentialPersistence(t *testing.T) {
	started, cancelled, persist, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	broker, err := newCodexRefreshBroker(func(ctx context.Context, _ string) (codexoauth.Tokens, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-persist
		close(finished)
		return codexoauth.Tokens{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close(context.Background())
	go func() {
		response, err := http.Post(broker.URL(), "application/json", bytes.NewBufferString(`{"grant_type":"refresh_token","refresh_token":"old"}`))
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("刷新未开始")
	}
	closed := make(chan struct{})
	go func() { broker.Close(context.Background()); close(closed) }()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		close(persist)
		t.Fatal("关闭未取消正在等待的刷新")
	}
	select {
	case <-closed:
		close(persist)
		t.Fatal("凭据尚未保存，刷新服务就提前退出")
	case <-time.After(20 * time.Millisecond):
	}
	close(persist)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("保存结束后服务未退出")
	}
	select {
	case <-finished:
	default:
		t.Fatal("刷新处理仍在后台运行")
	}
}

func codexAuthFixture(t *testing.T, refresh string) ([]byte, codexoauth.Tokens) {
	t.Helper()
	claims, _ := json.Marshal(map[string]any{"sub": "fixture-user", "email": "fixture@example.invalid", "exp": time.Now().Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "fixture-account", "chatgpt_plan_type": "plus"}})
	jwt := "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	tokens := codexoauth.Tokens{IDToken: jwt, AccessToken: jwt, RefreshToken: refresh}
	data, _ := json.Marshal(map[string]any{"tokens": map[string]string{"id_token": jwt, "access_token": jwt, "refresh_token": refresh, "account_id": "fixture-account"}, "last_refresh": time.Now().UTC(), "extra": "preserved"})
	return data, tokens
}

func TestCodexFileRefreshRepairsDelayedNativeWriteWithoutReusingRotatedToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	original, _ := codexAuthFixture(t, "old")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	_, next := codexAuthFixture(t, "new")
	var refreshes atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { refreshes.Add(1); _ = json.NewEncoder(w).Encode(next) }))
	defer endpoint.Close()
	client := codexoauth.New()
	client.Origin = endpoint.URL
	first, err := codexFileRefresh(path, client)
	if err != nil {
		t.Fatal(err)
	}
	second, err := codexFileRefresh(path, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	// 普通原生进程可能在收到刷新响应后才落盘，旧进程不得使权威倒退。
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := second(context.Background(), "old")
	if err != nil || result.RefreshToken != "new" || refreshes.Load() != 1 {
		t.Fatalf("延迟回写导致重复刷新: calls=%d error=%v", refreshes.Load(), err)
	}
	data, err := os.ReadFile(path)
	var saved map[string]any
	if err != nil || json.Unmarshal(data, &saved) != nil || saved["extra"] != "preserved" {
		t.Fatal("原生认证字段丢失")
	}
	if saved["tokens"].(map[string]any)["refresh_token"] != "new" {
		t.Fatal("没有修复延迟写入的旧令牌")
	}
}

func TestSystemCredentialReconciliationPreservesExternalLoginAndLogout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	original, _ := codexAuthFixture(t, "old")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	_, rotated := codexAuthFixture(t, "rotated")
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(rotated) }))
	defer endpoint.Close()
	client := codexoauth.New()
	client.Origin = endpoint.URL
	refresh, err := codexFileRefresh(path, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := refresh(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	external, _ := codexAuthFixture(t, "external-login")
	if err := os.WriteFile(path, external, 0600); err != nil {
		t.Fatal(err)
	}
	unlock, err := codexFileCredentialGate(path).Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	actual, err := reconcileCodexFileAuth(path)
	unlock()
	if err != nil || !bytes.Equal(actual, external) {
		t.Fatalf("外部新登录被本进程旧快照覆盖: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := refresh(context.Background(), "rotated"); !isAuthenticationFault(err) {
		t.Fatalf("外部退出登录后仍能刷新旧身份: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("对账恢复了用户已删除的登录: %v", err)
	}
}

var errTestRelease = errors.New("test account release failure")

func isAuthenticationFault(err error) bool {
	var f *Fault
	return errors.As(err, &f) && f.Code == FaultAuthenticationRequired
}
