package agentcli

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPBridgePreservesPreDispatchValidationFailure(t *testing.T) {
	registry := agentcapability.New(nil)
	defer registry.Close()
	var failValidation atomic.Bool
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: "save-execution", Validate: func(context.Context) error {
		if failValidation.Load() {
			return status_error.DatabaseError
		}
		return nil
	}, Bindings: []agentcapability.Binding{{Operation: "workflow.source.save", Call: func(context.Context, agentcapability.Request) (agentcapability.Result, error) {
		t.Error("pre-dispatch failure must not reach the mutation")
		return agentcapability.Result{}, nil
	}, Receipt: func(context.Context, string) (agentcapability.Result, error) {
		t.Error("pre-dispatch failure must not query a mutation receipt")
		return agentcapability.Result{}, nil
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	defer revoke()
	httpServer := httptest.NewServer(registry)
	defer httpServer.Close()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := agentcapability.WriteConnection(directory, "save.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: httpServer.URL + constants.AgentCLIPath, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := newMCPServer(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	a, b := mcp.NewInMemoryTransports()
	serverSession, err := bridge.Connect(context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	peer := mcp.NewClient(&mcp.Implementation{Name: "validation-failure-test", Version: "1"}, nil)
	session, err := peer.Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listing, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var outputSchema map[string]any
	for _, tool := range listing.Tools {
		if tool.Name == "workflow_source_save" {
			raw, _ := json.Marshal(tool.OutputSchema)
			if err := json.Unmarshal(raw, &outputSchema); err != nil {
				t.Fatal(err)
			}
		}
	}
	if outputSchema == nil {
		t.Fatal("bound save tool missing")
	}
	failValidation.Store(true)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "workflow_source_save", Arguments: map[string]any{"requestId": "save-once", "source": "return true;", "expectedRevisionId": "head"}})
	if err != nil {
		t.Fatalf("MCP schema masked the original validation failure: %v", err)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var wire any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if err := workflowdsl.ValidateJSON(outputSchema, wire); err != nil {
		t.Fatalf("validation failure violates advertised MCP output schema: %v response=%s", err, raw)
	}
	var response agentcapability.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if !result.IsError || response.OK || response.Operation != "workflow.source.save" || response.Error == nil || response.Error.Code != status_error.DatabaseError.Key() || response.Error.EffectState != "not_accepted" || response.Error.RetryAdvice != "none" {
		t.Fatalf("original failure or acceptance state changed at MCP bridge: %s", raw)
	}
}

func TestMCPBridgeUsesBoundOperationsAndFrozenGrant(t *testing.T) {
	registry := agentcapability.New(nil)
	defer registry.Close()
	calls := 0
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: "first", Validate: func(context.Context) error { return nil }, Bindings: []agentcapability.Binding{{Operation: "workflow.source.get", Call: func(_ context.Context, req agentcapability.Request) (agentcapability.Result, error) {
		calls++
		return agentcapability.Result{Data: map[string]any{"workflowId": "bound", "source": "source"}}, nil
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	defer revoke()
	httpServer := httptest.NewServer(registry)
	defer httpServer.Close()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	connection := agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: httpServer.URL + constants.AgentCLIPath, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token}
	path, err := agentcapability.WriteConnection(dir, "first.json", connection)
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := newMCPServer(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	a, b := mcp.NewInMemoryTransports()
	serverSession, err := bridge.Connect(context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	peer := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := peer.Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listing, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range listing.Tools {
		if tool.Name == "workflow_source_get" {
			found = true
		}
		if tool.Name == "workflow_source_save" {
			t.Fatal("unbound write tool exposed")
		}
	}
	if !found {
		t.Fatal("bound source tool missing")
	}
	call := func() *mcp.CallToolResult {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "workflow_source_get", Arguments: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := call()
	if result.IsError || calls != 1 {
		t.Fatalf("call=%+v count=%d", result, calls)
	}
	revoke()
	next, activateNext, revokeNext, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: "second", Validate: func(context.Context) error { return nil }, Bindings: []agentcapability.Binding{{Operation: "workflow.source.get", Call: func(context.Context, agentcapability.Request) (agentcapability.Result, error) {
		calls++
		return agentcapability.Result{}, nil
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	activateNext()
	defer revokeNext()
	connection.GrantID, connection.Token = next.ID, next.Token
	if _, err := agentcapability.WriteConnection(dir, "first.json", connection); err != nil {
		t.Fatal(err)
	}
	result = call()
	raw, _ := json.Marshal(result.StructuredContent)
	if !result.IsError || calls != 1 {
		t.Fatalf("old bridge acquired new grant: %s calls=%d", raw, calls)
	}
}

func TestMCPBridgeReturnsSavedRevisionWithManifestDigest(t *testing.T) {
	registry := agentcapability.New(nil)
	defer registry.Close()
	receipt := workflowdsl.SaveReceipt{DefinitionID: "workflow", RevisionID: "revision", Ordinal: 2, SourceDigest: "source-digest", ManifestDigest: "canvas-digest"}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}
	data["saved"] = true
	calls := 0
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: "save-execution", Validate: func(context.Context) error { return nil }, Bindings: []agentcapability.Binding{{Operation: "workflow.source.save", Call: func(_ context.Context, request agentcapability.Request) (agentcapability.Result, error) {
		calls++
		if request.RequestID != "save-once" {
			t.Errorf("request identity changed: %+v", request)
		}
		return agentcapability.Result{CommandID: "command", Data: data}, nil
	}, Receipt: func(context.Context, string) (agentcapability.Result, error) {
		return agentcapability.Result{CommandID: "command", Data: data}, nil
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	defer revoke()
	httpServer := httptest.NewServer(registry)
	defer httpServer.Close()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := agentcapability.WriteConnection(directory, "save.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: httpServer.URL + constants.AgentCLIPath, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := newMCPServer(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	a, b := mcp.NewInMemoryTransports()
	serverSession, err := bridge.Connect(context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	peer := mcp.NewClient(&mcp.Implementation{Name: "save-test", Version: "1"}, nil)
	session, err := peer.Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listing, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var outputSchema map[string]any
	for _, tool := range listing.Tools {
		if tool.Name == "workflow_source_save" {
			raw, _ := json.Marshal(tool.OutputSchema)
			if err := json.Unmarshal(raw, &outputSchema); err != nil {
				t.Fatal(err)
			}
		}
	}
	if outputSchema == nil {
		t.Fatal("save tool missing output schema")
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "workflow_source_save", Arguments: map[string]any{"requestId": "save-once", "source": "return true;", "expectedRevisionId": "head"}})
	if err != nil {
		t.Fatalf("saved revision rejected by MCP output schema: %v", err)
	}
	if result.IsError || calls != 1 {
		t.Fatalf("MCP reports error after successful save: result=%+v calls=%d", result, calls)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var wire any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if err := workflowdsl.ValidateJSON(outputSchema, wire); err != nil {
		t.Fatalf("actual MCP response violates its advertised output schema: %v", err)
	}
	var response agentcapability.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	returned, ok := response.Data.(map[string]any)
	if !response.OK || !ok || returned["saved"] != true || returned["manifestDigest"] != receipt.ManifestDigest || returned["revisionId"] != receipt.RevisionID {
		t.Fatalf("save receipt changed: %s", raw)
	}
}
