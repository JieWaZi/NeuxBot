package mcpconnector

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/mcpclient"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestChatConnectorLifecycleUsesApprovalAndScopedReceipts(t *testing.T) {
	service := testConnectorService(t)
	ctx := t.Context()
	approvals := 0
	probes := 0
	service.config.Probe = func(_ context.Context, connector models.MCPConnector) mcpclient.Result {
		probes++
		if approvals != probes || !Trusted(connector) {
			t.Fatal("a connector was probed before the user confirmed its configuration")
		}
		return mcpclient.Result{Status: constants.MCPTestStatusSucceeded, Tools: []models.MCPToolSummary{{Name: "search"}}}
	}
	var changes []string
	bindings := service.ManagementBindings("owner", "execution-1", func(context.Context, string) error {
		approvals++
		return nil
	}, func(_ context.Context, action string, result capability.Result) {
		if result.CommandID == "" || result.Data.(map[string]any)["connectorId"] == "" {
			t.Fatalf("change did not come from a real connection receipt: %+v", result)
		}
		changes = append(changes, action)
	})
	run := func(operation, requestID string, params map[string]any) (capability.Result, error) {
		t.Helper()
		return callBinding(t, ctx, bindings, operation, requestID, params)
	}
	create := map[string]any{"name": "Notes", "transport": "streamable_http", "url": "https://example.test/mcp"}
	created, err := run("create", "create", create)
	if err != nil {
		t.Fatal(err)
	}
	initial := created.Data.(map[string]any)
	id := initial["connectorId"].(string)
	checkRevision := func(receipt map[string]any) {
		t.Helper()
		saved, err := run("get", "", map[string]any{"connectorId": id})
		if err != nil || saved.Data.(map[string]any)["revision"] != receipt["revision"] {
			t.Fatalf("receipt revision differs from saved revision: receipt=%v saved=%+v err=%v", receipt["revision"], saved.Data, err)
		}
	}
	checkRevision(initial)
	if initial["enabled"] != true || initial["scope"] != "public" || initial["testStatus"] != "succeeded" {
		t.Fatalf("unexpected create result: %+v", initial)
	}
	if active, err := service.config.Connectors.ExecutionDeclarations(ctx, "owner"); err != nil || len(active) != 1 || active[0].ID != id {
		t.Fatalf("the tested connector is missing from the next turn: %+v %v", active, err)
	}
	if saved, err := service.Visible(ctx, "", id); err != nil || saved.Connector.OwnerWakerID != "" || saved.Connector.TestToolCount != 1 {
		t.Fatalf("the connector should be installable from the library with its test result: %v", err)
	}
	replay, err := run("create", "create", create)
	if err != nil || !replay.Replayed || replay.CommandID != created.CommandID || approvals != 1 || probes != 1 {
		t.Fatalf("create replay: %+v %v, %d approvals", replay, err, approvals)
	}
	getOther := service.ManagementBindings("other", "execution-2", nil, nil)
	if _, err := callBinding(t, ctx, getOther, "get", "", map[string]any{"connectorId": id}); !matchesStatus(err, status_error.MCPConnectorNotFound) {
		t.Fatalf("another partner read a connector without installing it: %v", err)
	}
	update := map[string]any{"connectorId": id, "expectedRevision": initial["revision"], "url": "https://example.test/mcp/v2"}
	edited, err := run("update", "update", update)
	if err != nil {
		t.Fatal(err)
	}
	current := edited.Data.(map[string]any)
	checkRevision(current)
	if current["name"] != "Notes" || current["revision"] == initial["revision"] || approvals != 2 {
		t.Fatalf("edit result: %+v, %d approvals", current, approvals)
	}
	if _, err := run("update", "stale", update); !errors.Is(err, status_error.Conflict) || approvals != 2 {
		t.Fatalf("stale update: %v, %d approvals", err, approvals)
	}
	deleted, err := run("delete", "delete", map[string]any{"connectorId": id, "expectedRevision": current["revision"]})
	if err != nil || deleted.Data.(map[string]any)["deleted"] != true || approvals != 3 {
		t.Fatalf("delete: %+v %v, %d approvals", deleted, err, approvals)
	}
	if got := strings.Join(changes, ","); got != "create,create,update,delete" {
		t.Fatalf("unexpected callbacks for reads, writes or retries: %s", got)
	}
	if _, err := run("get", "", map[string]any{"connectorId": id}); !matchesStatus(err, status_error.MCPConnectorNotFound) {
		t.Fatalf("deleted connector remains visible: %v", err)
	}
}

