package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
)

func TestCanceledMutationPreservesUnknownEffectAndReceiptAdvice(t *testing.T) {
	accepted, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		close(accepted)
		<-release
	}))
	defer server.Close()
	defer close(release)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := agentcapability.WriteConnection(root, "connection.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: server.URL + constants.AgentCLIPath, CoreEpoch: "epoch", GrantID: "grant", Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case <-accepted:
			cancel()
		case <-ctx.Done():
		}
	}()
	var out, errOut bytes.Buffer
	code := Execute(ctx, []string{"im", "deliveries", "create", "--target-id", "target", "--content", "report", "--request-id", "stable-report"}, IO{Out: &out, Err: &errOut, ContextFile: path})
	var response agentcapability.Response
	if code != 130 || out.Len() != 0 || json.Unmarshal(errOut.Bytes(), &response) != nil || response.Error == nil || response.Error.Code != "AgentCommandOutcomeUnknown" || response.Error.RetryAdvice != "query_receipt" || response.Error.EffectState != "unknown" {
		t.Fatalf("canceled write lost original uncertainty: code=%d stdout=%s stderr=%s", code, &out, &errOut)
	}
}

func TestCommandsUseCurrentGrantAndSeparateOutputStreams(t *testing.T) {
	registry := agentcapability.New(func(string) (string, error) { return "Only this guide.", nil })
	defer registry.Close()
	var calls []agentcapability.Request
	binding := agentcapability.Binding{Operation: "im.deliveries.create", Call: func(_ context.Context, input agentcapability.Request) (agentcapability.Result, error) {
		calls = append(calls, input)
		return agentcapability.Result{CommandID: "durable-delivery", Data: map[string]any{"state": "pending"}}, nil
	}, Receipt: func(context.Context, string) (agentcapability.Result, error) {
		return agentcapability.Result{CommandID: "durable-delivery", Data: map[string]any{"state": "sent"}}, nil
	}}
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: "actual-execution", Bindings: []agentcapability.Binding{binding}, Validate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	activate()
	server := httptest.NewServer(registry)
	defer server.Close()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contextFile, err := agentcapability.WriteConnection(root, "connection.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: server.URL + constants.AgentCLIPath, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		t.Fatal(err)
	}
	run := func(args []string, input string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, IO{In: strings.NewReader(input), Out: &out, Err: &errOut, ContextFile: contextFile})
		return code, out.String(), errOut.String()
	}
	code, out, errOut := run([]string{"im", "deliveries", "create", "--input", "-", "--request-id", "report-01"}, `{"targetId":"known-target","content":"report"}`)
	if code != 0 || errOut != "" || len(calls) != 1 || calls[0].RequestID != "report-01" || !strings.Contains(out, `"completion":"accepted"`) {
		t.Fatalf("code=%d stdout=%s stderr=%s calls=%v", code, out, errOut, calls)
	}
	if strings.Contains(out, grant.Token) {
		t.Fatal("credential leaked in stdout")
	}
	code, out, errOut = run([]string{"im", "deliveries", "create", "--input", "-", "--request-id", "report-02"}, `{"targetId":"known-target","content":"report","executionId":"spoof"}`)
	if code != 65 || out != "" || !strings.Contains(errOut, "AgentInputInvalid") || len(calls) != 1 {
		t.Fatalf("invalid input reached business handler: %d %q %q", code, out, errOut)
	}
	code, out, errOut = run([]string{"im", "deliveries", "create", "--input", "-", "--target-id", "different", "--request-id", "report-03"}, `{"targetId":"known-target","content":"report"}`)
	if code != 65 || out != "" || len(calls) != 1 {
		t.Fatalf("duplicate sources accepted: %d %q %q", code, out, errOut)
	}
	code, out, errOut = run([]string{"receipts", "get", "--operation", "im.deliveries.create", "--request-id", "report-01"}, "")
	if code != 0 || errOut != "" || !strings.Contains(out, `"replayed":true`) || !strings.Contains(out, `"state":"sent"`) {
		t.Fatalf("receipt: %d %q %q", code, out, errOut)
	}
	revoke()
	code, out, errOut = run([]string{"capabilities"}, "")
	if code != 77 || out != "" || !strings.Contains(errOut, "AgentContextUnauthorized") {
		t.Fatalf("revoked grant: %d %q %q", code, out, errOut)
	}
}

func TestHelpAndVersionNeedNoContext(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"workflow", "source", "save", "--help"}} {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 || out.Len() == 0 || errOut.Len() != 0 {
			t.Fatalf("%v code=%d stdout=%s stderr=%s", args, code, &out, &errOut)
		}
	}
	var out, errOut bytes.Buffer
	code := Execute(context.Background(), []string{"not-a-command"}, IO{Out: &out, Err: &errOut})
	var failure agentcapability.Response
	if code != 2 || out.Len() != 0 || json.Unmarshal(errOut.Bytes(), &failure) != nil || failure.OK {
		t.Fatalf("usage: %d %s %s", code, &out, &errOut)
	}
}

func TestRetiredKnowledgeMCPCommandIsNotAvailable(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute(context.Background(), []string{"serve-knowledge-mcp", "--help"}, IO{Out: &out, Err: &errOut})
	if code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "AgentInputInvalid") {
		t.Fatalf("retired MCP transport is still exposed: code=%d out=%s err=%s", code, &out, &errOut)
	}
}

func TestMemoryScopeHelpAndInvalidInputUseTheSharedContract(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute(context.Background(), []string{"memory", "entries", "search", "--help"}, IO{Out: &out, Err: &errOut})
	if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "choices: partner, project, all") ||
		!strings.Contains(out.String(), `default: "all"`) {
		t.Fatalf("memory search help omitted its legal scope/default: %d %s %s", code, &out, &errOut)
	}
	out.Reset()
	errOut.Reset()
	code = Execute(context.Background(), []string{"memory", "entries", "create", "--scope", "all", "--content", "Stable fact", "--request-id", "invalid-scope"}, IO{Out: &out, Err: &errOut})
	var response agentcapability.Response
	if code != 65 || out.Len() != 0 || json.Unmarshal(errOut.Bytes(), &response) != nil ||
		response.Error == nil || response.Error.Code != "AgentInputInvalid" || response.Error.Details["discovery"] == nil {
		t.Fatalf("invalid memory ownership was not rejected with discovery before connection: %d %s %s", code, &out, &errOut)
	}
}
