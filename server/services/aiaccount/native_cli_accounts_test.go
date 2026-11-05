package aiaccount

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestNativeCLIManagedLeases(t *testing.T) {
	service, _, _ := subscriptionFixture(t)
	service.dataDirectory = t.TempDir()
	ctx := t.Context()
	for index, tc := range []struct {
		// provider identifies the native runtime.
		provider string
		// protocol is the managed account protocol.
		protocol constants.AccountProtocol
		// rootKey is the environment variable read by the native CLI.
		rootKey string
	}{
		{constants.RuntimeIDKimi, constants.AccountProtocolOpenAICompletions, "KIMI_CODE_HOME"},
		{constants.RuntimeIDGemini, constants.AccountProtocolGoogleGenerativeAI, "GEMINI_CLI_HOME"},
		{constants.RuntimeIDGrok, constants.AccountProtocolOpenAIResponses, "GROK_HOME"},
		{constants.RuntimeIDOpenCode, constants.AccountProtocolOpenAIResponses, "XDG_DATA_HOME"},
		{constants.RuntimeIDPi, constants.AccountProtocolOpenAIResponses, "PI_CODING_AGENT_DIR"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			account := models.AIAccount{
				ID: fmt.Sprintf("%032x", index+1), Name: tc.provider, AuthMode: constants.AccountAuthModeAPIKey,
				Endpoints:      []models.AIAccountEndpoint{{Protocol: tc.protocol, BaseURL: "https://gateway.example.invalid/v1"}},
				SelectedModels: []string{"model-a", "model-b"}, DefaultModel: "model-a",
			}
			if err := service.accounts.Create(ctx, account, "test-key"); err != nil {
				t.Fatal(err)
			}
			cwd := t.TempDir()
			model := accountconfig.NativeModelID(tc.provider, "model-b")
			selected, err := service.ResolveAccount(ctx, agent.AccountRequest{
				Provider: tc.provider, AccountID: account.ID, CWD: cwd, Model: model,
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			lease, err := service.AcquireAccount(ctx, selected)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close(ctx)
			if selected.Model != model || lease.ConfigHome != selected.ConfigHome ||
				!strings.HasPrefix(lease.Environment[tc.rootKey], lease.ConfigHome+string(filepath.Separator)) {
				t.Fatal("managed configuration did not stay in the admitted account scope")
			}
			if tc.provider == constants.RuntimeIDGrok && lease.Environment["XAI_API_KEY"] != "" {
				t.Fatal("managed key became a first-party Grok key")
			}
			if tc.provider == constants.RuntimeIDGemini {
				path := filepath.Join(lease.Environment[tc.rootKey], ".gemini", "trustedFolders.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				canonical, err := filepath.EvalSymlinks(cwd)
				if err != nil {
					t.Fatal(err)
				}
				var trusted map[string]string
				if err := json.Unmarshal(data, &trusted); err != nil || trusted[canonical] != "TRUST_FOLDER" {
					t.Fatal("Gemini trust did not target the admitted workspace")
				}
			}
			if _, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
				t.Fatalf("native process did not hold the account lease: %v", err)
			}
			if err := lease.Close(ctx); err != nil {
				t.Fatal(err)
			}
			current, err := service.accounts.Get(ctx, account.ID)
			if err != nil {
				t.Fatal(err)
			}
			current.DefaultModel = "model-b"
			if err := service.accounts.Update(ctx, current, ""); err != nil {
				t.Fatal(err)
			}
			if _, err := service.AcquireAccount(ctx, selected); !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
				t.Fatalf("accepted stale account revision: %v", err)
			}
			fresh, err := service.ResolveAccount(ctx, selected, "")
			if err != nil {
				t.Fatal(err)
			}
			previous, err := service.accounts.Get(ctx, account.ID)
			if err != nil {
				t.Fatal(err)
			}
			var invalidated bool
			service.SetAccountAuthInvalidator(func(_ context.Context, provider, id string, generation uint64) {
				invalidated = invalidated || (provider == tc.provider && id == account.ID && generation == fresh.AuthGeneration)
			})
			defer service.SetAccountAuthInvalidator(nil)
			if err := service.accounts.Update(ctx, previous, "new-key"); err != nil {
				t.Fatal(err)
			}
			service.InvalidateAccountAuth(ctx, previous)
			if !invalidated {
				t.Fatal("credential rotation did not invalidate this native provider")
			}
			if _, err := service.AcquireAccount(ctx, fresh); !errors.Is(err, status_error.RuntimeAuthRequired) {
				t.Fatalf("accepted a retired credential generation: %v", err)
			}
		})
	}
}