func TestChatConnectorKeepsFailedCreationDisabledAndSkipsDeclinedProbe(t *testing.T) {
	service := testConnectorService(t)
	ctx := t.Context()
	probes := 0
	service.config.Probe = func(context.Context, models.MCPConnector) mcpclient.Result {
		probes++
		return mcpclient.Result{Status: constants.MCPTestStatusFailed, Error: "Connection refused"}
	}
	declined := true
	bindings := service.ManagementBindings("owner", "execution-1", func(context.Context, string) error {
		if declined {
			return status_error.AgentOperationForbidden
		}
		return nil
	}, nil)
	parameters := map[string]any{"name": "Unavailable", "transport": "streamable_http", "url": "https://example.test/mcp"}
	if _, err := callBinding(t, ctx, bindings, "create", "declined", parameters); !errors.Is(err, status_error.AgentOperationForbidden) || probes != 0 {
		t.Fatalf("a declined creation must not test the endpoint: %d %v", probes, err)
	}
	declined = false
	result, err := callBinding(t, ctx, bindings, "create", "accepted", parameters)
	if err != nil || probes != 1 {
		t.Fatalf("failed test should preserve the approved configuration: %d %v", probes, err)
	}
	data := result.Data.(map[string]any)
	if data["enabled"] != false || data["testStatus"] != "failed" {
		t.Fatalf("unreachable connector was not reported as disabled: %+v", data)
	}
	if enabled, err := service.config.Connectors.ExecutionDeclarations(ctx, "owner"); err != nil || len(enabled) != 0 {
		t.Fatalf("unreachable connector entered the next turn: %+v %v", enabled, err)
	}
}

