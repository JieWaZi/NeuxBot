package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestSaveRequiresResultPageDecisionForEveryWorker(t *testing.T) {
	service, store := workflowTestService(t)
	ctx := context.Background()
	definition, err := fixtureDefinition(ctx, store, definitionFixture{RequestID: "page-decision", Title: "分析"})
	if err != nil {
		t.Fatal(err)
	}
	source := `export const meta={sdk:'ally.workflow/1',title:'分析',description:'报告',phases:[{id:'work',title:'分析',detail:'形成结论',canExit:true}]};return await worker('分析',{id:'analysis',label:'分析',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'string'}});`
	_, diagnostics, err := service.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, RequestID: "missing", Source: source, SDKVersion: constants.WorkflowSDKVersion})
	if workflowstate.ErrorKey(err) != status_error.WorkflowSourceInvalid.Key() || len(diagnostics) != 1 || diagnostics[0].Code != "RESULT_PAGE_DECISION_REQUIRED" {
		t.Fatalf("unreviewed worker output saved: %v %+v", err, diagnostics)
	}
	unchanged, err := store.GetDefinition(ctx, definition.ID)
	if err != nil || unchanged.CurrentRevisionID != "" {
		t.Fatalf("rejected source changed revision: %+v %v", unchanged, err)
	}
	source = strings.Replace(source, "phases:", "nodeResultPageOmissions:{analysis:'仅作为下游合成的技术控制值，不交付可读内容'},phases:", 1)
	if _, diagnostics, err := service.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, RequestID: "reviewed", Source: source, SDKVersion: constants.WorkflowSDKVersion}); err != nil {
		t.Fatalf("explicit omission was rejected: %v %+v", err, diagnostics)
	}
}

func TestSourcePoliciesRejectUnsupportedNativeBeforeSave(t *testing.T) {
	service := &Service{}
	compiled, diagnostics, err := workflowdsl.Compile(`export const meta={sdk:'ally.workflow/1',title:'复核',description:'复核',phases:[{id:'check',title:'检查',detail:'检查'}]};
phase('check');
const result=await worker('复核',{id:'review',phase:'check',label:'复核',resolve:{kind:'waker',wakerId:'one'},toolPolicy:{connectors:{mode:'allowlist',connectorIds:[]},nativeTools:{mode:'allowlist',capabilityIds:[]}}});
return result;`)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	if err := service.validateSourcePolicies(context.Background(), compiled.Manifest); workflowstate.ErrorKey(err) != status_error.WorkflowCapabilityUnsupported.Key() {
		t.Fatalf("unsupported native restrictions were accepted: %v", err)
	}
	delete(compiled.Manifest.Nodes[0].Options["toolPolicy"].(map[string]any), "nativeTools")
	if err := service.validateSourcePolicies(context.Background(), compiled.Manifest); err != nil {
		t.Fatalf("supported empty MCP allowlist was rejected: %v", err)
	}
}

func TestWorkerConnectorAllowlistPreservesOnlyDeclaredPublicConnectors(t *testing.T) {
	options := map[string]any{"toolPolicy": map[string]any{"connectors": map[string]any{"mode": "allowlist", "connectorIds": []any{"business"}}}}
	allowed, err := workerConnectorAllowlist(options)
	if err != nil || allowed == nil || len(*allowed) != 1 || (*allowed)[0] != "business" {
		t.Fatalf("public connector scope: %v %v", allowed, err)
	}
	service := &Service{connectors: fixtureConnectors{connector: models.MCPConnector{ID: "business"}}}
	manifest := workflowdsl.Manifest{Nodes: []workflowdsl.Node{{Kind: "worker", Options: options}}}
	if err := service.validateSourcePolicies(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	options["toolPolicy"].(map[string]any)["connectors"].(map[string]any)["connectorIds"] = []any{"missing"}
	if err := service.validateSourcePolicies(context.Background(), manifest); workflowstate.ErrorKey(err) != status_error.WorkflowResourceInvalid.Key() {
		t.Fatal("undeclared business connector accepted", err)
	}
}
