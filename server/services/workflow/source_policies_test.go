package workflow

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestSaveWorkerSchemaWithoutExtraMeta(t *testing.T) {
	service, _ := workflowTestService(t)
	source := `export const meta={sdk:'ally.workflow/2',title:'分析',description:'报告',phases:[{id:'work',title:'分析',detail:'形成结论'}]};return await worker('分析',{id:'analysis',label:'分析',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'string'}});`
	definition := savedTestWorkflow(t, service, source)
	if definition.CurrentRevisionID == "" {
		t.Fatal("worker schema did not save")
	}
}

func TestSourcePoliciesRejectUnsupportedNativeBeforeSave(t *testing.T) {
	service := &Service{}
	compiled, diagnostics, err := workflowdsl.Compile(`export const meta={sdk:'ally.workflow/2',title:'复核',description:'复核',phases:[{id:'check',title:'检查',detail:'检查'}]};
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
