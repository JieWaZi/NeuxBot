package dsl

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const v2Meta = `export const meta={sdk:'ally.workflow/2',title:'V2',description:'契约',phases:[{id:'work',title:'执行',detail:'流程'}]`

func TestV2CanvasBusinessDescription(t *testing.T) {
	source := v2Meta + `};const value=await functionTask({id:'fn',label:'整理候选',description:'将查询结果整理为可供选择的方案',phase:'work',language:'javascript',code:'function main(){return []}'});return value;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	raw, _ := json.Marshal(ExportCanvas(compiled.Manifest))
	var canvas map[string]any
	_ = json.Unmarshal(raw, &canvas)
	for _, item := range canvas["nodes"].([]any) {
		node := item.(map[string]any)
		if node["id"] == "fn" {
			if node["description"] != "将查询结果整理为可供选择的方案" {
				t.Fatalf("missing readable description: %v", node)
			}
			return
		}
	}
	t.Fatal("node missing")
}

func TestV2StaticScriptsAndDynamicHumanCompile(t *testing.T) {
	source := v2Meta + `,actions:[{id:'list',type:'script',scriptId:'candidates',label:'候选',risk:'read'}]};
export const scripts={candidates:{language:'python',protocol:'json',code:String.raw` + "`" + `import json,sys
json.dump({"items":[{"value":"a","label":"A"}],"literal":"\\n"},sys.stdout)
` + "`" + `}};
const listed=await action('list',{id:'listed',label:'获取',phase:'work',input:false});
const choice=await askUser('选择',{id:'choice',label:'选择',phase:'work',kind:'select',choices:listed.data.items,allowCustom:false});
const result=await worker(choice.value,{id:'analyze',label:'分析',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'string'},successCondition:{path:'',equals:'ok'}});
return result;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("V2 sample rejected: %v %+v", err, diagnostics)
	}
	wire, _ := json.Marshal(compiled)
	var got map[string]any
	_ = json.Unmarshal(wire, &got)
	manifest := got["manifest"].(map[string]any)
	if manifest["formatVersion"] != float64(2) || manifest["sdkVersion"] != "ally.workflow/2" {
		t.Fatalf("missing V2 identity: %s", wire)
	}
	scripts := got["bundle"].(map[string]any)["scripts"].(map[string]any)
	code := scripts["candidates"].(map[string]any)["code"].(string)
	if !strings.Contains(code, `"literal":"\\n"`) || !strings.HasSuffix(code, "\n") {
		t.Fatalf("raw bytes changed: %q", code)
	}
}

func TestV2RejectsComputedStaticExportsAndInvalidHumanPolicies(t *testing.T) {
	for _, tail := range []string{
		`};export const scripts={s:{language:'python',protocol:'json',code:String.raw` + "`" + `${args.code}` + "`" + `}};return true;`,
		`};export const scripts={s:{language:'python',protocol:'json',code:getCode()}};return true;`,
		`};export const scripts={s:{language:'python',protocol:'yaml',code:'print(1)'}};return true;`,
		`};await askUser('x',{id:'ask',label:'问',phase:'work',kind:'form',schema:{type:'object'},choices:[]});return true;`,
		`};await askUser('x',{id:'ask',label:'问',phase:'work',kind:'confirm',timeoutMs:100,onTimeout:'default',defaultValue:true});return true;`,
		`};await askUser('x',{id:'ask',label:'问',phase:'work',kind:'select',choices:[{value:'a',label:'A'},{value:'a',label:'B'}]});return true;`,
		`};await functionTask({id:'fn',label:'检查',phase:'work',language:'javascript',code:'function main(){return true}',successCondition:{path:'bad',equals:true}});return true;`,
	} {
		if _, diagnostics, err := Compile(v2Meta + tail); err == nil || len(diagnostics) == 0 {
			t.Fatalf("invalid V2 source accepted: %s", tail)
		}
	}
}

