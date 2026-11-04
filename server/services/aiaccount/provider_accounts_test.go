package aiaccount

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestManagedClaudeLeaseFencesCredentialRotation(t *testing.T) {
	service, _, _ := subscriptionFixture(t)
	service.dataDirectory = t.TempDir()
	ctx := t.Context()
	account := models.AIAccount{
		ID: "fedcba9876543210fedcba9876543210", Name: "Claude fixture", AuthMode: constants.AccountAuthModeAPIKey,
		Endpoints:      []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: "https://gateway.example.invalid"}},
		SelectedModels: []string{"fixture-default", "fixture-opus"}, DefaultModel: "fixture-default",
		ClaudeModelRoles: map[string]string{"opus": "fixture-opus"},
	}
	if err := service.accounts.Create(ctx, account, "test-old"); err != nil {
		t.Fatal(err)
	}
	old, err := service.ResolveAccount(ctx, agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: account.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close(ctx)
	canonical, err := filepath.EvalSymlinks(service.dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if lease.ConfigHome != old.ConfigHome || !strings.HasPrefix(lease.ConfigHome, canonical+string(filepath.Separator)) {
		t.Fatal("Claude key escaped its generation's private configuration root")
	}
	if lease.Environment["ANTHROPIC_AUTH_TOKEN"] != "test-old" || lease.Environment["ANTHROPIC_API_KEY"] != "" ||
		lease.Environment["ANTHROPIC_BASE_URL"] != "https://gateway.example.invalid" ||
		lease.Environment["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "fixture-opus" ||
		lease.Environment["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "fixture-default" {
		t.Fatal("the selected key and model roles did not reach the launch environment")
	}
	if _, err := os.Stat(filepath.Join(lease.ConfigHome, "settings.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed API keys must stay out of native configuration files: %v", err)
	}
	if _, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("an active Claude lease did not block mutation: %v", err)
	}
	if err := lease.Close(ctx); err != nil {
		t.Fatal(err)
	}
	release, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	previous, err := service.accounts.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	var invalidated bool
	service.SetAccountAuthInvalidator(func(_ context.Context, provider, id string, generation uint64) {
		invalidated = provider == constants.RuntimeIDClaude && id == account.ID && generation == old.AuthGeneration
	})
	if err := service.accounts.Update(ctx, previous, "test-new"); err != nil {
		t.Fatal(err)
	}
	service.InvalidateCodexAuth(ctx, previous)
	if !invalidated {
		t.Fatal("managed Claude credential mutation did not invalidate its previous generation")
	}
	release()
	if _, err := service.AcquireAccount(ctx, old); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("old credential selection remained usable: %v", err)
	}
	current, err := service.ResolveAccount(ctx, old, "")
	if err != nil {
		t.Fatal(err)
	}
	if current.ConfigHome == old.ConfigHome || current.AuthGeneration != old.AuthGeneration+1 || current.Subject != old.Subject {
		t.Fatal("Claude key replacement changed identity or shared a native home")
	}
}

