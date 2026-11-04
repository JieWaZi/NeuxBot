package aiaccount

import (
	"bufio"
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
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
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
	if _, err := service.ResolveAccount(t.Context(), request, ""); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("the presence of settings does not establish a native account: %v", err)
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
	if _, err := service.ResolveAccount(t.Context(), request, ""); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("an old auth file bypassed the native logout marker: %v", err)
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

func TestACPSystemQoderRegionalPATIsolation(t *testing.T) {
	home := acpAccountTestHome(t)
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "same-synthetic-token")
	t.Setenv("QODERCN_PERSONAL_ACCESS_TOKEN", "same-synthetic-token")
	service := New(nil)
	resolve := func(provider string) agent.AccountRequest {
		t.Helper()
		request, err := service.ResolveAccount(t.Context(), agent.AccountRequest{
			Provider: provider, AccountID: agent.SystemAccountID, CWD: home,
		}, "")
		if err != nil {
			t.Fatal(err)
		}
		return request
	}
	global := resolve(constants.RuntimeIDQoder)
	cn := resolve(constants.RuntimeIDQoderCN)
	if global.Subject == cn.Subject || global.ConfigHome == cn.ConfigHome {
		t.Fatal("the two editions shared native account or configuration identity")
	}
	for _, tc := range []struct {
		// request is the admitted regional account.
		request agent.AccountRequest
		// token is the region's own credential environment variable.
		token string
		// other is the other region's credential environment variable.
		other string
	}{
		{global, "QODER_PERSONAL_ACCESS_TOKEN", "QODERCN_PERSONAL_ACCESS_TOKEN"},
		{cn, "QODERCN_PERSONAL_ACCESS_TOKEN", "QODER_PERSONAL_ACCESS_TOKEN"},
	} {
		lease, err := service.AcquireAccount(t.Context(), tc.request)
		if err != nil {
			t.Fatal(err)
		}
		if lease.Environment[tc.token] != "same-synthetic-token" || lease.Environment[tc.other] != "" {
			t.Fatal("a regional account lease projected the other edition's credential")
		}
	}
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "replacement-synthetic-token")
	if _, err := service.AcquireAccount(t.Context(), global); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("the stale global token could still acquire a lease: %v", err)
	}
	if _, err := service.AcquireAccount(t.Context(), cn); err != nil {
		t.Fatalf("the global token change fenced the CN selection: %v", err)
	}
	if _, err := service.ResolveAccount(t.Context(), global, cn.ConfigHome); !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
		t.Fatalf("global Qoder reused the CN account root: %v", err)
	}
}

func TestACPSystemQoderSavedLoginFencesNativeSubject(t *testing.T) {
	home := acpAccountTestHome(t)
	t.Setenv("NEUX_QODER_ACCOUNT_FIXTURE", "1")
	t.Setenv("NEUX_QODER_ACCOUNT_UID", "native-user-first")
	t.Setenv("NEUX_QODER_ACCOUNT_ORG", "native-organization-first")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	request := agent.AccountRequest{
		Provider: constants.RuntimeIDQoder, AccountID: agent.SystemAccountID,
		CWD: home, Executable: executable, PrefixArgs: []string{"-test.run=^TestACPSystemQoderAccountProcess$", "--"},
	}
	service := New(nil)
	resolved, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resolved.Subject, "native-user-first") || strings.Contains(resolved.Subject, "same-display@example.invalid") {
		t.Fatal("the host subject exposed native account data")
	}
	lease, err := service.AcquireAccount(t.Context(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	if lease.QoderUserID != "native-user-first" {
		t.Fatal("the management lease omitted the verified native identity")
	}
	t.Setenv("NEUX_QODER_ACCOUNT_UID", "native-user-second")
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("the same display email masked a native account switch: %v", err)
	}
	t.Setenv("NEUX_QODER_ACCOUNT_UID", "native-user-first")
	t.Setenv("NEUX_QODER_ACCOUNT_ORG", "native-organization-second")
	if _, err := service.AcquireAccount(t.Context(), resolved); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("the same native user ID masked an organization switch: %v", err)
	}

	t.Setenv("NEUX_QODER_ACCOUNT_UID", "")
	t.Setenv("NEUX_QODER_ACCOUNT_STATUS", "true")
	if _, err := service.ResolveAccount(t.Context(), request, ""); !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("status without an immutable identity was sufficient: %v", err)
	}
	t.Setenv("NEUX_QODER_ACCOUNT_STATUS", "false")
	if _, err := service.ResolveAccount(t.Context(), request, ""); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("the native signed-out receipt was lost: %v", err)
	}
}

func TestACPSystemQoderAccountProcess(t *testing.T) {
	if os.Getenv("NEUX_QODER_ACCOUNT_FIXTURE") == "" {
		return
	}
	if strings.HasSuffix(strings.Join(os.Args, " "), "status -o json") {
		fmt.Printf("{\"logged_in\":%s}\n", os.Getenv("NEUX_QODER_ACCOUNT_STATUS"))
		os.Exit(0)
	}
	if !strings.Contains(strings.Join(os.Args, " "), "--no-session-persistence") || os.Getenv("QODERCN_PERSONAL_ACCESS_TOKEN") != "" {
		os.Exit(2)
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var frame struct {
			// Type distinguishes control requests from user input.
			Type string `json:"type"`
			// ID correlates the native control reply.
			ID string `json:"request_id"`
			// Request is the exact operation sent by the adapter.
			Request map[string]string `json:"request"`
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil || frame.Type != "control_request" ||
			(frame.Request["type"] != "initialize" && frame.Request["type"] != "account_info") {
			os.Exit(3)
		}
		_ = encoder.Encode(map[string]any{"type": "control_response", "response": map[string]any{
			"request_id": frame.ID, "subtype": "success", "response": map[string]any{
				"account": map[string]string{
					"userId": os.Getenv("NEUX_QODER_ACCOUNT_UID"), "organization": os.Getenv("NEUX_QODER_ACCOUNT_ORG"),
					"email": "same-display@example.invalid",
				},
			},
		}})
	}
	os.Exit(0)
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
	for _, provider := range []string{constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder, constants.RuntimeIDQoderCN} {
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
		"QODER_CONFIG_DIR", "QODER_PERSONAL_ACCESS_TOKEN", "QODERCN_CONFIG_DIR", "QODERCN_PERSONAL_ACCESS_TOKEN",
	} {
		t.Setenv(key, "")
	}
	return home
}