func TestV2CanvasPreservesDynamicDependenciesAndControlBranches(t *testing.T) {
	source := v2Meta + `};
const listed=await worker('list',{id:'listed',label:'列举',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'object'}});
if(args.confirm){await askUser('选',{id:'choose',label:'选择',phase:'work',kind:'select',choices:listed.items});}else{await worker('other',{id:'other',label:'其他',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});}
return listed;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	var choose Node
	for _, node := range compiled.Manifest.Nodes {
		if node.ID == "choose" {
			choose = node
		}
	}
	found := false
	for _, input := range choose.Inputs {
		if input.Target == "choices" && input.Expression == "listed.items" && len(input.DependsOn) == 1 && input.DependsOn[0] == "listed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("dynamic choices lost source identity: %+v", choose.Inputs)
	}
	for _, edge := range compiled.Manifest.Edges {
		if edge.Kind == "control" && ((edge.From == "choose" && edge.To == "other") || (edge.From == "other" && edge.To == "choose")) {
			t.Fatalf("exclusive branches connected: %+v", edge)
		}
	}
	if len(compiled.Manifest.Controls) < 2 || compiled.Manifest.Controls[1].Kind != "decision" {
		t.Fatalf("missing nested topology: %+v", compiled.Manifest.Controls)
	}
}

func TestV2SuccessConditionDistinguishesMissingAndNull(t *testing.T) {
	data := map[string]any{"valid": false, "nullable": nil, "a/b": map[string]any{"~": []any{true}}}
	for _, test := range []struct {
		// condition 是针对真实业务返回的检查声明。
		condition any
		// want 是按 JSON Pointer 和字面值相等得到的结论。
		want bool
	}{
		{nil, true},
		{map[string]any{"path": "/valid", "equals": false}, true},
		{map[string]any{"path": "/valid", "equals": true}, false},
		{map[string]any{"path": "/nullable", "equals": nil}, true},
		{map[string]any{"path": "/missing", "equals": nil}, false},
		{map[string]any{"path": "/a~1b/~0/0", "equals": true}, true},
		{map[string]any{"path": "/a~1b/~0/00", "equals": true}, false},
	} {
		if got := EvaluateSuccessCondition(data, test.condition); got != test.want {
			t.Fatalf("condition=%+v got=%v want=%v", test.condition, got, test.want)
		}
	}
}

func TestV2SDKPublishesTypedProtocol(t *testing.T) {
	declarations := string(SDKOutputs()["workflow.d.ts"])
	for _, required := range []string{"sdk:'ally.workflow/2'", "status:'answered'|'skipped'|'timed_out'", "kind:'text'|'select'|'multiselect'|'form'|'confirm'", "choices?:", "successCondition?:", "protocol:'json'|'text'", "runStartedAt:number", "output:Json"} {
		if !strings.Contains(declarations, required) {
			t.Errorf("generated SDK missing %q", required)
		}
	}
	if strings.Contains(declarations, "Promise<T | null>") {
		t.Error("worker return still has implicit skipped null")
	}
}

func TestV2StaticExportBoundaryAndUnknownMetadata(t *testing.T) {
	for _, source := range []string{
		v2Meta + `,unknownField:true};return 1;`,
		strings.Replace(v2Meta, "title:'V2'", "title:`V2`", 1) + `};return 1;`,
		v2Meta + `};export const actions={};return 1;`,
		v2Meta + `};await askUser('x',{id:'x',label:'x',phase:'work',kind:'select'});return 1;`,
	} {
		if _, diagnostics, err := Compile(source); err == nil || len(diagnostics) == 0 {
			t.Fatalf("invalid static contract accepted: %s", source)
		}
	}
}

func TestV2CanvasPublicSchemaRejectsUnknownExecutionAndDanglingDependencies(t *testing.T) {
	compiled, diagnostics, err := Compile(v2Meta + `};return await worker('x',{id:'work-node',label:'执行',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});`)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	public := ExportCanvas(compiled.Manifest)
	if err := ValidateCanvas(public); err != nil {
		t.Fatalf("compiler produced invalid public canvas: %v", err)
	}
	wire, _ := json.Marshal(public)
	if strings.Contains(string(wire), `"meta":`) || strings.Contains(string(wire), `"options":`) || strings.Contains(string(wire), `"controlHints":`) {
		t.Fatalf("public canvas leaked internal facts: %s", wire)
	}
	public.Nodes[0].Execution["executable"] = "unapproved"
	if err := ValidateCanvas(public); err == nil {
		t.Fatal("worker execution accepted arbitrary command fields")
	}
	delete(public.Nodes[0].Execution, "executable")
	public.Nodes[0].Inputs = append(public.Nodes[0].Inputs, CanvasInput{Target: "prompt", Expression: "missing.value", DependsOn: []string{"missing"}})
	if err := ValidateCanvas(public); err == nil {
		t.Fatal("canvas accepted dangling input dependency")
	}
}

func TestV2CanvasJoinIsVisibleAndHasNoDanglingEdges(t *testing.T) {
	source := v2Meta + `};const handle=startWorkflow('child',{id:'start',label:'启动',phase:'work',mode:'joined'});const result=await joinWorkflow(handle);return result.output;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	public := ExportCanvas(compiled.Manifest)
	joins := 0
	for _, node := range public.Nodes {
		if node.Kind == "joinWorkflow" {
			joins++
			if node.Execution["handleExpression"] != "handle" {
				t.Fatalf("join lost handle: %+v", node)
			}
		}
	}
	if joins != 1 {
		t.Fatalf("join call is invisible: %+v", public.Nodes)
	}
	if err := ValidateCanvas(public); err != nil {
		t.Fatalf("canvas has dangling controls: %v", err)
	}
}

