package agent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go-sdk"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	native "github.com/JieWaZi/acp-go/pkg/agentclient"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func TestNativeProviderFailureKindsPreserveOpenCodeQuota(t *testing.T) {
	err := &acp.RequestError{Code: -32603, Message: "ACP agent request failed", Data: map[string]any{"classification": "quota_exhausted"}}
	if got := safeAgentError(err, status_error.RuntimeExecutionFailed); !errors.Is(got, status_error.RuntimeQuotaExhausted) {
		t.Fatalf("unassociated native refusal lost its cause: %v", got)
	}
	for _, outcome := range []native.NativeOutcome{{NativeFailure: err}, {OperationError: err}} {
		if got := agentOutcome(outcome); got.NativeErrorKind != "quota_exhausted" {
			t.Fatalf("quota failure lost its classification: %+v", got)
		}
	}
}

func TestKnownNativeErrorMessagePassesThroughProductBoundary(t *testing.T) {
	for _, code := range []int{-32603, -32602, -32000, 500} {
		err := &acp.RequestError{Code: code, Message: "native provider reason: select an available model"}
		got := safeAgentError(err, status_error.RuntimeExecutionFailed)
		var public statuserror.CommonError
		if !errors.As(got, &public) || public.Value() != err.Message {
			t.Fatalf("known native reason replaced: code=%d err=%v", code, got)
		}
		if code == -32000 && !errors.Is(got, status_error.RuntimeAuthRequired) {
			t.Fatal("native detail broke authentication classification")
		}
		if again := safeAgentError(got, status_error.RuntimeConfigurationInvalid); again != got {
			t.Fatal("a later adapter replaced the public native error")
		}
	}
}

func TestCursorLaunchUsesSelectedConfigHome(t *testing.T) {
	home := t.TempDir()
	env, err := nativeLaunchEnvironment("cursor", ProviderLaunch{
		ConfigHome:      home,
		BaseEnvironment: map[string]string{"CURSOR_CONFIG_DIR": "/unselected"},
	}, AccountLease{ConfigHome: home}, LaunchAuthLease{}, ResourceLease{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(env, "\n"), "CURSOR_CONFIG_DIR="+home) {
		t.Fatalf("selected configuration root not delivered: %v", env)
	}
}

func TestRemainingNativeProvidersKeepLeaseAuthority(t *testing.T) {
	for _, provider := range []string{"kimi", "gemini", "grok", "opencode", "pi"} {
		t.Run(provider, func(t *testing.T) {
			if !SupportsProvider(provider) {
				t.Fatal("provider has no candidate runtime owner")
			}
			env, err := nativeLaunchEnvironment(provider, ProviderLaunch{
				BaseEnvironment: map[string]string{"PATH": "/bin", "PI_MCP_MODULE_PATH": "/bridge.mjs"},
				Environment:     map[string]string{"GEMINI_API_KEY": "wrong-launch", "XDG_DATA_HOME": "/wrong"},
			}, AccountLease{ConfigHome: t.TempDir(), Environment: map[string]string{
				"GEMINI_API_KEY": "selected", "XDG_DATA_HOME": "/selected", "HOME": "/selected-home",
			}}, LaunchAuthLease{}, ResourceLease{Environment: map[string]string{"OPENAI_API_KEY": "wrong-resource"}})
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(env, "\n")
			if strings.Contains(text, "wrong") || !strings.Contains(text, "GEMINI_API_KEY=selected") ||
				!strings.Contains(text, "PI_MCP_MODULE_PATH=/bridge.mjs") || !strings.Contains(text, "HOME=/selected-home") {
				t.Fatalf("lease lost launch authority: %v", env)
			}
		})
	}
}

func TestNativeFamilyModelChangesPreserveBindingButPolicyChangesReplaceIt(t *testing.T) {
	for _, provider := range []string{"kimi", "gemini", "grok", "opencode", "pi"} {
		t.Run(provider, func(t *testing.T) {
			request := ThreadRequest{Connection: ProviderConnectionRequest{
				Account: AccountRequest{Provider: provider, CWD: t.TempDir(), Model: "first"},
				Launch:  ProviderLaunch{ProcessPermissionMode: "default"},
			}, Selection: &TurnSelection{}}
			initial, err := freezeThreadSettings(&request)
			if err != nil {
				t.Fatal(err)
			}
			request.Connection.Account.Model = "second"
			changed, err := freezeThreadSettings(&request)
			if err != nil || string(initial) != string(changed) {
				t.Fatalf("live model selection replaces native history: %s %s %v", initial, changed, err)
			}
			request.Connection.Launch.ProcessPermissionMode = "full-access"
			changed, err = freezeThreadSettings(&request)
			if err != nil || string(initial) == string(changed) {
				t.Fatalf("process permission change reused a frozen launch: %s %v", changed, err)
			}
		})
	}
}

func TestStandardACPProvidersUseTheSharedProcessOwner(t *testing.T) {
	for _, provider := range []string{"workbuddy", "qoder"} {
		t.Run(provider, func(t *testing.T) {
			request := providerTestRequest(t, provider)
			runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: providerTestAccount})
			defer runtime.Close(context.Background())
			connection, err := runtime.Open(t.Context(), request, ProviderConnectionPorts{})
			if err != nil {
				t.Fatal(err)
			}
			client, err := connection.Client()
			if err != nil {
				t.Fatal(err)
			}
			session, err := client.NewSession(t.Context(), AgentSessionOptions{})
			if err != nil || session.ID != "fixture-native" {
				t.Fatalf("session = %#v, %v", session, err)
			}
		})
	}
}

