package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func kimiAuthenticationFixture(t *testing.T, mode string) NativeAuthenticationClient {
	t.Helper()
	request := providerTestRequest(t, "kimi")
	request.Account.AccountID = SystemAccountID
	request.Account.NativeConfigHome = request.Account.CWD
	request.Launch.PrefixArgs = []string{"-test.run=^TestKimiAuthenticationChild$", "--"}
	request.Launch.Environment["KIMI_TEST_MODE"] = mode
	client, err := NewSystemAuthenticationClient(t.Context(), request.Account, request.Launch)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	return client
}

func TestKimiSystemLoginCapabilitiesNeverStartLogin(t *testing.T) {
	client := kimiAuthenticationFixture(t, "success")
	capabilities, err := client.Initialize(t.Context())
	if err != nil || len(capabilities.Methods) != 1 || capabilities.Methods[0].ID != "login" || capabilities.Methods[0].Kind != "agent" {
		t.Fatalf("missing native device login: %+v %v", capabilities, err)
	}
}

func TestKimiSystemLoginReportsNativeDeviceAndVerifies(t *testing.T) {
	client := kimiAuthenticationFixture(t, "success")
	if _, err := client.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	progress, ok := client.(interface{ SetAuthenticationProgressHandler(func(string, string)) })
	if !ok {
		t.Fatal("native login progress is unavailable")
	}
	var address, code string
	progress.SetAuthenticationProgressHandler(func(url, value string) { address, code = url, value })
	result, err := client.Authenticate(t.Context(), "login")
	if err != nil || result != constants.RuntimeAuthenticationResultAuthenticationCompleted {
		t.Fatalf("login: %s %v", result, err)
	}
	if address != "https://www.kimi.com/code/authorize_device?user_code=ABCD-EFGH" || code != "ABCD-EFGH" {
		t.Fatalf("device: %q %q", address, code)
	}
}

func TestKimiSystemLoginFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"failure", "cancel", "verify-failure"} {
		t.Run(mode, func(t *testing.T) {
			client := kimiAuthenticationFixture(t, mode)
			if _, err := client.Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
			defer cancel()
			result, err := client.Authenticate(ctx, "login")
			if err == nil || result != constants.RuntimeAuthenticationResultUnspecified {
				t.Fatalf("%s claimed success: %s %v", mode, result, err)
			}
			if mode != "cancel" && !errors.Is(err, status_error.RuntimeAuthRequired) {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
}

// TestKimiAuthenticationChild supplies a real process and never accesses user credentials.
func TestKimiAuthenticationChild(t *testing.T) {
	mode := os.Getenv("KIMI_TEST_MODE")
	if mode == "" {
		return
	}
	home := os.Getenv("KIMI_CODE_HOME")
	if home == "" {
		os.Exit(3)
	}
	if slices.Contains(os.Args, "login") {
		if slices.Contains(os.Args, "--help") {
			if _, err := os.Stat(filepath.Join(home, "login-started")); err == nil {
				os.Exit(4)
			}
			fmt.Println("Usage: kimi login [options]\nAuthenticate via the device-code flow.")
			os.Exit(0)
		}
		if err := os.WriteFile(filepath.Join(home, "login-started"), nil, 0600); err != nil {
			os.Exit(5)
		}
		fmt.Print("\x1b[32mOpening browser: https://www.kimi.com/code/authorize_device?user_code=AB")
		time.Sleep(10 * time.Millisecond)
		fmt.Println("CD-EFGH\x1b[0m")
		switch mode {
		case "failure":
			os.Exit(1)
		case "cancel":
			time.Sleep(time.Minute)
		}
		os.Exit(0)
	}
	if err := os.Setenv("FIXTURE_PROVIDER", "kimi"); err != nil {
		t.Fatal(err)
	}
	if mode == "verify-failure" {
		_ = os.Setenv("FIXTURE_KIMI_SIGNED_OUT", "true")
	}
	// The management fixture supplies auth-gated discovery without prompts.
	TestProviderRuntimeChild(t)
}

func TestKimiCatalogVerifiesLoginBeforeTrustingModels(t *testing.T) {
	request := providerTestRequest(t, "kimi")
	request.Launch.Environment["FIXTURE_KIMI_SIGNED_OUT"] = "true"
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) { return AccountLease{}, nil }})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	snapshot := NewModelCatalog(runtime).ReadForRuntime(t.Context(), "kimi", request, "", "", true)
	if snapshot.State != CatalogSignedOut {
		t.Fatalf("signed-out native login accepted: %+v", snapshot)
	}
	trace, _ := os.ReadFile(filepath.Join(request.Account.CWD, "trace"))
	if !strings.Contains(string(trace), "session/new") || strings.Contains(string(trace), "session/prompt") {
		t.Fatalf("login verification trace: %s", trace)
	}
}