func TestV2CanvasDependenciesRespectLexicalShadowingAndCallbackLocals(t *testing.T) {
	source := v2Meta + `};
const result=await worker('outer',{id:'outer',label:'外层',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'object'}});
{const result=await worker('inner',{id:'inner',label:'内层',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'object'}});}
await askUser('选择',{id:'choice',label:'选择',phase:'work',kind:'select',choices:result.items});
await parallel([async()=>{const local=await worker('local',{id:'local',label:'任务',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'object'}});return await askUser('任务选择',{id:'local-choice',label:'任务选择',phase:'work',kind:'select',choices:local.items});}],{id:'parallel-group'});
return result;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	wanted := map[string]string{"choice": "outer", "local-choice": "local"}
	for _, node := range compiled.Manifest.Nodes {
		if want, ok := wanted[node.ID]; ok {
			for _, input := range node.Inputs {
				if input.Target == "choices" {
					if len(input.DependsOn) != 1 || input.DependsOn[0] != want {
						t.Errorf("%s choices dependencies=%v want %s", node.ID, input.DependsOn, want)
					}
					delete(wanted, node.ID)
				}
			}
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("missing input projections: %v", wanted)
	}
}

func TestV2CanvasDigestMatchesPublicProjection(t *testing.T) {
	compiled, diagnostics, err := Compile(v2Meta + `};return true;`)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	public := ExportCanvas(compiled.Manifest)
	bytes, _ := json.Marshal(public)
	if got := CanvasDigest(public); got != Digest(bytes) || got != CanvasDigest(ExportCanvas(compiled.Manifest)) {
		t.Fatalf("public digest drift: %s", got)
	}
	original := CanvasDigest(public)
	public.Title = "变更"
	if CanvasDigest(public) == original {
		t.Fatal("public field change did not change digest")
	}
}

func TestV2CanvasPipelineUsesSequentialStageEdgesAndParameterDependencies(t *testing.T) {
	source := v2Meta + `};const items=await worker('items',{id:'items',label:'输入',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'array'}});return await pipeline(items,[async(item)=>await worker(item,{id:'first',label:'第一步',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'object'}}),async(previous)=>await worker(previous,{id:'second',label:'第二步',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}})],{id:'stages'});`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	sequential := false
	for _, edge := range compiled.Manifest.Edges {
		if edge.Kind == "control" && edge.From == "first" && edge.To == "second" {
			sequential = true
		}
	}
	if !sequential {
		t.Error("pipeline stages projected as independent branches")
	}
	wanted := map[string]string{"first": "items", "second": "first"}
	for _, node := range compiled.Manifest.Nodes {
		if want, ok := wanted[node.ID]; ok {
			for _, input := range node.Inputs {
				if input.Target == "prompt" {
					found := false
					for _, dependency := range input.DependsOn {
						found = found || dependency == want
					}
					if !found {
						t.Errorf("stage %s lost parameter dependency %s: %v", node.ID, want, input.DependsOn)
					}
				}
			}
		}
	}
}

func TestV2CompleteDesignExampleCompilesWithoutPresentationDeclarations(t *testing.T) {
	source, err := os.ReadFile("testdata/v2-candidates.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	compiled, diagnostics, err := Compile(string(source))
	if err != nil {
		t.Fatalf("complete design example rejected: %v %+v", err, diagnostics)
	}
	if len(compiled.Bundle.Scripts) != 2 || len(compiled.Manifest.Scripts) != 2 {
		t.Fatal("design example lost frozen Python programs")
	}
	if err := ValidateCanvas(ExportCanvas(compiled.Manifest)); err != nil {
		t.Fatal(err)
	}
}

func TestV2DesignFlowReplaysTypedHumanAndDirectWorkerValuesAcrossCheckpoints(t *testing.T) {
	source, err := os.ReadFile("testdata/v2-candidates.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	state := runTestMachine(t, string(source), map[string]any{}, func(boundary EffectBoundary) Value {
		calls = append(calls, boundary.Primitive+":"+boundary.NodeID)
		switch boundary.Primitive {
		case "phase":
			return JSONValue(nil)
		case "askUser":
			options := boundary.Arguments[1].Data.(map[string]any)
			value := "demo"
			if boundary.NodeID == "choose" {
				value = "a"
				choices, ok := options["choices"].([]any)
				if !ok || len(choices) != 1 || choices[0].(map[string]any)["value"] != "a" {
					t.Fatalf("dynamic choices not evaluated after replay: %+v", options)
				}
			}
			return JSONValue(map[string]any{"questionId": boundary.NodeID, "status": "answered", "source": "user", "value": value})
		case "worker":
			if !strings.Contains(boundary.Arguments[0].Data.(string), "候选 a") {
				t.Fatalf("human value not used by direct worker: %+v", boundary)
			}
			return JSONValue(map[string]any{"analysisText": "分析 a"})
		case "action":
			if boundary.NodeID == "list-options" {
				return JSONValue(map[string]any{"data": map[string]any{"items": []any{map[string]any{"value": "a", "label": "候选甲"}}}, "durationMs": 0})
			}
			return JSONValue(map[string]any{"data": map[string]any{"valid": true}, "durationMs": 0})
		default:
			t.Fatalf("unexpected effect: %+v", boundary)
			return JSONValue(nil)
		}
	})
	if state.Failure != nil {
		t.Fatalf("V2 replay failed: %+v", state.Failure)
	}
	result, _ := state.Result.JSON()
	wire, _ := json.Marshal(result)
	if string(wire) != `{"analysisText":"分析 a","valid":true}` || len(calls) != 6 {
		t.Fatalf("wrong replay result %s calls=%v", wire, calls)
	}
}

func TestV2CanvasTracksPureCallbackCapturedDependencies(t *testing.T) {
	source := v2Meta + `};const listed=await worker('list',{id:'listed',label:'列表',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'array'}});const query=await askUser('过滤',{id:'query',label:'过滤',phase:'work',kind:'text'});const filtered=listed.filter(item=>item.label.includes(query.value));return await askUser('选择',{id:'choose',label:'选择',phase:'work',kind:'select',choices:filtered});`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	for _, node := range compiled.Manifest.Nodes {
		if node.ID == "choose" {
			for _, input := range node.Inputs {
				if input.Target == "choices" {
					wire, _ := json.Marshal(input.DependsOn)
					if string(wire) != `["listed","query"]` {
						t.Fatalf("filter callback lost a captured input: %s", wire)
					}
				}
			}
		}
	}
}

func TestV2CanvasBusinessDescriptionFallsBackOnlyToActionDeclaration(t *testing.T) {
	source := v2Meta + `,actions:[{id:'fetch',type:'http',label:'获取资料',description:'读取候选方案供用户筛选',risk:'read',allowedOrigins:['https://example.com'],methods:['GET']}]};await action('fetch',{id:'fetch-node',label:'获取资料',phase:'work'});await action('fetch',{id:'override',label:'获取资料',description:'读取本次项目的候选方案',phase:'work'});return await worker('分析',{id:'analyze',label:'分析资料',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	raw, _ := json.Marshal(ExportCanvas(compiled.Manifest))
	var result map[string]any
	_ = json.Unmarshal(raw, &result)
	expected := map[string]any{"fetch-node": "读取候选方案供用户筛选", "override": "读取本次项目的候选方案", "analyze": nil}
	for _, item := range result["nodes"].([]any) {
		node := item.(map[string]any)
		if want, ok := expected[node["id"].(string)]; ok && node["description"] != want {
			t.Errorf("description for %s = %v want %v", node["id"], node["description"], want)
		}
	}
}

func TestV2NodeDescriptionIsStaticBoundedAndEditable(t *testing.T) {
	base := v2Meta + `};return await worker('分析',{id:'analyze',label:'分析',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});`
	for _, value := range []string{`args.description`, `123`, `'` + strings.Repeat("文", 501) + `'`} {
		source := strings.Replace(base, "label:'分析'", "label:'分析',description:"+value, 1)
		if _, diagnostics, err := Compile(source); err == nil || len(diagnostics) == 0 {
			t.Errorf("invalid description accepted: %s", value)
		}
	}
	edited, err := EditSource(base, SourceEdit{Target: "node", ID: "analyze", Fields: map[string]any{"description": strings.Repeat("文", 500)}})
	if err != nil {
		t.Fatal(err)
	}
	compiled, diagnostics, err := Compile(edited)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	raw, _ := json.Marshal(ExportCanvas(compiled.Manifest))
	if !strings.Contains(string(raw), strings.Repeat("文", 500)) {
		t.Fatal("edited business description was lost")
	}
}

func TestV2CanvasFinallyFollowsEitherTryPath(t *testing.T) {
	source := v2Meta + `};try{await worker('body',{id:'body',label:'主体',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});}catch(error){await worker('recover',{id:'recover',label:'恢复',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});}finally{await worker('cleanup',{id:'cleanup',label:'收尾',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});}return true;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	expected := map[string]bool{"body": false, "recover": false}
	for _, edge := range compiled.Manifest.Edges {
		if edge.Kind == "control" && edge.To == "cleanup" {
			if _, ok := expected[edge.From]; ok {
				expected[edge.From] = true
			}
			if edge.From == "__workflow_input__" {
				t.Error("finally projected as independent branch")
			}
		}
	}
	for from, found := range expected {
		if !found {
			t.Errorf("finally is not shown after %s", from)
		}
	}
}

func TestV2CanvasReturnWaitsForFinally(t *testing.T) {
	source := v2Meta + `};try{await worker('body',{id:'body',label:'主体',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});return true;}finally{await worker('cleanup',{id:'cleanup',label:'收尾',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});}`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	cleanupThenResult, bodyThenCleanup := false, false
	for _, edge := range compiled.Manifest.Edges {
		if edge.Kind != "control" {
			continue
		}
		bodyThenCleanup = bodyThenCleanup || edge.From == "body" && edge.To == "cleanup"
		cleanupThenResult = cleanupThenResult || edge.From == "cleanup" && edge.To == "__workflow_result__"
		if edge.From == "body" && edge.To == "__workflow_result__" {
			t.Error("return bypasses finally")
		}
	}
	if !cleanupThenResult || !bodyThenCleanup {
		t.Fatalf("finally return sequence missing: %+v", compiled.Manifest.Edges)
	}
}

func TestV2RejectsRemovedPresentationDesign(t *testing.T) {
	for _, field := range []string{"nodeResultPages:{}", "nodeResultFiles:{}", "nodeResultPageOmissions:{}", "inputPage:null"} {
		source := v2Meta + "," + field + `};return true;`
		if _, diagnostics, err := Compile(source); err == nil || len(diagnostics) == 0 {
			t.Errorf("removed display design accepted: %s", field)
		}
	}
	outputs := SDKOutputs()
	if _, present := outputs["sdk-presentation.md"]; present {
		t.Error("removed presentation topic is still generated")
	}
	for _, name := range []string{"workflow.d.ts", "creation-knowledge.md", "canvas-manifest.schema.json"} {
		for _, forbidden := range []string{"nodeResultPages", "nodeResultFiles", "nodeResultPageOmissions", "inputPage", "PageDefinition", "PresentationPage"} {
			if strings.Contains(string(outputs[name]), forbidden) {
				t.Errorf("%s still advertises %s", name, forbidden)
			}
		}
	}
}