func TestClaudeSystemLeaseRechecksSelectedNativePrincipal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a shell executable")
	}
	home, cwd := t.TempDir(), t.TempDir()
	alias := filepath.Join(t.TempDir(), "profile-alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n[ \"$*\" = '--fixture auth status --json' ] || exit 2\n[ -z \"$ANTHROPIC_API_KEY\" ] || exit 3\ncat \"$CLAUDE_CONFIG_DIR/status\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	writeStatus := func(account, organization string) {
		t.Helper()
		status := "{\"loggedIn\":true,\"authMethod\":\"claude.ai\",\"email\":\"owner@example.invalid\",\"orgId\":\"" + organization + "\"}"
		metadata := "{\"oauthAccount\":{\"accountUuid\":\"" + account + "\",\"organizationUuid\":\"" + organization + "\",\"emailAddress\":\"owner@example.invalid\"}}"
		if err := os.WriteFile(filepath.Join(home, "status"), []byte(status), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(metadata), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ANTHROPIC_API_KEY", "ambient-test-key")
	writeStatus("21d20551-7433-4a8c-85f1-000000000001", "4cf24d12-df92-4503-ae9b-000000000001")
	service := New(nil)
	request, err := service.ResolveAccount(t.Context(), agent.AccountRequest{
		Provider: constants.RuntimeIDClaude, AccountID: agent.SystemAccountID, Executable: cli, PrefixArgs: []string{"--fixture"}, CWD: cwd,
	}, alias)
	if err != nil {
		t.Fatal(err)
	}
	if request.Subject == "" || strings.Contains(request.Subject, "21d20551") || strings.Contains(request.Subject, "@") {
		t.Fatal("frozen subject is missing or exposes the native account")
	}
	lease, err := service.AcquireAccount(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Environment["ANTHROPIC_API_KEY"] != "" || lease.Environment["CLAUDE_CONFIG_DIR"] != alias || request.ConfigHome == alias {
		t.Fatal("native selection lost the exact lexical config directory or its canonical root")
	}
	writeStatus("21d20551-7433-4a8c-85f1-000000000001", "4cf24d12-df92-4503-ae9b-000000000002")
	if _, err := service.AcquireAccount(t.Context(), request); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("organization changed while keeping the display email: %v", err)
	}
	writeStatus("21d20551-7433-4a8c-85f1-000000000002", "4cf24d12-df92-4503-ae9b-000000000001")
	if _, err := service.AcquireAccount(t.Context(), request); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("account changed while keeping organization and display email: %v", err)
	}
}

func TestClaudeDefaultHomeDoesNotSelectASeparateKeychainScope(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a shell executable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	metadata := "{\"oauthAccount\":{\"accountUuid\":\"21d20551-7433-4a8c-85f1-000000000001\",\"organizationUuid\":\"4cf24d12-df92-4503-ae9b-000000000001\",\"emailAddress\":\"owner@example.invalid\"}}"
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(metadata), 0600); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nif printenv CLAUDE_CONFIG_DIR >/dev/null; then exit 3; fi\nprintf '%s' '{\"loggedIn\":true,\"authMethod\":\"claude.ai\",\"email\":\"owner@example.invalid\",\"orgId\":\"4cf24d12-df92-4503-ae9b-000000000001\"}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	service := New(nil)
	request, err := service.ResolveAccount(t.Context(), agent.AccountRequest{
		Provider: constants.RuntimeIDClaude, AccountID: agent.SystemAccountID, Executable: cli, CWD: t.TempDir(),
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if value, exists := lease.Environment["CLAUDE_CONFIG_DIR"]; !exists || value != "" {
		t.Fatal("default account does not request removal of an explicit CLAUDE_CONFIG_DIR")
	}
}

func TestCursorAPIKeyLeaseDoesNotUseNativeDashboard(t *testing.T) {
	service := New(nil)
	t.Setenv("CURSOR_API_KEY", "test-cursor-key")
	home := t.TempDir()
	request, err := service.ResolveAccount(t.Context(), agent.AccountRequest{
		Provider: constants.RuntimeIDCursor, AccountID: agent.SystemAccountID,
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Environment["CURSOR_API_KEY"] != "test-cursor-key" || lease.CursorQuota != nil {
		t.Fatal("API key launch did not exclusively select the key")
	}
	t.Setenv("CURSOR_API_KEY", "replacement-cursor-key")
	if _, err := service.AcquireAccount(t.Context(), request); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("ambient key replacement passed the frozen selection: %v", err)
	}
	if _, err := service.ResolveAccount(t.Context(), agent.AccountRequest{Provider: constants.RuntimeIDCursor, AccountID: "arbitrary-managed"}, home); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("unsupported Cursor managed account was accepted: %v", err)
	}
}

func TestCursorQuotaDoesNotFollowAChangedTokenOrNativePrincipal(t *testing.T) {
	ctx := t.Context()
	currentPrincipal, currentToken := "principal-one", "access-one"
	readPrincipal := func(context.Context) (string, error) { return currentPrincipal, nil }
	readToken := func(context.Context) (string, error) { return currentToken, nil }
	quota := bindCursorQuota(ctx, "principal-one", readPrincipal, readToken)
	if quota == nil {
		t.Fatal("quota could not bind to the selected native sign-in")
	}
	if token, err := quota.ReadAccessToken(ctx); err != nil || token != "access-one" {
		t.Fatalf("bound quota read failed: %v", err)
	}
	currentToken = "access-two"
	if _, err := quota.ReadAccessToken(ctx); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("quota accepted the replacement access token: %v", err)
	}
	currentToken, currentPrincipal = "access-one", "principal-two"
	if _, err := quota.ReadAccessToken(ctx); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("quota accepted a different principal: %v", err)
	}
}

