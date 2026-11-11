package aiaccount

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
)

func TestCodexSystemAccountCredentialBinding(t *testing.T) {
	ctx := t.Context()
	home := t.TempDir()
	canonical, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	request := agent.AccountRequest{Provider: constants.RuntimeIDCodex, AccountID: agent.SystemAccountID, CWD: t.TempDir()}
	firstToken := writeSystemAuthFixture(t, home, "user-a", "workspace-a", "first")
	first, err := ResolveCodexSystemAccount(ctx, request, home)
	if err != nil || first.ConfigHome != canonical || first.Subject == "" || first.AuthGeneration == 0 {
		t.Fatalf("resolve = %+v, %v", first, err)
	}
	nextToken := writeSystemAuthFixture(t, home, "user-a", "workspace-a", "rotated")
	next, err := ResolveCodexSystemAccount(ctx, request, home)
	if err != nil || !next.Equal(first) {
		t.Fatalf("ordinary refresh changed selection: %+v, %v", next, err)
	}
	lease, err := AcquireCodexSystemAccount(ctx, first)
	if err != nil || lease.ConfigHome != canonical || lease.VerifyCodexCredential == nil {
		t.Fatalf("acquire = %v", err)
	}
	if err := lease.VerifyCodexCredential(ctx, sha256.Sum256([]byte(firstToken))); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("stale native token accepted: %v", err)
	}
	if err := lease.VerifyCodexCredential(ctx, sha256.Sum256([]byte(nextToken))); err != nil {
		t.Fatalf("matching native token rejected: %v", err)
	}
	writeSystemAuthFixture(t, home, "user-b", "workspace-a", "switch")
	changed, err := ResolveCodexSystemAccount(ctx, request, home)
	if err != nil || changed.Subject == first.Subject {
		t.Fatalf("same-workspace user switch: %+v, %v", changed, err)
	}
	if _, err := AcquireCodexSystemAccount(ctx, first); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("stale frozen subject accepted: %v", err)
	}
	writeSystemAuthFixture(t, home, "user-b", "workspace-b", "switch")
	workspace, err := ResolveCodexSystemAccount(ctx, request, home)
	if err != nil || workspace.Subject == changed.Subject {
		t.Fatalf("same-user workspace switch: %+v, %v", workspace, err)
	}
}

func TestCodexSystemAccountScopeAndValidation(t *testing.T) {
	ctx := t.Context()
	home := t.TempDir()
	writeSystemAuthFixture(t, home, "user-a", "workspace-a", "token")
	t.Setenv("CODEX_HOME", home)
	request := agent.AccountRequest{Provider: constants.RuntimeIDCodex, AccountID: agent.SystemAccountID}
	first, err := ResolveCodexSystemAccount(ctx, request, "")
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	writeSystemAuthFixture(t, other, "user-a", "workspace-a", "token")
	second, err := ResolveCodexSystemAccount(ctx, request, other)
	if err != nil || second.ConfigHome == first.ConfigHome || second.Subject == first.Subject {
		t.Fatalf("explicit root did not isolate binding: %+v, %v", second, err)
	}
	invalid := first
	invalid.ConfigHome = second.ConfigHome
	if _, err := AcquireCodexSystemAccount(ctx, invalid); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("different frozen root accepted: %v", err)
	}
	invalid = first
	invalid.AuthGeneration++
	if _, err := AcquireCodexSystemAccount(ctx, invalid); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("unknown authority generation accepted: %v", err)
	}
	managed := request
	managed.AccountID = "managed"
	if _, err := ResolveCodexSystemAccount(ctx, managed, home); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("managed account accepted: %v", err)
	}
	unknown, err := ResolveCodexSystemAccount(ctx, request, t.TempDir())
	if err != nil {
		t.Fatalf("unobserved native login was blocked before execution: %v", err)
	}
	native, err := AcquireCodexSystemAccount(ctx, unknown)
	if err != nil || native.VerifyCodexCredential != nil || native.VerifyLogin == nil {
		t.Fatalf("unknown login required native authentication proof: %v", err)
	}
	if err := native.VerifyLogin(ctx); err != nil {
		t.Fatalf("local selection check required native login: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ResolveCodexSystemAccount(cancelled, request, home); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func writeSystemAuthFixture(t *testing.T, home, subject, workspace, nonce string) string {
	t.Helper()
	makeToken := func(kind string) string {
		payload, err := json.Marshal(map[string]any{
			"sub": subject, "nonce": nonce + kind,
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": workspace},
		})
		if err != nil {
			t.Fatal(err)
		}
		return "test-header." + base64.RawURLEncoding.EncodeToString(payload) + ".test-signature"
	}
	accessToken := makeToken("access")
	body, err := json.Marshal(map[string]any{
		"auth_mode": "chatgpt",
		"tokens":    codexLiveTokens{IDToken: makeToken("id"), AccessToken: accessToken, RefreshToken: "fixture-refresh", AccountID: workspace},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "auth.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	return accessToken
}
