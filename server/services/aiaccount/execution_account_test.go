package aiaccount

import (
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
)

func executionAccountFixture(t *testing.T, provider string) (agent.AccountRequest, string) {
	t.Helper()
	home := acpAccountTestHome(t)
	for _, key := range []string{"CURSOR_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CONFIG_DIR", "QODER_PERSONAL_ACCESS_TOKEN", "QODER_CONFIG_DIR"} {
		t.Setenv(key, "")
	}
	t.Setenv("AGENT_CLI_CREDENTIAL_STORE", "file")
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CURSOR_DATA_DIR", filepath.Join(home, "cursor-data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	trace := filepath.Join(home, "native-starts")
	t.Setenv("NEUX_EXECUTION_ACCOUNT_TRACE", trace)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return agent.AccountRequest{Provider: provider, AccountID: agent.SystemAccountID, Executable: executable, PrefixArgs: []string{"-test.run=^TestExecutionAccountProcess$", "--"}, CWD: home}, trace
}

func TestExecutionAccountProcess(t *testing.T) {
	trace := os.Getenv("NEUX_EXECUTION_ACCOUNT_TRACE")
	if trace == "" {
		return
	}
	arguments := strings.Join(os.Args, " ")
	file, err := os.OpenFile(trace, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(90)
	}
	_, _ = file.WriteString(arguments + "\n")
	_ = file.Close()
	if strings.Contains(arguments, "prompt") {
		_, _ = os.Stderr.WriteString("native authentication required\n")
		os.Exit(1)
	}
	_, _ = os.Stderr.WriteString("independent account probe forbidden\n")
	os.Exit(91)
}

func TestSystemExecutionAccountsDoNotProbeBeforeNativeAuthentication(t *testing.T) {
	for _, provider := range []string{constants.RuntimeIDCursor, constants.RuntimeIDClaude, constants.RuntimeIDQoder, constants.RuntimeIDWorkBuddy, constants.RuntimeIDCodex} {
		t.Run(provider, func(t *testing.T) {
			input, trace := executionAccountFixture(t, provider)
			service := New(nil)
			selected, err := service.ResolveAccount(t.Context(), input, "")
			if err != nil {
				t.Fatalf("local selection must leave native authentication to execution: %v", err)
			}
			lease, err := service.AcquireAccount(t.Context(), selected)
			if err != nil {
				t.Fatal(err)
			}
			if lease.VerifyLogin != nil {
				if err := lease.VerifyLogin(t.Context()); err != nil {
					t.Fatalf("local submission check required an online login: %v", err)
				}
			}
			if _, err := os.Stat(trace); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("account selection or submission launched an independent native process")
			}
			if lease.QoderLoginVerified || lease.CursorQuota != nil {
				t.Fatal("unobserved native identity was presented as authenticated")
			}
			command := exec.CommandContext(t.Context(), input.Executable, append(input.PrefixArgs, "prompt")...)
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "native authentication required") {
				t.Fatalf("the real native invocation must report signed-out state: %s (%v)", output, err)
			}
		})
	}
}

