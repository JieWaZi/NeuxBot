package runtimes

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
	acp "github.com/JieWaZi/acp-go/sdk"
)

func TestCodexCredentialAgentPreservesNativeAdapterInterfaces(t *testing.T) {
	var wrapped any = &codexCredentialAgent{}
	if _, ok := wrapped.(interface {
		SetAgentConnection(*acp.AgentSideConnection)
	}); !ok {
		t.Fatal("凭据包装丢失了宿主连接注入")
	}
	if _, ok := wrapped.(acp.AgentLoader); !ok {
		t.Fatal("凭据包装丢失了会话恢复能力")
	}
	if _, ok := wrapped.(interface {
		HandleExtensionMethod(context.Context, string, json.RawMessage) (any, error)
	}); !ok {
		t.Fatal("凭据包装丢失了扩展调用")
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
	defer broker.Close()
	wrong, err := http.Post(broker.url+"wrong", "application/json", bytes.NewBufferString(`{"grant_type":"refresh_token","refresh_token":"old"}`))
	if err != nil {
		t.Fatal(err)
	}
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusForbidden || calls.Load() != 0 {
		t.Fatal("错误能力路径访问了凭据")
	}
	for _, denied := range []bool{true, false} {
		request, _ := http.NewRequest(http.MethodPost, broker.url, bytes.NewBufferString(`{"grant_type":"refresh_token","refresh_token":"old"}`))
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
	broker.Close()
	if response, err := http.Post(broker.url, "application/json", bytes.NewBufferString(`{}`)); err == nil {
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
	defer broker.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, broker.url, bytes.NewBufferString(`{"grant_type":"refresh_token","refresh_token":"old"}`))
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
	defer broker.Close()
	go func() {
		response, err := http.Post(broker.url, "application/json", bytes.NewBufferString(`{"grant_type":"refresh_token","refresh_token":"old"}`))
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
	go func() { broker.Close(); close(closed) }()
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