func TestNativeCLISystemLeaseFencesEnvironmentAndNativeFiles(t *testing.T) {
	setNativeCLITestHome(t)
	t.Setenv("GROK_HOME", "")
	t.Setenv("XAI_API_KEY", "native-first")
	t.Setenv("OPENAI_API_KEY", "unrelated")
	service := New(nil)
	request := agent.AccountRequest{Provider: constants.RuntimeIDGrok, AccountID: agent.SystemAccountID, CWD: t.TempDir()}
	first, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Environment["XAI_API_KEY"] != "native-first" || lease.Environment["OPENAI_API_KEY"] != "" {
		t.Fatal("native launch inherited the wrong provider credentials")
	}
	t.Setenv("XAI_API_KEY", "native-second")
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("credential change escaped the pre-prompt fence: %v", err)
	}
	piHome := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", piHome)
	request.Provider = constants.RuntimeIDPi
	pi, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(piHome, "auth.json"), []byte(`{"anthropic":{"type":"api_key","key":"fixture"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcquireAccount(t.Context(), pi); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("native auth file change escaped admission fence: %v", err)
	}
}

func TestOpenCodeSystemLeaseFreezesAllXDGRoots(t *testing.T) {
	setNativeCLITestHome(t)
	t.Setenv("OPENCODE_CONFIG", "")
	t.Setenv("OPENCODE_DB", "")
	roots := map[string]string{}
	for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "OPENCODE_CONFIG_DIR"} {
		roots[key] = t.TempDir()
		t.Setenv(key, roots[key])
	}
	service := New(nil)
	selected, err := service.ResolveAccount(t.Context(), agent.AccountRequest{
		Provider: constants.RuntimeIDOpenCode, AccountID: agent.SystemAccountID, CWD: t.TempDir(),
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range roots {
		if lease.Environment[key] != value {
			t.Fatalf("native OpenCode lost %s", key)
		}
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, err := service.AcquireAccount(t.Context(), selected); !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
		t.Fatalf("native OpenCode accepted a changed state root: %v", err)
	}
}

func TestOpenCodeSystemLeaseReadsActiveCredentialIdentity(t *testing.T) {
	setNativeCLITestHome(t)
	for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG"} {
		t.Setenv(key, "")
	}
	database := filepath.Join(t.TempDir(), "opencode.db")
	t.Setenv("OPENCODE_DB", database)
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE credential (id text primary key, connector_id text, active integer, value text)`,
		`CREATE TABLE session (id text primary key)`,
		`INSERT INTO credential VALUES ('first', 'provider-a', 1, 'secret'), ('second', 'provider-a', 0, 'second-secret')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	service := New(nil)
	selected, err := service.ResolveAccount(t.Context(), agent.AccountRequest{
		Provider: constants.RuntimeIDOpenCode, AccountID: agent.SystemAccountID, CWD: t.TempDir(),
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO session VALUES ('session')`,
		`UPDATE credential SET value = 'refreshed' WHERE id = 'first'`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatalf("session write or token refresh changed the native principal: %v", err)
	}
	if _, err := db.Exec(`UPDATE credential SET active = CASE WHEN id = 'first' THEN 0 ELSE 1 END`); err != nil {
		t.Fatal(err)
	}
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("native OpenCode account switch escaped the fence: %v", err)
	}
}

func setNativeCLITestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
}

func TestGeminiSystemLeaseDistinguishesAccessRefreshFromAccountChange(t *testing.T) {
	setNativeCLITestHome(t)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	root := t.TempDir()
	t.Setenv("GEMINI_CLI_HOME", root)
	profile := filepath.Join(root, ".gemini")
	if err := os.Mkdir(profile, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(profile, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("settings.json", `{"security":{"auth":{"selectedType":"oauth-personal"}},"model":{"name":"before"}}`)
	write("oauth_creds.json", `{"access_token":"first","refresh_token":"grant"}`)
	service := New(nil)
	request := agent.AccountRequest{Provider: constants.RuntimeIDGemini, AccountID: agent.SystemAccountID, CWD: t.TempDir()}
	selected, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	write("settings.json", `{"security":{"auth":{"selectedType":"oauth-personal"}},"model":{"name":"after"}}`)
	write("oauth_creds.json", `{"access_token":"second","refresh_token":"grant"}`)
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatalf("native refresh and model choice invalidated the account: %v", err)
	}
	write("oauth_creds.json", `{"access_token":"third","refresh_token":"other-grant"}`)
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("native grant replacement escaped the fence: %v", err)
	}
}