func TestProviderClientKeepsStructuredInputAndAdmission(t *testing.T) {
	req := providerTestRequest(t, "cursor")
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: providerTestAccount})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	observed := make(chan AgentEvent, 4)
	conn, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{Callbacks: AgentClientCallbacks{
		Observe: func(_ context.Context, scope AgentScope, event AgentEvent) error {
			if scope.SessionID != "fixture-native" || scope.HostInvocationID != "host-input" {
				return status_error.RuntimeProtocolIncompatible
			}
			observed <- event
			return nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := conn.Client()
	if err != nil {
		t.Fatal(err)
	}
	capabilities := client.Capabilities()
	if !capabilities.ImageInput || capabilities.Steer || capabilities.Compact {
		t.Fatalf("negotiated capabilities = %+v", capabilities)
	}
	session, err := client.NewSession(t.Context(), AgentSessionOptions{})
	if err != nil || session.ID != "fixture-native" {
		t.Fatalf("new session = %+v, %v", session, err)
	}
	input := AgentPromptRequest{
		SessionID: session.ID, HostInvocationID: "host-input", Content: []AgentContent{
			{Kind: "text", Text: "look"},
			{Kind: "image", Data: "AAE=", MIMEType: "image/png"},
			{Kind: "resource_link", URI: "file:///workspace/receipt.pdf", Name: "receipt.pdf"},
		},
	}
	input.BeforeSubmit = func(_ context.Context, submitted AgentPromptSubmission) error {
		if submitted.HostInvocationID != "host-input" || submitted.InputID == "" {
			t.Errorf("frozen input = %+v", submitted)
		}
		return status_error.AgentOperationForbidden
	}
	denied, err := client.ReservePrompt(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result := denied(); result.Submission != "not_sent" || !errors.Is(result.OperationError, status_error.AgentOperationForbidden) {
		t.Fatalf("rejected result = %+v", result)
	}
	trace := filepath.Join(req.Account.CWD, "trace")
	body, err := os.ReadFile(trace)
	if err != nil || strings.Contains(string(body), "session/prompt") {
		t.Fatalf("rejected input went to native: %q, %v", body, err)
	}
	input.BeforeSubmit = nil
	run, err := client.ReservePrompt(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	result := run()
	if result.Admission != "confirmed" || result.Outcome == nil || result.Outcome.Turn.Status != acpmeta.NativeTurnCompleted {
		t.Fatalf("native outcome = %+v", result)
	}
	body, err = os.ReadFile(trace)
	if err != nil || !strings.Contains(string(body), `"mimeType":"image/png"`) || !strings.Contains(string(body), "file:///workspace/receipt.pdf") {
		t.Fatalf("structured input was lost: %q, %v", body, err)
	}
	select {
	case event := <-observed:
		if event.Kind != "text" || event.Text != "native answer" {
			t.Fatalf("observation = %+v", event)
		}
	default:
		t.Fatal("native text was not observed")
	}
	if _, err := client.Steer(t.Context(), AgentSteerRequest{SessionID: session.ID, Text: "another"}); err != status_error.RuntimePolicyUnsupported {
		t.Fatalf("unsupported steer = %v", err)
	}
}

func TestProviderClientReadsNativeModelSettingsAndReconcilesFork(t *testing.T) {
	req := providerTestRequest(t, "codex")
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: providerTestAccount})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	conn, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := conn.Client()
	if err != nil {
		t.Fatal(err)
	}
	models, err := client.ReadModels(t.Context())
	if err != nil || models.DefaultID != "fixture-model" || len(models.Models) != 1 || models.Models[0].Config == nil {
		t.Fatalf("model catalog = %+v, %v", models, err)
	}
	config := models.Models[0].Config
	if len(config.Options) == 0 || len(config.Options[0].Choices) != 2 || config.Options[0].Choices[1].ID != "high" {
		t.Fatalf("native model configuration was lost: %+v", config)
	}
	request := AgentForkRequest{
		SourceSessionID: "fixture-source", SourceCWD: req.Account.CWD,
		Position: "exact-turn", ReceiptDirectory: filepath.Join(req.Account.CWD, "fork-receipt"),
	}
	created, err := client.ForkSession(t.Context(), request)
	if err != nil || created.ID != "fixture-child" {
		t.Fatalf("fork = %+v, %v", created, err)
	}
	request.ReconcileOnly = true
	recovered, err := client.ForkSession(t.Context(), request)
	if err != nil || recovered.ID != created.ID {
		t.Fatalf("reconcile = %+v, %v", recovered, err)
	}
	request.Position = "wrong-turn"
	if _, err := client.ForkSession(t.Context(), request); err != status_error.ChatForkIndeterminate {
		t.Fatalf("mismatched receipt = %v", err)
	}
	trace, err := os.ReadFile(filepath.Join(req.Account.CWD, "trace"))
	if err != nil || strings.Count(string(trace), `"method":"thread/fork"`) != 1 || !strings.Contains(string(trace), `"lastTurnId":"exact-turn"`) {
		t.Fatalf("wrong native fork writes = %q, %v", trace, err)
	}
}

func TestProviderClientStrictTextUsesNativeNoToolLaunch(t *testing.T) {
	req := providerTestRequest(t, "claude")
	req.Launch.RequireNoTools = true
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: providerTestAccount})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	conn, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := conn.Client()
	if err != nil || !client.Capabilities().ToolsDisabled {
		t.Fatalf("strict launch capability = %v, %v", client != nil, err)
	}
	instructions := "Only return the answer."
	session, err := client.NewSession(t.Context(), AgentSessionOptions{DeveloperInstructions: &instructions})
	if err != nil || session.ID == "" {
		t.Fatalf("strict native session = %+v, %v", session, err)
	}
	body, err := os.ReadFile(filepath.Join(req.Account.CWD, "trace"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--tools=", "--setting-sources=", "--strict-mcp-config", `{"mcpServers":{}}`, instructions} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("native strict launch omitted %q: %s", expected, body)
		}
	}
	if _, err := client.ResumeSession(t.Context(), session.ID, AgentSessionOptions{}); err != status_error.RuntimePolicyUnsupported {
		t.Fatalf("strict launch recovered an existing session: %v", err)
	}

	for _, provider := range []string{"codex", "cursor"} {
		req := providerTestRequest(t, provider)
		req.Launch.RequireNoTools = true
		conn, err := runtime.Open(t.Context(), req, ProviderConnectionPorts{})
		if err != status_error.RuntimePolicyUnsupported || conn == nil {
			t.Fatalf("%s accepted unproven tool restriction: %v", provider, err)
		}
		if _, err := os.Stat(filepath.Join(req.Account.CWD, "trace")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s started before strict launch refusal: %v", provider, err)
		}
	}
}

