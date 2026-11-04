package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	acp "github.com/JieWaZi/acp-go/sdk"
)

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

func providerTestAccount(context.Context, AccountRequest) (AccountLease, error) {
	return AccountLease{}, nil
}
