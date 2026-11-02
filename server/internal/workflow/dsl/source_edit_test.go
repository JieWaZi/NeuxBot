package dsl

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

func TestEditSourceChangesOneWorkerAndPreservesExecutableExpressions(t *testing.T) {
	source := `// 用户保留的源码注释
export const meta={sdk:'ally.workflow',title:'检查',description:'检查',phases:[{id:'check',title:'检查',detail:'原说明',canExit:true}]};
phase('check');
const result=await worker(` + "`检查 ${args.material}`" + `,{id:'review',phase:'check',label:'原节点',resolve:{kind:'waker',wakerId:'one'},schema:{type:'object'}});
return result; // 不触及结果逻辑`
	prompt := "复核 ${args.material}"
	edited, err := EditSource(source, SourceEdit{Target: "node", ID: "review", Fields: map[string]any{"label": "用户节点", "resolve": map[string]any{"kind": "waker", "wakerId": "two", "runtimeId": "codex", "model": "model", "reasoning": "high"}}, Prompt: &prompt})
	if err != nil {
		t.Fatal(err)
	}
	compiled, diagnostics, err := Compile(edited)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	node := compiled.Manifest.Nodes[0]
	if node.LabelTemplate != "用户节点" || node.Options["resolve"].(map[string]any)["wakerId"] != "two" || node.PromptTemplate != "`复核 ${args.material}`" {
		t.Fatalf("edit lost fields: %+v", node)
	}
	if !strings.HasPrefix(edited, "// 用户保留的源码注释\nexport const meta={sdk:'ally.workflow',title:'检查',description:'检查'") || !strings.HasSuffix(edited, "return result; // 不触及结果逻辑") || !strings.Contains(edited, "schema:{type:'object'}") {
		t.Fatal("editing reformatted unrelated source")
	}
}

func TestEditConcatenatedPromptRetainsInputAndLiteralEscapes(t *testing.T) {
	source := `export const meta={sdk:'ally.workflow',title:'复核',description:'复核',phases:[{id:'check',title:'检查',detail:'检查',canExit:true}]};
phase('check');
const result=await worker("材料：" + args.material,{id:'review',phase:'check',label:'复核',resolve:{kind:'waker',wakerId:'one'},schema:{type:'object'}});
return result;`
	prompt := "手改 ${args.material}，`代码`，\\`已有转义\\`，\\${literal}"
	edited, err := EditSource(source, SourceEdit{Target: "node", ID: "review", Prompt: &prompt})
	if err != nil {
		t.Fatal(err)
	}
	compiled, diagnostics, err := Compile(edited)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	value, err := goja.New().RunString("const args={material:'虚构材料'}; " + compiled.Manifest.Nodes[0].PromptTemplate)
	if err != nil || value.String() != "手改 虚构材料，`代码`，`已有转义`，${literal}" {
		t.Fatalf("manual prompt lost executable input or literal text: %v %v", value, err)
	}
}

func TestEditSourceMetadataAndPhaseUseStableIdentity(t *testing.T) {
	edited, err := EditSource(simpleSource, SourceEdit{Target: "phase", ID: "compose", Fields: map[string]any{"title": "用户组合", "detail": "手工修改阶段"}})
	if err != nil {
		t.Fatal(err)
	}
	edited, err = EditSource(edited, SourceEdit{Target: "meta", Fields: map[string]any{"title": "用户标题"}})
	if err != nil {
		t.Fatal(err)
	}
	compiled, diagnostics, err := Compile(edited)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	if compiled.Manifest.Meta.Title != "用户标题" || compiled.Manifest.Meta.Phases[1].Title != "用户组合" {
		t.Fatal("metadata edit was not compiled")
	}
	if !strings.Contains(edited, "phase('compose');") || !strings.Contains(edited, "const message = words.join(' ') + '!';") {
		t.Fatal("stage identity or business logic changed")
	}
}

func TestEditSourceRejectsUnknownTargetAndIdentityChanges(t *testing.T) {
	for _, edit := range []SourceEdit{{Target: "phase", ID: "missing", Fields: map[string]any{"title": "bad"}}, {Target: "phase", ID: "compose", Fields: map[string]any{"id": "new"}}, {Target: "meta", Fields: map[string]any{"sdk": "other"}}} {
		if _, err := EditSource(simpleSource, edit); err == nil {
			t.Fatalf("accepted invalid edit %+v", edit)
		}
	}
}

func TestSaveReceiptHasNoRetiredFields(t *testing.T) {
	payload, err := json.Marshal(SaveReceipt{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), `"unchanged"`) {
		t.Fatalf("retired save field is still published: %s", payload)
	}
}