func TestChatConnectorProbesBeforeReplacingEnabledConfiguration(t *testing.T) {
	service := testConnectorService(t)
	ctx := t.Context()
	current, err := service.Create(ctx, "owner", Definition{
		Name: "Notes", Transport: constants.MCPTransportStreamableHTTP, URL: "https://example.test/mcp", TrustConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	approvals, probes := 0, 0
	status := constants.MCPTestStatusFailed
	const proposed = "https://example.test/mcp/v2"
	service.config.Probe = func(_ context.Context, connector models.MCPConnector) mcpclient.Result {
		probes++
		if connector.URL != proposed || approvals != probes {
			t.Fatalf("incorrect probe target or ordering: %s, %d approvals, %d probes", connector.URL, approvals, probes)
		}
		return mcpclient.Result{Status: status}
	}
	bindings := service.ManagementBindings("owner", "execution-1", func(_ context.Context, reason string) error {
		approvals++
		if !strings.Contains(reason, proposed) {
			t.Fatalf("approval did not show the proposed endpoint: %s", reason)
		}
		return nil
	}, nil)
	params := map[string]any{"connectorId": current.Connector.ID, "expectedRevision": connectorRevision(current.Connector), "url": proposed}
	if _, err := callBinding(t, ctx, bindings, "update", "probe-failed", params); !errors.Is(err, status_error.MCPConnectorProbeFailed) || probes != 1 {
		t.Fatalf("failed probe did not block enabled configuration change: %v, %d probes", err, probes)
	}
	original, err := service.Visible(ctx, "owner", current.Connector.ID)
	if err != nil || !original.Enabled || original.Connector.URL != current.Connector.URL {
		t.Fatalf("failed edit modified working configuration: %+v %v", original, err)
	}
	status = constants.MCPTestStatusSucceeded
	if _, err := callBinding(t, ctx, bindings, "update", "probe-success", params); err != nil || probes != 2 {
		t.Fatalf("successful probe did not permit edit: %v, %d probes", err, probes)
	}
	updated, err := service.Visible(ctx, "owner", current.Connector.ID)
	if err != nil || !updated.Enabled || updated.Connector.URL != proposed {
		t.Fatalf("verified edit was not saved: %+v %v", updated, err)
	}
}

func TestChatConnectorRejectsCredentialArgumentsBeforeApproval(t *testing.T) {
	service := testConnectorService(t)
	bindings := service.ManagementBindings("owner", "execution-1", func(context.Context, string) error {
		t.Fatal("credential argument reached approval display")
		return nil
	}, nil)
	for _, args := range [][]string{{"--api-key", "value"}, {"MCP_ACCESS_TOKEN=value"}, {"--header", "Authorization: value"}} {
		_, err := callBinding(t, t.Context(), bindings, "create", "secret", map[string]any{
			"name": "Notes", "transport": "stdio", "command": "notes-server", "arguments": args,
		})
		if !errors.Is(err, status_error.AgentInputInvalid) {
			t.Fatalf("credential argument result: %v", err)
		}
	}
}

func matchesStatus(err error, want status_error.StatusError) bool {
	var status interface{ Code() int64 }
	return errors.As(err, &status) && status.Code() == want.Code()
}

func TestChatConnectorKeepsCredentialsAndProbeOutputProtected(t *testing.T) {
	service := testConnectorService(t)
	ctx := t.Context()
	const secret = "private-test-credential"
	item, err := service.Create(ctx, "owner", Definition{
		Name: "Notes", Transport: constants.MCPTransportStreamableHTTP, URL: "https://example.test/mcp",
		Headers: []SecretInput{{Name: "Authorization", Value: secret}}, TrustConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	deny := true
	approvals := 0
	service.config.Probe = func(_ context.Context, connector models.MCPConnector) mcpclient.Result {
		calls++
		if connector.Headers[0].Value != secret {
			t.Fatalf("configured credentials lost at the approved connection: %+v", connector.Headers)
		}
		return mcpclient.Result{Status: constants.MCPTestStatusFailed, Error: secret, Tools: []models.MCPToolSummary{{Name: secret}}}
	}
	bindings := service.ManagementBindings("owner", "execution-1", func(context.Context, string) error {
		approvals++
		if deny {
			return status_error.AgentOperationForbidden
		}
		return nil
	}, nil)
	ids := map[string]any{"connectorId": item.Connector.ID}
	for _, action := range []string{"list", "get"} {
		params := ids
		if action == "list" {
			params = nil
		}
		result, err := callBinding(t, ctx, bindings, action, "", params)
		encoded, _ := json.Marshal(result.Data)
		if err != nil || !strings.Contains(string(encoded), `"hasCredentials":true`) || strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "Authorization") {
			t.Fatalf("%s credential response: %s %v", action, encoded, err)
		}
	}
	if _, err := callBinding(t, ctx, bindings, "update", "move", map[string]any{
		"connectorId": item.Connector.ID, "expectedRevision": connectorRevision(item.Connector), "url": "https://other.test/mcp",
	}); !errors.Is(err, status_error.MCPConnectorTrustRequired) || approvals != 0 {
		t.Fatalf("moving credentials should be rejected before approval: %v, %d approvals", err, approvals)
	}
	if _, err := callBinding(t, ctx, bindings, "test", "", ids); !errors.Is(err, status_error.AgentOperationForbidden) || calls != 0 {
		t.Fatalf("probe should not run without user approval: %v, %d probe calls", err, calls)
	}
	deny = false
	tested, err := callBinding(t, ctx, bindings, "test", "", ids)
	encoded, _ := json.Marshal(tested.Data)
	if err != nil || strings.Contains(string(encoded), secret) || calls != 1 {
		t.Fatalf("approved probe exposed raw server details: %s %v, %d probe calls", encoded, err, calls)
	}
	if saved, err := service.Visible(ctx, "owner", item.Connector.ID); err != nil || saved.Connector.TestStatus != constants.MCPTestStatusUntested {
		t.Fatalf("read-only probe changed connector: %+v %v", saved, err)
	}
}

func TestChatConnectorRechecksSharedApprovalAndDetachesOnlyCurrentPartner(t *testing.T) {
	service := testConnectorService(t)
	ctx := t.Context()
	shared, err := service.Create(ctx, "", Definition{
		Name: "Shared", Transport: constants.MCPTransportStreamableHTTP, URL: "https://example.test/mcp", TrustConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := shared.Connector.ID
	attach := func(waker string) {
		t.Helper()
		err := service.config.Connectors.EnsureWakerConnector(ctx, models.WakerMCPConnector{
			ID: "relation-" + waker, MCPConnectorID: id, WakerID: waker, Enabled: true,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	attach("owner")
	attach("other")
	swap := false
	bindings := service.ManagementBindings("owner", "execution-1", func(context.Context, string) error {
		if swap {
			return service.config.Connectors.DeleteWakerConnector(ctx, "other", id)
		}
		return nil
	}, nil)
	params := map[string]any{"connectorId": id, "expectedRevision": connectorRevision(shared.Connector)}
	swap = true
	if _, err := callBinding(t, ctx, bindings, "delete", "stale-shared", params); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("global delete used a stale affected partner scope: %v", err)
	}
	swap = false
	attach("other")
	if _, err := callBinding(t, ctx, bindings, "detach", "detach", params); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Visible(ctx, "owner", id); !matchesStatus(err, status_error.MCPConnectorNotFound) {
		t.Fatalf("current partner remains attached: %v", err)
	}
	if _, err := service.Visible(ctx, "other", id); err != nil {
		t.Fatalf("detaching the current partner modified the shared connector: %v", err)
	}
	attach("owner")
	if _, err := callBinding(t, ctx, bindings, "delete", "delete", params); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Visible(ctx, "other", id); !matchesStatus(err, status_error.MCPConnectorNotFound) {
		t.Fatalf("globally deleted connector still visible: %v", err)
	}
}

func callBinding(t *testing.T, ctx context.Context, bindings []capability.Binding, operation, requestID string, params map[string]any) (capability.Result, error) {
	t.Helper()
	id := "mcp.connectors." + operation
	for _, binding := range bindings {
		if binding.Operation == id {
			return binding.Call(ctx, capability.Request{Operation: id, RequestID: requestID, Params: params})
		}
	}
	t.Fatalf("missing binding %s", id)
	return capability.Result{}, nil
}

func testConnectorService(t *testing.T) *Service {
	t.Helper()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "mcp.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	wakers := controllers.NewWakerController(db.DB)
	for _, id := range []string{"owner", "other"} {
		if err := wakers.Create(t.Context(), models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	return New(Config{
		Connectors: controllers.NewMCPConnectorController(db.DB), Wakers: wakers,
		Categories:   controllers.NewResourceCategoryController(db.DB),
		Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)),
		Commands:     controllers.NewAgentCommandController(db.DB),
	})
}
