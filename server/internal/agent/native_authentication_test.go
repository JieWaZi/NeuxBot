package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go-sdk"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	native "github.com/JieWaZi/acp-go/pkg/agentclient"
)

// authenticationNativeBoundary panics on any unintended execution or session operation.
type authenticationNativeBoundary struct {
	// Client supplies unsupported methods so a session call fails the test immediately.
	native.Client
	// accounts supplies native account observations before and after authentication.
	accounts []acpmeta.AccountInfo
	// accountErrors supplies bounded failures for the corresponding account read.
	accountErrors []error
	// accountReads counts native reads, never credential-file inspection.
	accountReads int
	// authenticateError models a native refresh/authentication failure.
	authenticateError error
	// request records the protocol handshake.
	request acp.InitializeRequest
	// method records the explicit native authentication identity.
	method string
	// closes records exact-owner cleanup.
	closes int
}

func (f *authenticationNativeBoundary) Initialize(_ context.Context, request acp.InitializeRequest) (acp.InitializeResponse, error) {
	f.request = request
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersionNumber, Meta: map[string]any{"defaultAuthMethodId": "xai.api_key"}, AuthMethods: []acp.AuthMethod{{Agent: &acp.AuthMethodAgent{Id: "grok.com/oidc", Name: "Grok"}}, {Terminal: &acp.AuthMethodTerminalInline{Id: "terminal", Name: "Terminal", Type: "terminal"}}}}, nil
}
func (f *authenticationNativeBoundary) Authenticate(_ context.Context, request acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	f.method = request.MethodId
	return acp.AuthenticateResponse{}, f.authenticateError
}
func (f *authenticationNativeBoundary) ReadAccount(context.Context) (acpmeta.AccountInfo, error) {
	index := f.accountReads
	f.accountReads++
	var view acpmeta.AccountInfo
	var err error
	if index < len(f.accounts) {
		view = f.accounts[index]
	}
	if index < len(f.accountErrors) {
		err = f.accountErrors[index]
	}
	return view, err
}
func (f *authenticationNativeBoundary) Close(context.Context) error { f.closes++; return nil }
func TestNativeAuthenticationUsesManagementHandshakeWithoutSession(t *testing.T) {
	fake := &authenticationNativeBoundary{}
	client := &nativeAuthenticationClient{client: &directAgentClient{native: fake}}
	capabilities, err := client.Initialize(t.Context())
	if err != nil || !capabilities.APIKeyMode || len(capabilities.Methods) != 2 || fake.request.ProtocolVersion != acp.ProtocolVersionNumber {
		t.Fatalf("initialize=%+v %v", capabilities, err)
	}
	if result, err := client.Authenticate(t.Context(), "grok.com/oidc"); err != nil || fake.method != "grok.com/oidc" || result != constants.RuntimeAuthenticationResultAuthenticationCompleted {
		t.Fatalf("authenticate=%s result=%s %v", fake.method, result, err)
	}
	if err := client.Close(t.Context()); err != nil || fake.closes != 1 {
		t.Fatalf("close=%d %v", fake.closes, err)
	}
}
func TestNativeAuthenticationAdmissionFencesOnlySelectedSystemProvider(t *testing.T) {
	runtime := NewProviderRuntime(ProviderRuntimeOptions{})
	release, err := runtime.BeginSystemAuthentication(t.Context(), "grok")
	if err != nil {
		t.Fatal(err)
	}
	request := ProviderConnectionRequest{Account: AccountRequest{Provider: "grok", AccountID: SystemAccountID, CWD: t.TempDir()}}
	if owner, err := runtime.Open(t.Context(), request, ProviderConnectionPorts{}); owner != nil || !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("admitted system consumer: %v %v", owner, err)
	}
	if _, err := runtime.BeginSystemAuthentication(t.Context(), "grok"); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("duplicate mutation=%v", err)
	}
	if err := runtime.CheckSystemAuthenticationIdle("grok"); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	runtime.connections[1] = &providerConnection{account: providerAccount{provider: "grok", id: "managed"}}
	runtime.mu.Unlock()
	if err := runtime.CheckSystemAuthenticationIdle("grok"); err != nil {
		t.Fatalf("managed consumer blocked system scope: %v", err)
	}
	runtime.mu.Lock()
	runtime.connections[2] = &providerConnection{account: providerAccount{provider: "grok", id: SystemAccountID}}
	runtime.mu.Unlock()
	if err := runtime.CheckSystemAuthenticationIdle("grok"); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("live system owner bypassed fence: %v", err)
	}
	release()
	release()
	if err := runtime.CheckSystemAuthenticationIdle("grok"); !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
		t.Fatalf("release did not remove fence: %v", err)
	}
}

