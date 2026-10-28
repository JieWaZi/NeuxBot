package dsl

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const simpleSource = `export const meta = {
 sdk: 'ally.workflow/2', title: '纯计算', description: '计算字符串',
 inputSchema: {type:'object', required:['text'], properties:{text:{type:'string'},repeat:{type:'integer',minimum:1,maximum:5,default:2}}},
 outputSchema: {type:'object',required:['message','length','repeat'],properties:{message:{type:'string'},length:{type:'integer'},repeat:{type:'integer'}}},
 phases: [{id:'prepare',title:'准备',detail:'整理输入',next:['compose']},{id:'compose',title:'组合',detail:'组合并返回',canExit:true}]
};
phase('prepare');
let words = [];
for (let index = 0; index < args.repeat; index++) { words.push(args.text.toUpperCase()); }
phase('compose');
const message = words.join(' ') + '!';
return {message, length:message.length, repeat:args.repeat};`

func TestCompileFunctionAcceptsTenMinuteTimeout(t *testing.T) {
	for _, timeout := range []string{"600000", "600001"} {
		t.Run(timeout, func(t *testing.T) {
			source := `export const meta={sdk:'ally.workflow/2',title:'脚本期限',description:'十分钟',phases:[{id:'work',title:'执行',detail:'计算',canExit:true}]};return await functionTask({id:'script',label:'计算',phase:'work',language:'javascript',code:'function main(input){return input;}',input:1,timeoutMs:` + timeout + `});`
			_, diagnostics, err := Compile(source)
			if timeout == "600000" {
				if err != nil {
					t.Fatalf("ten-minute script deadline rejected: %v %+v", err, diagnostics)
				}
				return
			}
			if err == nil || len(diagnostics) == 0 || diagnostics[0].Code != "TIMEOUT_INVALID" {
				t.Fatalf("script exceeded host deadline: %v %+v", err, diagnostics)
			}
		})
	}
}

func TestCompileWorkerDoesNotExposeNodeTimeout(t *testing.T) {
	source := `export const meta={sdk:'ally.workflow/2',title:'伙伴期限',description:'无独立期限',phases:[{id:'work',title:'执行',detail:'伙伴任务',canExit:true}]};return await worker('work',{id:'work',label:'执行',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},timeoutMs:600000});`
	_, diagnostics, err := Compile(source)
	if err == nil || len(diagnostics) == 0 || diagnostics[0].Code != "OPTION_UNKNOWN" {
		t.Fatalf("worker timeoutMs was accepted: %v %+v", err, diagnostics)
	}
}

func TestCompileChecksMinimumInstanceBudgetWithoutCountingAlternativeBranches(t *testing.T) {
	meta := `export const meta={sdk:'ally.workflow/2',title:'实例预算',description:'保存前校验',limits:{concurrency:1,maxInstances:1},phases:[{id:'work',title:'工作',detail:'任务',canExit:true}]};`
	first := `await askUser('第一步',{id:'first',label:'第一步',phase:'work'});`
	second := `await askUser('第二步',{id:'second',label:'第二步',phase:'work'});`
	for _, test := range []struct {
		// name 区分顺序执行与动态控制流的预算场景。
		name string
		// body 是真实编译的主流程，保持同一实例预算。
		body string
		// invalid 表示所有正常结束路径都必定超过已声明预算。
		invalid bool
	}{
		{"sequential", first + second + `return true;`, true},
		{"branches-then-required", `if(args.choice){` + first + `}else{` + second + `}` + `await askUser('汇总',{id:'final',label:'汇总',phase:'work'});return true;`, true},
		{"exclusive-branches", `if(args.choice){` + first + `}else{` + second + `}return true;`, false},
		{"optional-loop", `for(let i=0;i<args.count;i++){` + first + `}` + second + `return true;`, false},
		{"early-return", `if(args.skip){return false;}` + first + second + `return true;`, false},
		{"phase-is-not-instance", `phase('work');` + first + `return true;`, false},
		{"exception-return", `try{` + first + second + `}catch(error){return false;}return true;`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics, err := Compile(meta + test.body)
			if !test.invalid {
				if err != nil {
					t.Fatalf("valid budget rejected: %v %+v", err, diagnostics)
				}
				return
			}
			if err == nil || len(diagnostics) == 0 || diagnostics[0].Code != "INSTANCE_BUDGET_TOO_LOW" || diagnostics[0].Range.Start.Line < 1 {
				t.Fatalf("impossible budget saved without actionable source diagnostic: %v %+v", err, diagnostics)
			}
		})
	}
}