func TestExecutionCursorLocalAccountSwitchAndRefresh(t *testing.T) {
	input, _ := executionAccountFixture(t, constants.RuntimeIDCursor)
	config, err := cursorSystemConfiguration("")
	if err != nil {
		t.Fatal(err)
	}
	write := func(id, expiry string) {
		t.Helper()
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user|` + id + `","exp":` + expiry + `}`))
		if err := os.MkdirAll(filepath.Dir(config.authPath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config.authPath, []byte(`{"accessToken":"header.`+claims+`.signature"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("first", "100")
	service := New(nil)
	selected, err := service.ResolveAccount(t.Context(), input, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	write("first", "200")
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatalf("access-token refresh changed local account identity: %v", err)
	}
	write("second", "300")
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("local account switch reused a native binding: %v", err)
	}
}

func TestExecutionManagedClaudeSynchronizesWithoutNativeProbe(t *testing.T) {
	service, account, snapshot := claudeSubscriptionFixture(t)
	account = loginClaudeFixture(t, service, account)
	input, trace := executionAccountFixture(t, constants.RuntimeIDClaude)
	input.AccountID = account.ID
	service.claudeFactory = nil
	selected, err := service.ResolveAccount(t.Context(), input, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatalf("managed acquisition launched an account probe: %v", err)
	}
	refreshed := *snapshot
	refreshed.Credential = []byte(`{"claudeAiOauth":{"accessToken":"native-refreshed","refreshToken":"rotated","expiresAt":200}}`)
	if err := service.claudeCredentials.Write(t.Context(), selected.ConfigHome, refreshed); err != nil {
		t.Fatal(err)
	}
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	stored, err := service.claudeStoredCredential(t.Context(), account)
	if err != nil || !strings.Contains(string(stored.Credential), "native-refreshed") {
		t.Fatalf("native refresh was not persisted: %v", err)
	}
	if _, err := os.Stat(trace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("managed local synchronization launched a native account probe")
	}
}

func TestExecutionCursorLocalQuotaIdentityIsOptionalAndReadOnDemand(t *testing.T) {
	input, trace := executionAccountFixture(t, constants.RuntimeIDCursor)
	config, err := cursorSystemConfiguration("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(config.authPath), 0700); err != nil {
		t.Fatal(err)
	}
	path := config.authPath
	if err := os.WriteFile(path, []byte(`{"accessToken":"fixture-bearer","userInfo":{"userId":77}}`), 0600); err != nil {
		t.Fatal(err)
	}
	service := New(nil)
	selected, err := service.ResolveAccount(t.Context(), input, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	if lease.CursorQuota == nil || lease.CursorQuota.NativeUserID != "77" {
		t.Fatal("an explicitly saved numeric quota identity was discarded")
	}
	if err := os.WriteFile(path, []byte(`{"accessToken":"refreshed-bearer","userInfo":{"userId":77}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatalf("native refresh changed saved quota identity: %v", err)
	}
	token, err := lease.CursorQuota.ReadAccessToken(t.Context())
	if err != nil || token != "refreshed-bearer" {
		t.Fatalf("explicit quota read did not load the selected bearer on demand: %v", err)
	}
	if _, err := os.Stat(trace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("local quota selection launched an identity diagnostic")
	}
}

func TestExecutionClaudeDefaultMetadataTracksNativeHomeThroughConfigSymlink(t *testing.T) {
	input, trace := executionAccountFixture(t, constants.RuntimeIDClaude)
	actualConfig := t.TempDir()
	if err := os.Symlink(actualConfig, filepath.Join(input.CWD, ".claude")); err != nil {
		t.Skipf("symlink fixture unsupported: %v", err)
	}
	metadata := filepath.Join(input.CWD, ".claude.json")
	write := func(account string) {
		t.Helper()
		if err := os.WriteFile(metadata, []byte(`{"oauthAccount":{"accountUuid":"`+account+`","organizationUuid":"org"}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("first")
	service := New(nil)
	selected, err := service.ResolveAccount(t.Context(), input, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	write("second")
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("native HOME metadata switch escaped local identity checks: %v", err)
	}
	if _, err := os.Stat(trace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("metadata check launched a native diagnostic")
	}
}

func TestExecutionClaudeSystemMetadataPreservesNativeSizeBound(t *testing.T) {
	for _, filename := range []string{constants.ClaudeAccountMetadataFileName, constants.ClaudeLegacyMetadataFileName} {
		for _, size := range []int{(1 << 20) + 128, 16 << 20, (16 << 20) + 1} {
			t.Run(filename+"/"+strconv.Itoa(size), func(t *testing.T) {
				input, trace := executionAccountFixture(t, constants.RuntimeIDClaude)
				path := filepath.Join(input.CWD, filename)
				if filename == constants.ClaudeLegacyMetadataFileName {
					config := filepath.Join(input.CWD, ".claude")
					if err := os.Mkdir(config, 0700); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(config, filename)
				}
				prefix := `{"oauthAccount":{"accountUuid":"account-a","organizationUuid":"org-a"},"projects":"`
				suffix := `"}`
				document := prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
				if err := os.WriteFile(path, []byte(document), 0600); err != nil {
					t.Fatal(err)
				}
				service := New(nil)
				selected, err := service.ResolveAccount(t.Context(), input, "")
				if size > 16<<20 {
					if !errors.Is(err, status_error.AIAccountCredentialError) {
						t.Fatalf("oversized native metadata accepted: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("valid native metadata within 16 MiB blocked execution: %v", err)
				}
				lease, err := service.AcquireAccount(t.Context(), selected)
				if err != nil {
					t.Fatal(err)
				}
				if err := lease.VerifyLogin(t.Context()); err != nil {
					t.Fatalf("submission check rejected valid metadata: %v", err)
				}
				if _, err := os.Stat(trace); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("metadata compatibility check launched a native diagnostic")
				}
			})
		}
	}
}

func TestNativeCredentialFileRetainsOneMiBSizeBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", (1<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeAccountFile(path); !errors.Is(err, status_error.AIAccountCredentialError) {
		t.Fatalf("actual credential reader exceeded its 1 MiB bound: %v", err)
	}
}

func TestExecutionClaudeSystemMetadataRejectsNonRegularFile(t *testing.T) {
	input, _ := executionAccountFixture(t, constants.RuntimeIDClaude)
	if err := os.Mkdir(filepath.Join(input.CWD, constants.ClaudeAccountMetadataFileName), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil).ResolveAccount(t.Context(), input, ""); !errors.Is(err, status_error.AIAccountCredentialError) {
		t.Fatalf("native metadata accepted a directory: %v", err)
	}
}
