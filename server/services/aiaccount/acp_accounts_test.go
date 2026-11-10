package aiaccount

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
)

func TestACPSystemWorkBuddyFencesNativePrincipal(t *testing.T) {
	home := acpAccountTestHome(t)
	configHome := filepath.Join(home, "codebuddy-settings")
	if err := os.Mkdir(configHome, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEBUDDY_CONFIG_DIR", configHome)
	t.Setenv("WORKBUDDY_CONFIG_DIR", filepath.Join(home, "desktop-settings"))
	platformData := filepath.Join(home, ".local", "share")
	switch runtime.GOOS {
	case "darwin":
		platformData = filepath.Join(home, "Library", "Application Support")
	case "windows":
		platformData = filepath.Join(home, "AppData", "Local")
	}
	path := filepath.Join(platformData, "CodeBuddyExtension", "Data", "Public", "auth", "Tencent-Cloud.coding-copilot.info")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	writeLogin := func(uid, org, credential string) {
		t.Helper()
		body := `{"account":{"uid":"` + uid + `","enterpriseId":"` + org + `","type":"personal"},"auth":{"accessToken":"` + credential + `"}}`
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	service := New(nil)
	request := agent.AccountRequest{Provider: constants.RuntimeIDWorkBuddy, AccountID: agent.SystemAccountID, CWD: home}
	if _, err := service.ResolveAccount(t.Context(), request, ""); err != nil {
		t.Fatalf("unobserved native login must reach actual CLI execution: %v", err)
	}

	writeLogin("native-principal", "first-organization", "test-access")
	resolved, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Subject == "" || resolved.AuthGeneration == 0 ||
		strings.Contains(resolved.Subject, "native-principal") || strings.Contains(resolved.Subject, "test-access") {
		t.Fatal("the frozen selection must use an opaque provider identity")
	}
	lease, err := service.AcquireAccount(t.Context(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	if lease.ConfigHome != configHome || lease.Environment["CODEBUDDY_CONFIG_DIR"] != configHome ||
		lease.Environment["WORKBUDDY_CONFIG_DIR"] != "" || lease.VerifyLogin == nil {
		t.Fatal("the WorkBuddy lease must preserve the CLI scope and check identity before submission")
	}
	writeLogin("native-principal", "first-organization", "test-refreshed-access")
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatalf("a refresh under the same native principal changed its identity: %v", err)
	}
	writeLogin("native-principal", "other-organization", "test-other-organization")
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("the submission check accepted another organization: %v", err)
	}
	if _, err := service.AcquireAccount(t.Context(), resolved); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("the acquisition accepted another organization: %v", err)
	}

	if err := os.WriteFile(path+".logged-out", []byte("marker"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("an old lease bypassed the native logout marker: %v", err)
	}
	if _, err := service.ResolveAccount(t.Context(), request, ""); err != nil {
		t.Fatalf("native signed-out selection was blocked before execution: %v", err)
	}
}

func TestACPSystemWorkBuddyExplicitCredentialPrecedence(t *testing.T) {
	home := acpAccountTestHome(t)
	t.Setenv("CODEBUDDY_API_KEY", "fixture-key")
	t.Setenv("CODEBUDDY_AUTH_TOKEN", "fixture-token")
	t.Setenv("CODEBUDDY_INTERNET_ENVIRONMENT", "internal")
	service := New(nil)
	request := agent.AccountRequest{Provider: constants.RuntimeIDWorkBuddy, AccountID: agent.SystemAccountID, CWD: home}
	resolved, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Environment["CODEBUDDY_AUTH_TOKEN"] != "fixture-token" || lease.Environment["CODEBUDDY_API_KEY"] != "" ||
		lease.Environment["CODEBUDDY_INTERNET_ENVIRONMENT"] != "internal" || lease.Environment["CODEBUDDY_CONFIG_DIR"] != "" {
		t.Fatal("the system lease did not project only the higher-priority credential and native defaults")
	}
	t.Setenv("CODEBUDDY_AUTH_TOKEN", "")
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("a switch from token to API key passed the frozen identity check: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".codebuddy"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codebuddy", "settings.local.json"), []byte(`{"apiKeyHelper":"would-override-selected-key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveAccount(t.Context(), request, ""); !errors.Is(err, status_error.RuntimePolicyUnsupported) {
		t.Fatalf("an unbound native settings helper could replace the selected credential: %v", err)
	}
}

func TestACPSystemQoderPATLeaseFencesCredential(t *testing.T) {
	home := acpAccountTestHome(t)
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "synthetic-token")
	service := New(nil)
	selected, err := service.ResolveAccount(t.Context(), agent.AccountRequest{
		Provider: constants.RuntimeIDQoder, AccountID: agent.SystemAccountID, CWD: home,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if selected.ConfigHome != filepath.Join(home, ".qoder") {
		t.Fatalf("unexpected config root: %s", selected.ConfigHome)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Environment["QODER_PERSONAL_ACCESS_TOKEN"] != "synthetic-token" {
		t.Fatal("the lease did not project the selected credential")
	}
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "replacement-synthetic-token")
	if _, err := service.AcquireAccount(t.Context(), selected); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("the stale token could still acquire a lease: %v", err)
	}
}

func TestACPSystemQoderLocalEvidenceFencesChangesWithoutProbe(t *testing.T) {
	request, trace := executionAccountFixture(t, constants.RuntimeIDQoder)
	auth := filepath.Join(request.CWD, ".qoder", ".auth")
	if err := os.MkdirAll(auth, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(auth, "user")
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("opaque-local-account-a")
	service := New(nil)
	selected, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	if lease.QoderLoginVerified || lease.QoderUserID != "" {
		t.Fatal("opaque local evidence was presented as an authenticated native account")
	}
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatal(err)
	}
	write("opaque-local-account-b")
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("local credential change reused the old native binding: %v", err)
	}
	next, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil || next.Subject == selected.Subject {
		t.Fatalf("new local selection was not isolated: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveAccount(t.Context(), request, ""); err != nil {
		t.Fatalf("missing native login should reach real execution: %v", err)
	}
	if _, err := os.Stat(trace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("local selection launched an independent native process")
	}
}

func TestACPSystemAccountsRejectUnboundWorkingScope(t *testing.T) {
	home := acpAccountTestHome(t)
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "fixture-pat")
	t.Setenv("CODEBUDDY_API_KEY", "fixture-key")
	service := New(nil)
	request := agent.AccountRequest{
		Provider: constants.RuntimeIDQoder, AccountID: agent.SystemAccountID,
		CWD: home, PrefixArgs: []string{"--cwd=" + filepath.Join(home, "other")},
	}
	if _, err := service.ResolveAccount(t.Context(), request, ""); !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
		t.Fatalf("a native wrapper can redirect the account's frozen working directory: %v", err)
	}

	request.Provider, request.PrefixArgs = constants.RuntimeIDWorkBuddy, nil
	config := filepath.Join(home, ".codebuddy")
	if err := os.Mkdir(config, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "settings.json"), []byte(`{"env":{"CODEBUDDY_PROJECT_CONFIG_DIR":"/other"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveAccount(t.Context(), request, ""); !errors.Is(err, status_error.RuntimePolicyUnsupported) {
		t.Fatalf("native settings can redirect the credential source: %v", err)
	}
}

func TestACPSystemAccountsRejectManagedSelection(t *testing.T) {
	service := New(nil)
	for _, provider := range []string{constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder} {
		request := agent.AccountRequest{Provider: provider, AccountID: "host-managed"}
		if _, err := service.ResolveAccount(t.Context(), request, ""); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
			t.Fatalf("%s accepted a host-managed selection: %v", provider, err)
		}
	}
}

func acpAccountTestHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	for _, key := range []string{
		"CODEBUDDY_CONFIG_DIR", "CODEBUDDY_API_KEY", "CODEBUDDY_AUTH_TOKEN", "CODEBUDDY_INTERNET_ENVIRONMENT",
		"QODER_CONFIG_DIR", "QODER_PERSONAL_ACCESS_TOKEN",
	} {
		t.Setenv(key, "")
	}
	return home
}

// TestUnifiedQoderCNUsesNativeScope 验证统一条目下 CN 程序的配置与凭据仍使用原生范围。
func TestUnifiedQoderCNUsesNativeScope(t *testing.T) {
	home := acpAccountTestHome(t)
	t.Setenv("QODERCN_PERSONAL_ACCESS_TOKEN", "cn-token")
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "global-token")
	config, err := newACPSystemAccount(agent.AccountRequest{Provider: constants.RuntimeIDQoder, Executable: "/bin/qoderclicn"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if config.configHome != filepath.Join(home, ".qoder-cn") || config.environment["QODERCN_PERSONAL_ACCESS_TOKEN"] != "cn-token" || config.environment["QODER_PERSONAL_ACCESS_TOKEN"] != "" {
		t.Fatal("CN selection reused global account scope")
	}
}
