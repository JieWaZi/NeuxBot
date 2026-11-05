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
	"time"

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

func TestACPSystemQoderReusesOnlyVerifiedUnchangedLogin(t *testing.T) {
	home := acpAccountTestHome(t)
	auth := filepath.Join(home, ".qoder", ".auth")
	if err := os.MkdirAll(auth, 0o700); err != nil {
		t.Fatal(err)
	}
	credential := filepath.Join(auth, "user")
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(credential, "opaque-credential-a")
	write(filepath.Join(auth, "machine_id"), "machine-key")
	trace := filepath.Join(home, "qoder-account-trace")
	t.Setenv("NEUX_QODER_ACCOUNT_FIXTURE", "1")
	t.Setenv("NEUX_QODER_ACCOUNT_TRACE", trace)
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
	s := New(nil)
	checkStarts := func(want int) {
		t.Helper()
		raw, err := os.ReadFile(trace)
		if err != nil || strings.Count(string(raw), "sdk\n") != want {
			t.Fatalf("expected %d native SDK starts, got %q (%v)", want, raw, err)
		}
	}
	selected, err := s.ResolveAccount(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.AcquireAccount(t.Context(), selected)
	if err != nil || lease.QoderUserID != "native-user-first" {
		t.Fatalf("cannot acquire verified login: %v", err)
	}
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatal(err)
	}
	if current, err := s.ResolveAccount(t.Context(), request, ""); err != nil || !current.Equal(selected) {
		t.Fatalf("unchanged login did not retain the subject: %v", err)
	}
	checkStarts(1)

	cnAuth := filepath.Join(home, ".qoder-cn", ".auth")
	if err := os.MkdirAll(cnAuth, 0o700); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(cnAuth, "user"), "opaque-credential-a")
	write(filepath.Join(cnAuth, "machine_id"), "machine-key")
	cnRequest := request
	cnRequest.Provider = constants.RuntimeIDQoderCN
	t.Setenv("NEUX_QODER_ACCOUNT_UID", "native-user-cn")
	cn, err := s.ResolveAccount(t.Context(), cnRequest, "")
	if err != nil || cn.Subject == selected.Subject {
		t.Fatalf("regional account did not receive independent verification: %v", err)
	}
	checkStarts(2)
	t.Setenv("NEUX_QODER_ACCOUNT_UID", "native-user-first")
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatal(err)
	}
	checkStarts(2)

	previous := s.qoderLogins[constants.RuntimeIDQoder]
	previous.observedAt = time.Now().Add(-time.Minute)
	s.qoderLogins[constants.RuntimeIDQoder] = previous
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatalf("expired evidence could not be refreshed: %v", err)
	}
	checkStarts(3)

	write(credential, "opaque-credential-refreshed")
	if err := lease.VerifyLogin(t.Context()); err != nil {
		t.Fatalf("same subject after credential refresh: %v", err)
	}
	checkStarts(4)
	t.Setenv("NEUX_QODER_ACCOUNT_UID", "native-user-second")
	write(credential, "opaque-credential-b")
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("credential changed to a different subject: %v", err)
	}
	checkStarts(5)
	t.Setenv("NEUX_QODER_ACCOUNT_UID", "")
	t.Setenv("NEUX_QODER_ACCOUNT_STATUS", "false")
	if err := os.Remove(credential); err != nil {
		t.Fatal(err)
	}
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("deleted credentials did not fence the verified subject: %v", err)
	}
	checkStarts(6)
	write(credential, "opaque-credential-b")
	if _, err := s.ResolveAccount(t.Context(), request, ""); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("signed-out observation left an older positive cache entry: %v", err)
	}
	checkStarts(7)
}