func TestCompileBindsFullNormalizedWeeklySource(t *testing.T) {
	source, err := os.ReadFile("testdata/weekly-report.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	compiled, diagnostics, err := Compile(string(source))
	if err != nil {
		t.Fatalf("%v: %+v", err, diagnostics)
	}
	if len(compiled.Manifest.Meta.Phases) != 4 || len(compiled.Manifest.Nodes) != 11 || len(compiled.Manifest.Groups) != 1 {
		t.Fatalf("lost reference capabilities: %+v", compiled.Manifest)
	}
	workers, humans := 0, 0
	for _, node := range compiled.Manifest.Nodes {
		if node.Kind == "input" || node.Kind == "result" {
			continue
		}
		if node.Kind == "worker" {
			workers++
		}
		if node.Kind == "askUser" {
			humans++
		}
		if node.ID == "" || node.Range.Start.Line < 1 {
			t.Fatal("node has no stable identity or source evidence")
		}
	}
	if workers != 7 || humans != 2 {
		t.Fatalf("workers=%d humans=%d", workers, humans)
	}
	data, err := json.Marshal(compiled.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	var restored ProgramBundle
	if json.Unmarshal(data, &restored) != nil || len(restored.Programs) < 3 {
		t.Fatal("execution program is not independently serializable")
	}
	if compiled.SourceDigest != Digest(source) {
		t.Fatal("digest was not bound to exact original bytes")
	}
}

func TestCompileDeterministicSourceAndPrograms(t *testing.T) {
	first, diagnostics, err := Compile(simpleSource)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	second, _, err := Compile(simpleSource)
	if err != nil || first.ManifestDigest != second.ManifestDigest || first.IRDigest != second.IRDigest {
		t.Fatal("same source produced different programs")
	}
}

func TestCompileRejectsInvalidJavaScriptFunctionBeforeSave(t *testing.T) {
	for name, code := range map[string]string{
		"top-level return": "return { ok: true };",
		"missing main":     "const output = { ok: true };",
		"invalid syntax":   "function main(input) { return {; }",
		"async main":       "async function main(input) { return input; }",
		"generator main":   "function* main(input) { yield input; }",
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(code)
			if err != nil {
				t.Fatal(err)
			}
			source := `export const meta = {sdk:'ally.workflow/2',title:'Function 校验',description:'保存前拒绝无效入口',phases:[{id:'compute',title:'计算',detail:'真实入口',canExit:true}]}; phase('compute'); const value = await functionTask({id:'compute_value',label:'计算',phase:'compute',language:'javascript',code:` + string(encoded) + `,input:args}); return value;`
			_, diagnostics, err := Compile(source)
			if err == nil || len(diagnostics) == 0 {
				t.Fatalf("invalid Function was saved without diagnostics: %v %+v", err, diagnostics)
			}
			if !strings.HasPrefix(diagnostics[0].Code, "FUNCTION_") || diagnostics[0].Range.Start.Line < 1 {
				t.Fatalf("Function source diagnostic lost: %+v", diagnostics)
			}
		})
	}
}

func TestCompileFullEnvironmentReleaseContracts(t *testing.T) {
	source, err := os.ReadFile("testdata/environment-release.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	compiled, diagnostics, err := Compile(string(source))
	if err != nil {
		t.Fatalf("complete F2 source: %v %+v", err, diagnostics)
	}
	if len(compiled.Manifest.Meta.Phases) != 5 || len(compiled.Manifest.Groups) != 3 || len(compiled.Manifest.Nodes) != 16 {
		t.Fatalf("full F2 templates lost: phases=%d groups=%d nodes=%d", len(compiled.Manifest.Meta.Phases), len(compiled.Manifest.Groups), len(compiled.Manifest.Nodes))
	}
}

func TestCompileRejectsInvalidContractsBeforeSaving(t *testing.T) {
	cases := map[string]string{
		"sdk":           strings.Replace(simpleSource, "ally.workflow/2", "unknown/1", 1),
		"literal":       strings.Replace(simpleSource, "title: '纯计算'", "title: args.title", 1),
		"duplicate":     strings.Replace(simpleSource, "id:'compose'", "id:'prepare'", 1),
		"phase":         strings.Replace(simpleSource, "phase('prepare')", "phase('missing')", 1),
		"host":          simpleSource + "\nrequire('fs');",
		"class":         simpleSource + "\nclass Hidden {}",
		"unbound":       simpleSource + "\nmissingFunction();",
		"capture write": simpleSource + "\nlet shared=[]; await parallel([async()=>{shared.push('unsafe');return 'x';}],{id:'tasks'});",
		"recursion":     simpleSource + "\nfunction recurse(){return recurse();}",
		"remote schema": strings.Replace(simpleSource, "type:'object', required:", "$ref:'https://example.com/schema', type:'object', required:", 1),
		"hidden host":   simpleSource + "\nasync function hidden(){ return await worker('x',{id:'hidden',label:'hidden',phase:'prepare',resolve:{kind:'waker',wakerId:'fixture'}}); }",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			compiled, diagnostics, err := Compile(source)
			if err == nil || len(diagnostics) == 0 || compiled.SourceDigest != "" {
				t.Fatalf("invalid source admitted: %v %+v", err, diagnostics)
			}
		})
	}
}

func TestDefaultsPreserveExplicitNullAndDoNotGuessBranches(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"missing": map[string]any{"default": "filled"}, "nullable": map[string]any{"default": "must not replace null"}}, "oneOf": []any{map[string]any{"properties": map[string]any{"branch": map[string]any{"default": "must not guess"}}}, map[string]any{"properties": map[string]any{"anotherBranch": map[string]any{"default": "also must not guess"}}}}}
	result := ApplyDefaults(schema, map[string]any{"nullable": nil}).(map[string]any)
	if result["missing"] != "filled" || result["nullable"] != nil || result["branch"] != nil {
		t.Fatalf("unexpected defaults: %+v", result)
	}
}
