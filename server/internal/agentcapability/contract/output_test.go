package contract

import (
	"encoding/json"
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
	// 与真实工作流适配器一致：在持久化领域回执上补充保存完成标志。
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
	compiled, diagnostics, err := workflowdsl.Compile(`export const meta={sdk:'ally.workflow/2',title:'契约测试',description:'只计算',phases:[{id:'work',title:'计算',detail:'返回输入'}]};return args;`)
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