func TestACPSystemQoderRejectsSubjectSwitchedDuringNativeRead(t *testing.T) {
	home := acpAccountTestHome(t)
	auth := filepath.Join(home, ".qoder", ".auth")
	if err := os.MkdirAll(auth, 0o700); err != nil {
		t.Fatal(err)
	}
	credential := filepath.Join(auth, "user")
	for path, content := range map[string]string{credential: "opaque-original", filepath.Join(auth, "machine_id"): "machine-key"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("NEUX_QODER_ACCOUNT_FIXTURE", "1")
	t.Setenv("NEUX_QODER_ACCOUNT_UID", "native-user-first")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	request := agent.AccountRequest{
		Provider: constants.RuntimeIDQoder, AccountID: agent.SystemAccountID,
		CWD: home, Executable: executable, PrefixArgs: []string{"-test.run=^TestACPSystemQoderAccountProcess$", "--"},
	}
	service := New(nil)
	selected, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	// A refresh causes a real verification read. While that read still
	// reports the first user, the fixture swaps the on-disk credential.
	if err := os.WriteFile(credential, []byte("opaque-before-switch"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEUX_QODER_ACCOUNT_ROTATE_ON_PROBE", credential)
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("pre-submit verification accepted the old subject after a concurrent switch: %v", err)
	}
}

func TestACPSystemQoderServiceAccountFencesPrincipalAndOrganization(t *testing.T) {
	home := acpAccountTestHome(t)
	t.Setenv("NEUX_QODER_ACCOUNT_FIXTURE", "1")
	t.Setenv("NEUX_QODER_ACCOUNT_STATUS_JSON", `{"logged_in":true,"account_type":"service_account","principal_id":"principal-a","org_id":"organization-a"}`)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	request := agent.AccountRequest{
		Provider: constants.RuntimeIDQoder, AccountID: agent.SystemAccountID,
		CWD: home, Executable: executable, PrefixArgs: []string{"-test.run=^TestACPSystemQoderAccountProcess$", "--"},
	}
	service := New(nil)
	selected, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), selected)
	if err != nil || !lease.QoderLoginVerified || lease.QoderUserID != "" {
		t.Fatalf("service account did not carry native sign-in evidence: %v", err)
	}
	t.Setenv("NEUX_QODER_ACCOUNT_STATUS_JSON", `{"logged_in":true,"account_type":"service_account","principal_id":"principal-a","org_id":"organization-b"}`)
	if err := lease.VerifyLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("the service account organization switch escaped the fence: %v", err)
	}
	t.Setenv("NEUX_QODER_ACCOUNT_STATUS_JSON", `{"logged_in":true,"account_type":"service_account","principal_id":"principal-a"}`)
	unscoped, err := service.ResolveAccount(t.Context(), request, "")
	if err != nil || unscoped.Subject == selected.Subject {
		t.Fatalf("a native principal without an organization was rejected or conflated: %v", err)
	}

	auth := filepath.Join(home, ".qoder", ".auth")
	if err := os.MkdirAll(auth, 0o700); err != nil {
		t.Fatal(err)
	}
	credential := filepath.Join(auth, "user")
	for path, content := range map[string]string{credential: "opaque-original", filepath.Join(auth, "machine_id"): "machine-key"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("NEUX_QODER_ACCOUNT_ROTATE_ON_STATUS", credential)
	if _, err := service.ResolveAccount(t.Context(), request, ""); !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("status accepted a principal after a detected credential change: %v", err)
	}
}

func TestACPSystemQoderAccountProcess(t *testing.T) {
	if os.Getenv("NEUX_QODER_ACCOUNT_FIXTURE") == "" {
		return
	}
	if strings.HasSuffix(strings.Join(os.Args, " "), "status -o json") {
		if status := os.Getenv("NEUX_QODER_ACCOUNT_STATUS_JSON"); status != "" {
			if credential := os.Getenv("NEUX_QODER_ACCOUNT_ROTATE_ON_STATUS"); credential != "" {
				if err := os.WriteFile(credential, []byte("opaque-rotated-by-status"), 0o600); err != nil {
					os.Exit(4)
				}
			}
			fmt.Println(status)
			os.Exit(0)
		}
		fmt.Printf("{\"logged_in\":%s}\n", os.Getenv("NEUX_QODER_ACCOUNT_STATUS"))
		os.Exit(0)
	}
	if !strings.Contains(strings.Join(os.Args, " "), "--no-session-persistence") || os.Getenv("QODERCN_PERSONAL_ACCESS_TOKEN") != "" {
		os.Exit(2)
	}
	if trace := os.Getenv("NEUX_QODER_ACCOUNT_TRACE"); trace != "" {
		file, err := os.OpenFile(trace, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(4)
		}
		if _, err := file.WriteString("sdk\n"); err != nil || file.Close() != nil {
			os.Exit(4)
		}
	}
	userID := os.Getenv("NEUX_QODER_ACCOUNT_UID")
	if credential := os.Getenv("NEUX_QODER_ACCOUNT_ROTATE_ON_PROBE"); credential != "" {
		raw, err := os.ReadFile(credential)
		if err != nil {
			os.Exit(4)
		}
		if string(raw) == "opaque-before-switch" {
			if err := os.WriteFile(credential, []byte("opaque-after-switch"), 0o600); err != nil {
				os.Exit(4)
			}
		} else {
			userID = "native-user-after-switch"
		}
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
					"userId": userID, "organization": os.Getenv("NEUX_QODER_ACCOUNT_ORG"),
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
