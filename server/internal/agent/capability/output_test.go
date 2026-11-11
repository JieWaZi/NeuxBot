package capability

import (
	"encoding/json"
	"strings"
	"testing"

	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
)

func TestWorkflowV2SaveReceiptMatchesClosedOutputContract(t *testing.T) {
	receipt := workflowdsl.SaveReceipt{DefinitionID: "definition", RevisionID: "revision", Ordinal: 2, SourceDigest: "source-digest", ManifestDigest: "canvas-digest"}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}
	// Match the real workflow adapter by retaining the completion marker in the durable domain receipt.
	data["saved"] = true
	op, _ := Find("workflow.source.save")
	if err := workflowdsl.ValidateJSON(op.OutputSchema, data); err != nil {
		t.Fatalf("saved revision cannot be returned to MCP: %v", err)
	}
	data["unexpectedField"] = true
	if err := workflowdsl.ValidateJSON(op.OutputSchema, data); err == nil {
		t.Fatal("closed output contract accepts undeclared fields")
	}
}

func TestWorkflowV2ValidationCanReturnPublicManifest(t *testing.T) {
	compiled, diagnostics, err := workflowdsl.Compile(`export const meta={sdk:'ally.workflow',title:'契约测试',description:'只计算',phases:[{id:'work',title:'计算',detail:'返回输入'}]};return args;`)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	data := map[string]any{"valid": true, "diagnostics": diagnostics, "summary": map[string]any{"title": compiled.Manifest.Meta.Title, "nodes": len(compiled.Manifest.Nodes), "phases": len(compiled.Manifest.Meta.Phases), "sourceDigest": compiled.SourceDigest}, "manifest": workflowdsl.ExportCanvas(compiled.Manifest)}
	encoded, _ := json.Marshal(data)
	var normalized any
	_ = json.Unmarshal(encoded, &normalized)
	op, _ := Find("workflow.source.validate")
	if err := workflowdsl.ValidateJSON(op.OutputSchema, normalized); err != nil {
		t.Fatalf("includeManifest output violates operation contract: %v", err)
	}
}

// TestAuthoringOutputRequiresWorkspace ensures neither authoring entry point omits the fixed directory contract.
func TestAuthoringOutputRequiresWorkspace(t *testing.T) {
	workspace := map[string]any{"rootDirectory": "/managed/workflow", "scriptsDirectory": "/managed/workflow/scripts", "stateDirectory": "/managed/workflow/state", "cacheDirectory": "/managed/workflow/cache", "temporaryDirectory": "/managed/workflow/tmp", "runsDirectory": "/managed/workflow/runs"}
	for _, id := range []string{"workflow.source.get", "workflow.authoring.prepare"} {
		t.Run(id, func(t *testing.T) {
			op, _ := Find(id)
			wrap := func(source map[string]any) map[string]any {
				if id == "workflow.authoring.prepare" {
					return map[string]any{"workflow": source}
				}
				return source
			}
			if err := workflowdsl.ValidateJSON(op.OutputSchema, wrap(map[string]any{"workspace": workspace})); err != nil {
				t.Fatalf("complete workspace rejected: %v", err)
			}
			for name, data := range map[string]map[string]any{
				"missing":              {},
				"missing-workspace":    wrap(map[string]any{"workflowId": "workflow"}),
				"incomplete-workspace": wrap(map[string]any{"workspace": map[string]any{"rootDirectory": "/managed/workflow"}}),
			} {
				if err := workflowdsl.ValidateJSON(op.OutputSchema, data); err == nil {
					t.Errorf("%s accepted without a complete workspace", name)
				}
			}
		})
	}
}

func TestMCPRequestIdentityMatchesCoreConstraints(t *testing.T) {
	op, _ := Find("workflow.source.save")
	transport := Operation{InputSchema: op.MCPInputSchema()}
	for _, test := range []struct {
		// id is an idempotency identifier constructed by the model according to the public tool contract.
		id string
		// valid lists the inputs accepted by Core's fixed protocol.
		valid bool
	}{
		{id: "a", valid: true}, {id: "save.request:1-2_3", valid: true},
		{id: strings.Repeat("a", 128), valid: true},
		{id: strings.Repeat("a", 129)}, {id: strings.Repeat("a", 200)},
		{id: "save request 1"}, {id: "保存请求"}, {id: "_save"}, {id: ""},
	} {
		err := transport.Validate(map[string]any{"source": "return true;", "expectedRevisionId": "head", "requestId": test.id})
		if (err == nil) != test.valid {
			t.Errorf("MCP requestId %q: accepted=%v, want %v", test.id, err == nil, test.valid)
		}
	}
}