func TestKimiLoginOutputRejectsUnsafeAndIncompleteAddresses(t *testing.T) {
	calls := 0
	output := &kimiAuthenticationOutput{progress: func(address, code string) { calls++ }}
	for _, line := range []string{
		"https://evil.example/code/authorize_device?user_code=ABCD-EFGH\n",
		"https://www.kimi.com.evil.example/code/authorize_device?user_code=ABCD-EFGH\n",
		"https://user@www.kimi.com/code/authorize_device?user_code=ABCD-EFGH\n",
		"https://www.kimi.com:443/code/authorize_device?user_code=ABCD-EFGH\n",
		"https://www.kimi.com/code/authorize_device?user_code=",
	} {
		_, _ = output.Write([]byte(line))
	}
	if calls != 0 {
		t.Fatal("unsafe or incomplete address escaped")
	}
	_, _ = output.Write([]byte("ABCD-EFGH\n"))
	_, _ = output.Write([]byte("https://www.kimi.ai/code/authorize_device?user_code=SECOND-CODE\n"))
	_, _ = output.Write([]byte("https://www.kimi.ai/code/authorize_device?user_code=SECOND-CODE\n"))
	if calls != 2 {
		t.Fatalf("duplicate device event: %d", calls)
	}
	_, _ = output.Write([]byte("https://auth.kimi.com/verify?user_code=THIRD-CODE\n"))
	if calls != 3 {
		t.Fatalf("device callback count=%d", calls)
	}
}

func TestKimiLoginOutputReportsNativeVerificationURIFallback(t *testing.T) {
	var updates [][2]string
	output := &kimiAuthenticationOutput{progress: func(address, code string) {
		updates = append(updates, [2]string{address, code})
	}}
	for _, code := range []string{"FIRST-CODE", "SECOND-CODE", "SECOND-CODE"} {
		before := len(updates)
		_, _ = output.Write([]byte("Opening browser for Kimi device login: https://www.kimi.com/code/authorize_device\n"))
		if len(updates) != before {
			t.Fatal("address without its code was published")
		}
		_, _ = output.Write([]byte("If the browser did not open, paste the URL above and enter code: " + code[:3]))
		_, _ = output.Write([]byte(code[3:] + "\n"))
	}
	if len(updates) != 2 || updates[0] != [2]string{"https://www.kimi.com/code/authorize_device", "FIRST-CODE"} || updates[1][1] != "SECOND-CODE" {
		t.Fatalf("native verificationUri fallback or renewal lost: %+v", updates)
	}
}

func TestKimiLoginOutputRejectsUnboundVerificationURIFallback(t *testing.T) {
	for _, output := range []string{
		"Opening browser for Kimi device login: https://evil.example/verify\nIf the browser did not open, paste the URL above and enter code: FIRST-CODE\n",
		"https://www.kimi.com/verify\nIf the browser did not open, paste the URL above and enter code: FIRST-CODE\n",
		"Opening browser for Kimi device login: https://www.kimi.com/verify\nother output\nIf the browser did not open, paste the URL above and enter code: FIRST-CODE\n",
		"Opening browser for Kimi device login: https://www.kimi.com/verify\nIf the browser did not open, paste the URL above and enter code: invalid code\n",
	} {
		writer := &kimiAuthenticationOutput{progress: func(address, code string) { t.Fatalf("unbound fallback published: %q %q", address, code) }}
		_, _ = writer.Write([]byte(output))
	}
}

func TestKimiSystemLoginRealCapabilities(t *testing.T) {
	binary := os.Getenv("NEUXBOT_KIMI_AUTH_SMOKE_BINARY")
	if binary == "" {
		t.Skip("explicit installed CLI required")
	}
	root := t.TempDir()
	client, err := NewSystemAuthenticationClient(t.Context(), AccountRequest{Provider: "kimi", AccountID: SystemAccountID, NativeConfigHome: root, CWD: root}, ProviderLaunch{Executable: binary})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	capabilities, err := client.Initialize(t.Context())
	if err != nil || len(capabilities.Methods) != 1 {
		t.Fatalf("real CLI capabilities=%+v %v", capabilities, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("capability check modified account root: entries=%d error=%v", len(entries), err)
	}

	account := AccountRequest{Provider: "kimi", AccountID: SystemAccountID, CWD: root}
	verifier, err := newDirectAgentClient(t.Context(), account, ProviderLaunch{Executable: binary}, AccountLease{Environment: map[string]string{"KIMI_CODE_HOME": root, "KIMI_SHARE_DIR": root}}, LaunchAuthLease{}, ResourceLease{}, AgentClientCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	auth := &nativeAuthenticationClient{provider: "kimi", client: verifier}
	defer func() {
		if err := auth.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	if caps, err := auth.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	} else {
		t.Logf("native auth capabilities: %d advertised interactive methods", len(caps.Methods))
	}
	if err := verifier.verifyKimiLogin(t.Context()); !errors.Is(err, status_error.RuntimeAuthRequired) {
		t.Fatalf("empty real native home was not signed out: %v", err)
	}
}

func TestKimiCatalogReportsPositiveNativeAuthGateReceipt(t *testing.T) {
	request := providerTestRequest(t, "kimi")
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) { return AccountLease{}, nil }})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	snapshot := NewModelCatalog(runtime).ReadForRuntime(t.Context(), "kimi", request, "", "", true)
	if snapshot.State != CatalogFresh || !snapshot.KimiLoginVerified || len(snapshot.Models) != 1 {
		t.Fatalf("native auth gate receipt lost: %+v", snapshot)
	}
	trace, _ := os.ReadFile(filepath.Join(request.Account.CWD, "trace"))
	if !strings.Contains(string(trace), "session/new") || strings.Contains(string(trace), "session/prompt") || strings.Contains(string(trace), "authenticate") {
		t.Fatalf("unexpected discovery commands: %s", trace)
	}
}