func TestProviderClientPreservesDetachedEvidenceWithoutInventingContext(t *testing.T) {
	usage := acpmeta.TurnUsage{Source: "fixture", Coverage: "invocation"}
	notification := acp.SessionNotification{
		Meta: acpmeta.WithTurnUsage(acpmeta.WithNativeTurn(nil, acpmeta.NativeTurn{
			InvocationID: "detached", Origin: acpmeta.NativeTurnOriginProvider, Status: acpmeta.NativeTurnCompleted,
		}), usage),
		Update: acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{Used: 0, Size: 1}},
	}
	event, ok := agentEvent(notification)
	if !ok || event.Usage == nil || event.NativeTurn == nil || event.NativeTurn.InvocationID != "detached" {
		t.Fatalf("detached native evidence was lost: %+v", event)
	}
	if event.ContextUsedTokens != nil || event.ContextWindowTokens != nil {
		t.Fatalf("placeholder context values were published: %+v", event)
	}
}

func TestProviderClientAccountHomeAndEnvironmentAuthority(t *testing.T) {
	for _, example := range []struct {
		provider, key string
	}{
		{"codex", "CODEX_HOME"}, {"claude", "CLAUDE_CONFIG_DIR"},
	} {
		t.Run(example.provider, func(t *testing.T) {
			for _, typed := range []bool{true, false} {
				request := providerTestRequest(t, example.provider)
				verified := t.TempDir()
				request.Launch.ConfigHome = t.TempDir()
				request.Launch.Environment["FIXTURE_HOME_VARIABLE"] = example.key
				runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) {
					lease := AccountLease{
						Environment: map[string]string{example.key: verified, "FIXTURE_AUTHORITY": "account"},
						PrepareLaunchAuth: func(context.Context) (LaunchAuthLease, error) {
							return LaunchAuthLease{Environment: map[string]string{
								example.key: "unverified-relative-path", "FIXTURE_AUTHORITY": "unverified",
							}}, nil
						},
					}
					if typed {
						lease.ConfigHome = verified
						if example.provider == "codex" {
							lease.Environment[example.key] = t.TempDir()
						} else {
							lease.ConfigHome, _ = filepath.EvalSymlinks(verified)
						}
					}
					return lease, nil
				}})
				t.Cleanup(func() { _ = runtime.Close(context.Background()) })
				conn, err := runtime.Open(t.Context(), request, ProviderConnectionPorts{})
				if err != nil {
					t.Fatal(err)
				}
				trace, err := os.ReadFile(filepath.Join(request.Account.CWD, "trace"))
				if err != nil || !strings.Contains(string(trace), "selected-home="+verified+"\nselected-auth=account\n") {
					t.Fatalf("account authority not passed to selected runtime: %s, %v", trace, err)
				}
				if err := conn.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestProviderClientMCPTrustsOnlySelectedProviderEvidence(t *testing.T) {
	for _, example := range []struct {
		provider, status string
	}{
		{"cursor", "load_unverified"}, {"claude", "verified"},
	} {
		t.Run(example.provider, func(t *testing.T) {
			request := providerTestRequest(t, example.provider)
			request.Launch.Environment["FIXTURE_MCP_REPORT"] = "1"
			runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: providerTestAccount})
			t.Cleanup(func() { _ = runtime.Close(context.Background()) })
			conn, err := runtime.Open(t.Context(), request, ProviderConnectionPorts{AcquireResources: func(context.Context, ResourceSelection) (ResourceLease, error) {
				return ResourceLease{MCPServers: []AgentMCPServer{{Name: "requested", Transport: "stdio", Command: "fixture-no-start"}}}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			client, err := conn.Client()
			if err != nil {
				t.Fatal(err)
			}
			session, err := client.NewSession(t.Context(), AgentSessionOptions{})
			if err != nil || session.MCPStatus != example.status {
				t.Fatalf("MCP readiness = %q, %v; want %q", session.MCPStatus, err, example.status)
			}
		})
	}
}

func TestProviderClientRejectsMCPTransportsMissingFromNativeHandshake(t *testing.T) {
	for _, transport := range []string{"http", "sse"} {
		t.Run(transport, func(t *testing.T) {
			request := providerTestRequest(t, "gemini")
			runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: providerTestAccount})
			t.Cleanup(func() { _ = runtime.Close(context.Background()) })
			conn, err := runtime.Open(t.Context(), request, ProviderConnectionPorts{AcquireResources: func(context.Context, ResourceSelection) (ResourceLease, error) {
				return ResourceLease{MCPServers: []AgentMCPServer{{Name: "requested", Transport: transport, URL: "https://example.invalid/mcp"}}}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			client, err := conn.Client()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.NewSession(t.Context(), AgentSessionOptions{}); !errors.Is(err, status_error.RuntimePolicyUnsupported) {
				t.Fatalf("native session silently dropped unsupported %s resource: %v", transport, err)
			}
		})
	}
}

func TestProviderClientResourceLinkPreservesOptionalMetadata(t *testing.T) {
	for _, original := range []acp.ContentBlockResourceLink{
		{Type: "resource_link", Uri: "file:///report", Name: "report"},
		{Type: "resource_link", Uri: "file:///report", Name: "report", Title: acp.Ptr(""), Description: acp.Ptr(""), Size: acp.Ptr(0)},
		{Type: "resource_link", Uri: "file:///report", Name: "report", Title: acp.Ptr("Report"), Description: acp.Ptr("Details"), Size: acp.Ptr(19)},
	} {
		content, ok := agentContent(acp.ContentBlock{ResourceLink: &original})
		if !ok {
			t.Fatal("valid resource link was not observed")
		}
		client := &directAgentClient{}
		blocks, err := client.promptContent("", []AgentContent{content})
		if err != nil || !reflect.DeepEqual(blocks[0].ResourceLink, &original) {
			t.Fatalf("round trip = %+v, %v; want %+v", blocks[0].ResourceLink, err, original)
		}
	}
}

func providerTestAccount(context.Context, AccountRequest) (AccountLease, error) {
	return AccountLease{}, nil
}

type steeringProjectionFixture struct {
	// Client supplies adapter operations unused by this test.
	native.Client
	// steer records direct requests and returns adapter receipts.
	steer func(context.Context, native.SteerRequest) (native.SteerResponse, error)
	// meta holds metadata negotiated during native initialization.
	meta map[string]any
}

func (f steeringProjectionFixture) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersionNumber, Meta: f.meta}, nil
}

func TestProviderClientHonorsNativeSteeringAdvertisement(t *testing.T) {
	enabled, disabled := true, false
	for _, tc := range []struct {
		// name identifies the capability evidence supplied by the adapter.
		name string
		// advertised is the optional native steering capability declaration.
		advertised *bool
		// want is the steering capability the direct client should expose.
		want bool
	}{
		{name: "absent metadata", want: true},
		{name: "explicitly supported", advertised: &enabled, want: true},
		{name: "explicitly unsupported", advertised: &disabled, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := steeringProjectionFixture{}
			if tc.advertised != nil {
				fixture.meta = map[string]any{"steering": map[string]any{"supported": *tc.advertised}}
			}
			client := &directAgentClient{native: fixture}
			if err := client.Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			if client.Capabilities().Steer != tc.want {
				t.Fatalf("steering capability = %t, want %t", client.Capabilities().Steer, tc.want)
			}
		})
	}
}

func (f steeringProjectionFixture) Steer(ctx context.Context, req native.SteerRequest) (native.SteerResponse, error) {
	return f.steer(ctx, req)
}

func TestProviderClientSteerProjectsExactAdmissionAndReceipt(t *testing.T) {
	expected := AgentScope{SessionID: "session", HostInvocationID: "opening", NativeInvocationID: "running"}
	nativeInput := "native input"
	cleaned := false
	calls := 0
	c := &directAgentClient{
		capabilities: AgentCapabilities{Steer: true},
		native: steeringProjectionFixture{steer: func(ctx context.Context, req native.SteerRequest) (native.SteerResponse, error) {
			calls++
			if req.SessionID != "session" || agentScope(req.Target) != expected || !req.PromptRequired ||
				len(req.Prompt) != 1 || req.Prompt[0].Text == nil || req.Prompt[0].Text.Text != "followup" ||
				req.Options.HostInvocationID != "followup host" {
				t.Fatalf("native steer request = %+v", req)
			}
			in := native.PromptSubmission{SessionID: "session", HostInvocationID: "followup host", InputID: "input", NativeInputID: &nativeInput}
			cleanup, err := req.Options.PrepareSubmission(ctx, in)
			if cleanup == nil {
				t.Fatal("preparation ownership was lost")
			}
			cleanup()
			return native.SteerResponse{Receipt: &native.PromptReceipt{Input: in, Submission: native.SubmissionNotSent,
				Admission: native.AdmissionNotAccepted, OperationError: err}}, err
		}},
	}
	req := AgentSteerRequest{SessionID: "session", Target: expected, HostInvocationID: "followup host", Text: "followup",
		Prepare: func(context.Context) (func(), error) { return func() { cleaned = true }, nil },
		BeforeSubmit: func(_ context.Context, in AgentPromptSubmission) error {
			if in.SessionID != "session" || in.HostInvocationID != "followup host" || in.InputID != "input" || *in.NativeInputID != "native input" {
				t.Fatalf("gate input = %+v", in)
			}
			*in.NativeInputID = "gate mutation"
			return status_error.AgentOperationForbidden
		},
	}
	result, err := c.Steer(t.Context(), req)
	if err != status_error.AgentOperationForbidden || !cleaned || result.Receipt == nil ||
		result.Receipt.OperationError != status_error.AgentOperationForbidden || result.Receipt.Submission != "not_sent" ||
		result.Receipt.Admission != "not_accepted" || *result.Receipt.Input.NativeInputID != "native input" {
		t.Fatalf("steer rejection = %+v, %v", result.Receipt, err)
	}
	*result.Receipt.Input.NativeInputID = "receipt mutation"
	if nativeInput != "native input" {
		t.Fatal("host receipt aliases the adapter's identity")
	}
	req.Target.SessionID = "another"
	if _, err := c.Steer(t.Context(), req); err == nil || calls != 1 {
		t.Fatalf("foreign scope dispatched: calls=%d, %v", calls, err)
	}
	req.Target = expected
	req.BeforeSubmit = nil
	if _, err := c.Steer(t.Context(), req); err == nil || calls != 1 {
		t.Fatalf("ungated steer dispatched: calls=%d, %v", calls, err)
	}
}

func TestProviderClientMessageReplacementRequiresExactTextIdentity(t *testing.T) {
	body := ""
	info := acpmeta.MessageInfo{Phase: acpmeta.MessagePhaseCommentary, Complete: true, Source: acpmeta.MessageSourceNativePhase,
		ReplacementText: &body}
	update := acp.UpdateAgentMessageText("")
	update.AgentMessageChunk.Meta = acpmeta.WithMessageInfo(nil, info)
	notification := acp.SessionNotification{Update: update}
	event, ok := agentEvent(notification)
	if !ok || event.Message == nil || event.Message.ReplacementText != nil {
		t.Fatalf("anonymous replacement was not isolated: %+v", event)
	}
	update.AgentMessageChunk.MessageId = acp.Ptr("exact-message")
	event, ok = agentEvent(notification)
	if !ok || event.MessageID != "exact-message" || event.Message == nil || event.Message.ReplacementText == nil ||
		*event.Message.ReplacementText != "" || event.Text != "" || event.Message.Phase != acpmeta.MessagePhaseCommentary {
		t.Fatalf("message replacement projection = %+v, info = %+v", event, event.Message)
	}
	*event.Message.ReplacementText = "consumer mutation"
	next, ok := agentEvent(notification)
	if !ok || next.Message.ReplacementText == nil || *next.Message.ReplacementText != "" {
		t.Fatalf("consumers share replacement storage: %+v", next.Message)
	}

	update.AgentMessageChunk.Content = acp.TextBlock("append")
	event, ok = agentEvent(notification)
	if !ok || event.Text != "append" || event.Message == nil || event.Message.ReplacementText != nil {
		t.Fatalf("append and replacement mixed: %+v", event)
	}
}

func TestManagedCodexEnvironmentUsesOnlySelectedCredential(t *testing.T) {
	launch := ProviderLaunch{BaseEnvironment: map[string]string{"OPENAI_API_KEY": "inherited", "CODEX_API_KEY": "other", "PATH": "/fixture"}, Environment: map[string]string{"OPENAI_BASE_URL": "https://wrong.invalid", "NEUX_CODEX_ACCOUNT_API_KEY": "wrong"}}
	lease := AccountLease{CodexCredentialKind: "api_key", ConfigHome: t.TempDir(), Environment: map[string]string{"NEUX_CODEX_ACCOUNT_API_KEY": "selected"}}
	env, err := nativeLaunchEnvironment("codex", launch, lease, LaunchAuthLease{}, ResourceLease{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "inherited") || strings.Contains(joined, "wrong") || strings.Contains(joined, "other") || !strings.Contains(joined, "NEUX_CODEX_ACCOUNT_API_KEY=selected") {
		t.Fatalf("credential authority lost: %s", joined)
	}
}

func TestNativeLaunchEnvironmentExcludesHostCLIConfiguration(t *testing.T) {
	env, err := nativeLaunchEnvironment("codex", ProviderLaunch{BaseEnvironment: map[string]string{"PATH": "/native/tools", "ALLYCLI_BINARY_PATH": "/host/allycli", "ALLYCLI_CONTEXT_FILE": "/old/context.json"}}, AccountLease{}, LaunchAuthLease{}, ResourceLease{Environment: map[string]string{"PLUGIN_RESOURCE": "ready"}})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		values[name] = value
	}
	if values["ALLYCLI_BINARY_PATH"] != "" || values["ALLYCLI_CONTEXT_FILE"] != "" {
		t.Fatalf("host CLI configuration leaked into native process: %v", values)
	}
	if values["PATH"] != "/native/tools" || values["PLUGIN_RESOURCE"] != "ready" {
		t.Fatalf("ordinary runtime environment changed: %v", values)
	}
}

func TestNativeToolCollectionsKeepOmittedEmptyAndNonemptyPresence(t *testing.T) {
	for _, mode := range []string{"omitted", "empty", "nonempty"} {
		t.Run(mode, func(t *testing.T) {
			update := acp.SessionToolCallUpdate{ToolCallId: "tool"}
			if mode == "empty" {
				update.Content, update.Locations = []acp.ToolCallContent{}, []acp.ToolCallLocation{}
			} else if mode == "nonempty" {
				update.Content = []acp.ToolCallContent{acp.ToolContent(acp.TextBlock("new content"))}
				update.Locations = []acp.ToolCallLocation{{Path: "/new/location"}}
			}
			note := acp.SessionNotification{SessionId: "session", Update: acp.SessionUpdate{ToolCallUpdate: &update}}
			encoded, err := json.Marshal(note)
			if err != nil {
				t.Fatal(err)
			}
			var cloned acp.SessionNotification
			if err := json.Unmarshal(encoded, &cloned); err != nil {
				t.Fatal(err)
			}
			event, ok := agentSessionEvent(cloned)
			if !ok || event.Tool == nil {
				t.Fatal("SDK tool update lost its event")
			}
			var buffer DeltaBuffer
			change, ok, err := buffer.messageChange(event)
			if err != nil || !ok {
				t.Fatalf("tool conversion failed: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(change.Data, &fields); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"content", "locations"} {
				value, present := fields[field]
				if mode == "omitted" && present || mode != "omitted" && !present || mode == "empty" && string(value) != "[]" {
					t.Fatalf("%s %s lost presence: %s", mode, field, change.Data)
				}
			}
			merged, err := mergePendingTool(json.RawMessage(`{"id":"tool","locations":[{"path":"/old/location"}],"content":["old"]}`), change.Data)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "omitted" && !strings.Contains(string(merged), "/old/location") ||
				mode == "empty" && (!strings.Contains(string(merged), `"locations":[]`) || !strings.Contains(string(merged), `"content":[]`)) ||
				mode == "nonempty" && (!strings.Contains(string(merged), "/new/location") || !strings.Contains(string(merged), "new content")) {
				t.Fatalf("coalescing changed collection presence: %s", merged)
			}
		})
	}
}

func TestNativeLaunchPluginPathPreservesCLIInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX env shebang regression")
	}
	tools, private := t.TempDir(), t.TempDir()
	interpreter := filepath.Join(tools, "neux-test-interpreter")
	if err := os.WriteFile(interpreter, []byte("#!/bin/sh\nprintf initialized"), 0700); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(t.TempDir(), "native-cli")
	if err := os.WriteFile(cli, []byte("#!/usr/bin/env neux-test-interpreter\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"codex", "claude", "cursor", "workbuddy", "qoder", "kimi", "gemini", "grok", "opencode", "pi"} {
		t.Run(provider, func(t *testing.T) {
			launch := ProviderLaunch{BaseEnvironment: map[string]string{"PATH": tools}}
			resources := ResourceLease{Environment: map[string]string{"PATH": private}}
			env, err := nativeLaunchEnvironment(provider, launch, AccountLease{}, LaunchAuthLease{}, resources)
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(cli)
			command.Env = env
			output, err := command.CombinedOutput()
			if err != nil || string(output) != "initialized" {
				t.Fatalf("plugin PATH broke native CLI interpreter: %v %s", err, output)
			}
			var path string
			for _, entry := range env {
				if key, value, ok := strings.Cut(entry, "="); ok && strings.EqualFold(key, "PATH") {
					path = value
				}
			}
			if path != private+string(filepath.ListSeparator)+tools {
				t.Fatalf("private tooling lost precedence or inherited interpreter path: %s", path)
			}
			if launch.BaseEnvironment["PATH"] != tools || resources.Environment["PATH"] != private {
				t.Fatal("launch environment inputs were mutated")
			}
		})
	}
}

func TestNativeLaunchPluginPathMergesSelectedAccountPathAndDeduplicates(t *testing.T) {
	private, selected, inherited := t.TempDir(), t.TempDir(), t.TempDir()
	for _, provider := range []string{"codex", "workbuddy", "kimi"} {
		env, err := nativeLaunchEnvironment(provider,
			ProviderLaunch{BaseEnvironment: map[string]string{"PATH": inherited}, Environment: map[string]string{"PATH": selected}},
			AccountLease{Environment: map[string]string{"PATH": selected}}, LaunchAuthLease{},
			ResourceLease{Environment: map[string]string{"PATH": private + string(filepath.ListSeparator) + selected + string(filepath.ListSeparator) + private}})
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range env {
			if key, value, _ := strings.Cut(entry, "="); strings.EqualFold(key, "PATH") && value != private+string(filepath.ListSeparator)+selected {
				t.Fatalf("%s used ambient or duplicate paths: %s", provider, value)
			}
		}
	}
}

func TestNativePluginViewsAreIsolatedAndHoldFilesUntilLastConsumer(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(`{"name":"one"}`), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := plugins.ContentDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	targets := []plugins.NativeTarget{{ID: "id-a", Name: "one", Version: "1", Root: source, Digest: digest, Runtime: "cursor"}, {ID: "id-b", Name: "two", Version: "1", Root: source, Digest: digest, Runtime: "cursor"}}
	cwd, other := t.TempDir(), t.TempDir()
	first, release, err := AcquirePluginViews(t.Context(), Workspace{Managed: true, Path: cwd}, targets)
	if err != nil {
		t.Fatal(err)
	}
	second, releaseSecond, err := AcquirePluginViews(t.Context(), Workspace{Managed: true, Path: cwd}, targets)
	if err != nil {
		t.Fatal(err)
	}
	third, releaseThird, err := AcquirePluginViews(t.Context(), Workspace{Managed: true, Path: other}, targets)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseThird(t.Context())
	if len(first) != 2 || first[0].Directory == first[1].Directory || first[0].Directory != second[0].Directory || first[0].Directory == third[0].Directory {
		t.Fatal(first, second, third)
	}
	if err := release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(first[0].Directory, "plugin.json")); err != nil {
		t.Fatal(err)
	}
	if err := releaseSecond(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first[0].Directory); !os.IsNotExist(err) {
		t.Fatalf("last release retained view: %v", err)
	}
}

