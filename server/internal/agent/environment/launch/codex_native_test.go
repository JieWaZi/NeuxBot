package launch

import (
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"

	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestOfficialCodexRefreshUsesHostBroker 只使用合成凭据检查真实 CLI 的刷新协议；所有外部网络均禁用。
func TestOfficialCodexRefreshUsesHostBroker(t *testing.T) {
	binary := os.Getenv("ALLY_CODEX_OFFICIAL_CLI")
	if binary == "" {
		t.Skip("set ALLY_CODEX_OFFICIAL_CLI to verify native refresh against synthetic credentials")
	}
	home := t.TempDir()
	data, _ := codexAuthFixture(t, "old")
	_, tokens := codexAuthFixture(t, "new")
	if err := os.WriteFile(filepath.Join(home, "auth.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("cli_auth_credentials_store = \"file\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var refreshes atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config := Config{Native: native.Config{RuntimeID: "codex", Executable: binary, WorkingDirectory: home, Environment: []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "CODEX_HOME=" + home, "NO_PROXY=127.0.0.1,localhost", "HTTPS_PROXY=http://127.0.0.1:1", "HTTP_PROXY=http://127.0.0.1:1", "ALL_PROXY=http://127.0.0.1:1", "BROWSER=/usr/bin/false"}}, Refresh: func(ctx context.Context, previous string) (codexoauth.Tokens, error) {
		refreshes.Add(1)
		return tokens, nil
	}}
	prepared, err := Prepare(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close(context.Background())
	agent, err := prepared.New(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = agent.(interface{ Close(context.Context) error }).Close(closeCtx)
	}()
	if _, err := agent.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber}); err != nil {
		t.Fatal(err)
	}
	_, authErr := agent.Authenticate(ctx, acp.AuthenticateRequest{MethodId: "chat-gpt"})
	if refreshes.Load() == 0 {
		t.Fatalf("真实 CLI 未使用宿主刷新入口: %v", authErr)
	}
	// 合成令牌不能完成真实账号策略查询；只验收原生刷新请求、响应和落盘合同。
	updated, err := os.ReadFile(filepath.Join(home, "auth.json"))
	var native struct {
		// Tokens 是真实 CLI 写回的原生令牌束。
		Tokens codexoauth.Tokens `json:"tokens"`
	}
	if err != nil || json.Unmarshal(updated, &native) != nil || native.Tokens.RefreshToken != tokens.RefreshToken {
		t.Fatalf("真实 CLI 未保存宿主刷新回执: %v", err)
	}
}