func TestNativeAuthenticationEnvironmentKeepsOnlySelectedCredentialScope(t *testing.T) {
	launch := ProviderLaunch{BaseEnvironment: map[string]string{"GROK_HOME": "/native/grok", "XAI_API_KEY": "fixture-xai", "OPENAI_API_KEY": "unrelated", "ANTHROPIC_API_KEY": "unrelated", "ALLYCLI_AUTH": "private", "PATH": "/bin"}}
	selected := systemAuthenticationEnvironment("grok", launch)
	if selected["XAI_API_KEY"] != "fixture-xai" || selected["GROK_HOME"] != "/native/grok" || selected["OPENAI_API_KEY"] != "" || selected["ANTHROPIC_API_KEY"] != "" || selected["ALLYCLI_AUTH"] != "" {
		t.Fatal("management lease crossed native credential scopes")
	}
	if !systemAuthenticationHasCredential("grok", selected) || systemAuthenticationHasCredential("qoder", selected) {
		t.Fatal("explicit environment capability used the wrong provider")
	}
	if _, err := NewSystemAuthenticationClient(t.Context(), AccountRequest{AccountID: "managed"}, ProviderLaunch{}); !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
		t.Fatalf("managed scope accepted: %v", err)
	}
}

// TestCodexAuthenticationConfirmsNativeAccountWithoutClaimingNewAuthorization
// reproduces the adapter's existing-login no-op without launching any browser or CLI.
func TestCodexAuthenticationConfirmsNativeAccountWithoutClaimingNewAuthorization(t *testing.T) {
	chatGPT := acpmeta.AccountInfo{Account: &acpmeta.AccountIdentity{Kind: constants.NativeAccountKindAccount}}
	apiKey := acpmeta.AccountInfo{Account: &acpmeta.AccountIdentity{Kind: "api_key"}}
	for _, fixture := range []struct {
		// name identifies the native observation sequence.
		name string
		// before is the initial native account context.
		before acpmeta.AccountInfo
		// after is the native account observed after the authenticate acknowledgement.
		after acpmeta.AccountInfo
		// readErrors injects transport failures without touching credentials.
		readErrors []error
		// authError injects a native refresh/authentication failure.
		authError error
		// want is the evidence-backed public result.
		want constants.RuntimeAuthenticationResult
		// failed requires the operation to reject a misleading empty success.
		failed bool
	}{
		{name: "existing login is refreshed and verified", before: chatGPT, after: chatGPT, want: constants.RuntimeAuthenticationResultExistingLoginVerified},
		{name: "signed out connection is verified conservatively", after: chatGPT, want: constants.RuntimeAuthenticationResultConnectionVerified},
		{name: "empty acknowledgement has no authenticated account", failed: true},
		{name: "API key cannot confirm requested ChatGPT login", after: apiKey, failed: true},
		{name: "failed post-read cannot confirm login", before: chatGPT, after: chatGPT, readErrors: []error{nil, status_error.RuntimeUnavailable}, failed: true},
		{name: "existing identity cannot bypass native refresh failure", before: chatGPT, authError: status_error.RuntimeAuthRequired, failed: true},
		{name: "unreadable old state still permits native refresh", after: chatGPT, readErrors: []error{status_error.RuntimeAuthRequired}, want: constants.RuntimeAuthenticationResultConnectionVerified},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			fake := &authenticationNativeBoundary{accounts: []acpmeta.AccountInfo{fixture.before, fixture.after}, accountErrors: fixture.readErrors, authenticateError: fixture.authError}
			client := &nativeAuthenticationClient{provider: constants.RuntimeIDCodex, client: &directAgentClient{native: fake}}
			result, err := client.Authenticate(t.Context(), constants.CodexSubscriptionAuthMethod)
			if (err != nil) != fixture.failed || result != fixture.want || fake.method != constants.CodexSubscriptionAuthMethod {
				t.Fatalf("result=%s err=%v method=%s", result, err, fake.method)
			}
			wantReads := 2
			if fixture.authError != nil {
				wantReads = 1
			}
			if fake.accountReads != wantReads {
				t.Fatalf("native account confirmation reads=%d want=%d", fake.accountReads, wantReads)
			}
		})
	}
}