func TestCodexPluginsUseOnlyLeasedAccountCacheAndLaunchConfiguration(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(`{"name":"office","version":"1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := plugins.ContentDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	targets, release, err := AcquirePluginViews(t.Context(), Workspace{Managed: true, Path: cwd}, []plugins.NativeTarget{{ID: "one", Name: "office", Version: "1", Root: source, Digest: digest, Runtime: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	defer release(t.Context())
	account := AccountRequest{AccountID: "managed", Provider: "codex", ConfigHome: t.TempDir()}
	lease := AccountLease{ConfigHome: account.ConfigHome}
	config, done, err := prepareCodexPluginCache(t.Context(), account, lease, targets)
	if err != nil {
		t.Fatal(err)
	}
	defer done(t.Context())
	if config["features.plugins"] == nil || config["plugins"] == nil {
		t.Fatal(config)
	}
	if _, err := os.Stat(filepath.Join(account.ConfigHome, "plugins", "cache", targets[0].Marketplace, "office", "1", "plugin.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(account.ConfigHome, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("account config changed")
	}
	if _, err := os.Stat(filepath.Join(targets[0].MarketplaceRoot, ".agents", "plugins", "marketplace.json")); err != nil {
		t.Fatal(err)
	}
	account.AccountID = SystemAccountID
	if _, _, err := prepareCodexPluginCache(t.Context(), account, lease, targets); err == nil {
		t.Fatal("system account cache write permitted")
	}
}

// TestResourceLeasePluginDirectoriesReachNativeClient exercises the production ProviderRuntime factory.
func TestResourceLeasePluginDirectoriesReachNativeClient(t *testing.T) {
	for _, provider := range []string{"cursor", "claude", "workbuddy", "qoder"} {
		t.Run(provider, func(t *testing.T) {
			req := providerTestRequest(t, provider)
			first, _ := filepath.EvalSymlinks(t.TempDir())
			second, _ := filepath.EvalSymlinks(t.TempDir())
			owner := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: providerTestAccount})
			t.Cleanup(func() { _ = owner.Close(context.Background()) })
			conn, err := owner.Open(t.Context(), req, ProviderConnectionPorts{AcquireResources: func(context.Context, ResourceSelection) (ResourceLease, error) {
				return ResourceLease{NativePlugins: []NativePlugin{{Provider: provider, Directory: first}, {Provider: provider, Directory: second}}}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if provider == "claude" {
				client, err := conn.Client()
				if err != nil {
					t.Fatal(err)
				}
				if _, err = client.NewSession(t.Context(), AgentSessionOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			trace, err := os.ReadFile(filepath.Join(req.Account.CWD, "trace"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(trace), "--plugin-dir") || !strings.Contains(string(trace), first) || !strings.Contains(string(trace), second) {
				t.Fatalf("typed plugin roots missing from launch: %s", trace)
			}
		})
	}
}

func TestPluginViewReleaseNeverRemovesAReplacementDirectory(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := plugins.ContentDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	targets, closeFiles, err := AcquirePluginViews(t.Context(), Workspace{Path: t.TempDir(), Managed: true}, []plugins.NativeTarget{{ID: "id", Name: "one", Version: "1", Root: source, Digest: digest, Runtime: "cursor"}})
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Dir(targets[0].Directory)
	if err = os.Rename(original, original+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(original, "user-file")
	if err = os.WriteFile(sentinel, []byte("user content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = closeFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(sentinel); err != nil {
		t.Fatalf("lease deleted a different workspace directory: %v", err)
	}
}

func TestPluginDirectoryBusyViewDoesNotBlockAnotherWorkspace(t *testing.T) {
	busy, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lockPluginDirectory(t.Context(), filepath.Join(busy, "view"))
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	other := t.TempDir()
	go func() {
		_, release, err := acquirePluginDirectory(t.Context(), other, "view", nil, map[string][]byte{"plugin.json": []byte("{}")})
		if err == nil {
			err = release(t.Context())
		}
		completed <- err
	}()
	select {
	case err := <-completed:
		unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		unlock()
		<-completed
		t.Fatal("an unrelated workspace waited for the busy plugin view")
	}
}

func TestPluginDirectoryWaitAndReleaseHonorCancellation(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := acquirePluginDirectory(t.Context(), parent, "view", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lockPluginDirectory(t.Context(), filepath.Join(parent, "view"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	completed := make(chan error, 1)
	go func() { completed <- release(ctx) }()
	select {
	case err := <-completed:
		unlock()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled release = %v", err)
		}
	case <-time.After(time.Second):
		unlock()
		<-completed
		t.Error("cancelled release waited for busy plugin files")
	}
	if _, err := os.Stat(filepath.Join(parent, "view")); err != nil {
		t.Fatal("cancelled release removed a leased view", err)
	}
	if err := release(t.Context()); err != nil {
		t.Fatal(err)
	}
}