func TestCursorSystemSubjectUsesNativeUserID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a shell executable")
	}
	home := t.TempDir()
	cli := filepath.Join(t.TempDir(), "cursor-agent")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n[ \"$*\" = '--fixture status --format json' ] || exit 2\ncat \"$CURSOR_CONFIG_DIR/status\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURSOR_API_KEY", "")
	writeStatus := func(id, email string) {
		t.Helper()
		body := `{"isAuthenticated":true,"userInfo":{"userId":"` + id + `","email":"` + email + `"}}`
		if err := os.WriteFile(filepath.Join(home, "status"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	service := New(nil)
	selection := agent.AccountRequest{Provider: constants.RuntimeIDCursor, AccountID: agent.SystemAccountID, Executable: cli, PrefixArgs: []string{"--fixture"}, CWD: t.TempDir()}
	writeStatus("fixture-user-1", "first@example.invalid")
	first, err := service.ResolveAccount(t.Context(), selection, home)
	if err != nil {
		t.Fatal(err)
	}
	writeStatus("fixture-user-1", "new-label@example.invalid")
	renamed, err := service.ResolveAccount(t.Context(), selection, home)
	if err != nil || renamed.Subject != first.Subject {
		t.Fatalf("an updated Cursor label changed the subject: %v", err)
	}
	writeStatus("fixture-user-2", "new-label@example.invalid")
	if _, err := service.AcquireAccount(t.Context(), first); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("a different Cursor user acquired the old subject: %v", err)
	}
}

func TestManagedClaudeAllowsNativeAliasesWithoutChangingAccountDefault(t *testing.T) {
	service, _, _ := subscriptionFixture(t)
	service.dataDirectory = t.TempDir()
	ctx := t.Context()
	account := models.AIAccount{
		ID: "fedcba9876543210fedcba9876543210", Name: "Claude role fixture", AuthMode: constants.AccountAuthModeAPIKey,
		Endpoints:      []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: constants.AIAccountAnthropicURL}},
		SelectedModels: []string{"fixture-default", "fixture-sonnet"}, DefaultModel: "fixture-default",
		ClaudeModelRoles: map[string]string{"sonnet": "fixture-sonnet"},
	}
	if err := service.accounts.Create(ctx, account, "test-key"); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"", "sonnet", "sonnet[1m]", "fixture-sonnet", "fixture-sonnet[1m]"} {
		if _, err := service.ValidateSelection(ctx, account.ID, constants.RuntimeIDClaude, model, ""); err != nil {
			t.Fatalf("configured Claude model %q was rejected while saving: %v", model, err)
		}
		resolved, err := service.ResolveAccount(ctx, agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: account.ID, Model: model}, "")
		if err != nil {
			t.Fatalf("configured Claude model %q was rejected at admission: %v", model, err)
		}
		lease, err := service.AcquireAccount(ctx, resolved)
		if err != nil {
			t.Fatal(err)
		}
		if lease.Environment["ANTHROPIC_MODEL"] != "fixture-default" ||
			lease.Environment["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "fixture-sonnet" {
			t.Fatalf("selecting %q rewrote the native account model map", model)
		}
		if err := lease.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, model := range []string{"unlisted[1m]", "default"} {
		if _, err := service.ResolveAccount(ctx, agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: account.ID, Model: model}, ""); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
			t.Fatalf("unauthorized Claude model %q was admitted: %v", model, err)
		}
	}
}
