package dsl

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func presentationFixture() PageDefinition {
	return PageDefinition{FormatVersion: 1, CatalogVersion: constants.WorkflowResultCatalogVersion, Title: "处理清单", Spec: map[string]any{"root": "list", "elements": map[string]any{
		"list": map[string]any{"type": "Collection", "props": map[string]any{"items": map[string]any{"$state": "/result/items"}, "pageSize": float64(10)}, "children": []any{"text"}},
		"text": map[string]any{"type": "Text", "props": map[string]any{"value": map[string]any{"$item": "title"}}},
	}}}
}
func TestPagePolicyRejectsActionsUnknownPropsAndBrokenTrees(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}}}}}
	if reason := validatePage(presentationFixture(), constants.WorkflowResultCatalogVersion, schema); reason != "" {
		t.Fatal(reason)
	}
	for _, mutation := range []func(PageDefinition){
		func(page PageDefinition) { page.Spec["state"] = map[string]any{} },
		func(page PageDefinition) {
			page.Spec["elements"].(map[string]any)["text"].(map[string]any)["on"] = map[string]any{"press": map[string]any{"action": "setState"}}
		},
		func(page PageDefinition) {
			page.Spec["elements"].(map[string]any)["text"].(map[string]any)["props"].(map[string]any)["value"] = map[string]any{"$bindState": "/result/items"}
		},
		func(page PageDefinition) {
			page.Spec["elements"].(map[string]any)["text"].(map[string]any)["props"].(map[string]any)["style"] = "red"
		},
		func(page PageDefinition) {
			page.Spec["elements"].(map[string]any)["list"].(map[string]any)["children"] = []any{"list"}
		},
		func(page PageDefinition) {
			page.Spec["elements"].(map[string]any)["list"].(map[string]any)["children"] = []any{"missing"}
		},
	} {
		page := presentationFixture()
		mutation(page)
		if validatePage(page, constants.WorkflowResultCatalogVersion, schema) == "" {
			t.Fatal("invalid page accepted", page)
		}
	}
}
func TestCompileFreezesNodePageAndRequiresDeclaredWorkerSchema(t *testing.T) {
	page := presentationFixture()
	data, _ := json.Marshal(page)
	source := `export const meta={sdk:'ally.workflow/1',title:'产出',description:'结果',nodeResultPages:{deliver:` + string(data) + `},phases:[{id:'work',title:'整理',detail:'任务',canExit:true}]}; const result = await worker('整理',{id:'deliver',label:'整理',phase:'work',resolve:{kind:'waker',wakerId:'00000000000000000000000000000001'},schema:{type:'object',properties:{items:{type:'array',items:{type:'object',properties:{title:{type:'string'}}}}}}}); return result;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	if compiled.Manifest.Meta.NodeResultPages["deliver"].CatalogDigest == "" {
		t.Fatal("catalog contract was not frozen")
	}
	if compiled.Manifest.Meta.NodeResultPages["deliver"].Title != page.Title {
		t.Fatal("page lost from frozen manifest")
	}
	changed := strings.Replace(source, "处理清单", "另一清单", 1)
	second, _, err := Compile(changed)
	if err != nil || compiled.ManifestDigest == second.ManifestDigest {
		t.Fatal("page mutation did not alter immutable manifest", err)
	}
	bad := strings.Replace(source, "schema:{type:'object',properties:{items:{type:'array',items:{type:'object',properties:{title:{type:'string'}}}}}}", "timeoutMs:1000", 1)
	if _, _, err := Compile(bad); err == nil {
		t.Fatal("page without worker schema accepted")
	}
}
func TestInputPageRequiresCompleteSchemaCoverage(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"first": map[string]any{"type": "string"}, "second": map[string]any{"type": "boolean"}}}
	page := PageDefinition{FormatVersion: 1, CatalogVersion: constants.WorkflowInputCatalogVersion, Spec: map[string]any{"root": "field", "elements": map[string]any{"field": map[string]any{"type": "Field", "props": map[string]any{"path": "/args/first"}}}}}
	if validatePage(page, constants.WorkflowInputCatalogVersion, schema) == "" {
		t.Fatal("missing field accepted")
	}
	page.Spec["elements"].(map[string]any)["field"].(map[string]any)["props"] = map[string]any{"path": "/args"}
	if reason := validatePage(page, constants.WorkflowInputCatalogVersion, schema); reason != "" {
		t.Fatal(reason)
	}
}

func TestActualCatalogAcceptsAllComponentsAndRejectsMalformedScopes(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}}}, "summary": map[string]any{"type": "string"}}}
	fixtures := map[string]map[string]any{
		"Page": {"width": "wide", "align": "center", "gap": "large"}, "Heading": {"value": "完整页面", "level": "h1"},
		"Card": {"tone": "accent"}, "Callout": {"title": "阅读范围", "value": map[string]any{"$state": "/result/summary"}, "tone": "info"}, "Disclosure": {"title": "详情", "defaultOpen": false},
		"Stack": {}, "Grid": {"columns": float64(2)}, "Section": {"title": "标题", "description": map[string]any{"$state": "/result/summary"}},
		"Text": {"value": map[string]any{"$state": "/result/summary"}}, "Markdown": {"value": "正文"},
		"KeyValue": {"label": "名称", "value": false}, "Metric": {"label": "数量", "value": float64(0), "unit": "项"},
		"Badge": {"value": "完成", "tone": "success"}, "Table": {"rows": map[string]any{"$state": "/result/items"}, "columns": []any{map[string]any{"key": "title", "label": "标题"}}},
		"Collection": {"items": map[string]any{"$state": "/result/items"}, "layout": "grid", "columns": float64(2)}, "Link": {"label": "来源", "href": "https://example.com", "variant": "inline"}, "FileList": {"files": nil},
	}
	for kind, props := range fixtures {
		t.Run(kind, func(t *testing.T) {
			page := PageDefinition{FormatVersion: 1, CatalogVersion: constants.WorkflowResultCatalogVersion, Spec: map[string]any{"root": "root", "elements": map[string]any{"root": map[string]any{"type": kind, "props": props}}}}
			if reason := validatePage(page, constants.WorkflowResultCatalogVersion, schema); reason != "" {
				t.Fatal(reason)
			}
		})
	}
	for _, mutate := range []func(PageDefinition){
		func(p PageDefinition) {
			p.Spec["elements"].(map[string]any)["text"].(map[string]any)["props"] = map[string]any{"value": map[string]any{"$item": "missing"}}
		},
		func(p PageDefinition) {
			p.Spec["elements"].(map[string]any)["list"].(map[string]any)["props"] = map[string]any{"items": map[string]any{"$state": "/result/summary"}}
		},
		func(p PageDefinition) {
			p.Spec["elements"].(map[string]any)["text"].(map[string]any)["visible"] = map[string]any{"$index": true, "eq": map[string]any{"$computed": "forbidden"}}
		},
		func(p PageDefinition) {
			p.Spec["elements"].(map[string]any)["list"].(map[string]any)["children"] = []any{"text", "text"}
		},
	} {
		page := presentationFixture()
		mutate(page)
		if validatePage(page, constants.WorkflowResultCatalogVersion, schema) == "" {
			t.Fatal("invalid scope accepted", page)
		}
	}
}

func TestInputLeafLayoutPreservesOptionalParentControls(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"detail": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "required": []any{"name"}}}}
	page := PageDefinition{FormatVersion: 1, CatalogVersion: constants.WorkflowInputCatalogVersion, Spec: map[string]any{"root": "field", "elements": map[string]any{"field": map[string]any{"type": "Field", "props": map[string]any{"path": "/args/detail/name"}}}}}
	if validatePage(page, constants.WorkflowInputCatalogVersion, schema) == "" {
		t.Fatal("optional parent control was bypassed")
	}
	schema["required"] = []any{"detail"}
	if reason := validatePage(page, constants.WorkflowInputCatalogVersion, schema); reason != "" {
		t.Fatal(reason)
	}
}

func TestAuthoringRequiresEveryWorkerPresentationDecision(t *testing.T) {
	page, _ := json.Marshal(presentationFixture())
	source := `export const meta={sdk:'ally.workflow/1',title:'分析',description:'分析',phases:[{id:'work',title:'分析',detail:'分析',canExit:true}]};
const result=await worker('分析',{id:'analysis',label:'分析',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'object',properties:{items:{type:'array',items:{type:'object',properties:{title:{type:'string'}}}}}}});
await worker('内部控制值',{id:'internal',label:'内部控制值',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});return result;`
	for _, tc := range []struct {
		// name 标识需要验证的展示决策组合。
		name string
		// declarations 是待插入 Meta 的页面或省略声明。
		declarations string
		// wantCode 是非法声明应当返回的具体诊断，空值表示允许保存。
		wantCode string
	}{
		{name: "no decisions", wantCode: "RESULT_PAGE_DECISION_REQUIRED"},
		{name: "one page is insufficient", declarations: `nodeResultPages:{analysis:` + string(page) + `},`, wantCode: "RESULT_PAGE_DECISION_REQUIRED"},
		{name: "page and reason", declarations: `nodeResultPages:{analysis:` + string(page) + `},nodeResultPageOmissions:{internal:'仅生成下游控制值，无可读产出'},`},
		{name: "empty reason", declarations: `nodeResultPageOmissions:{analysis:' ',internal:'内部控制值'},`, wantCode: "RESULT_PAGE_OMISSION_INVALID"},
		{name: "unknown worker", declarations: `nodeResultPageOmissions:{missing:'内部控制值'},`, wantCode: "RESULT_PAGE_OMISSION_INVALID"},
		{name: "phase is not worker", declarations: `nodeResultPageOmissions:{work:'内部控制值'},`, wantCode: "RESULT_PAGE_OMISSION_INVALID"},
		{name: "conflicting decisions", declarations: `nodeResultPages:{analysis:` + string(page) + `},nodeResultPageOmissions:{analysis:'内部控制值',internal:'内部控制值'},`, wantCode: "RESULT_PAGE_DECISION_CONFLICT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := strings.Replace(source, "phases:", tc.declarations+"phases:", 1)
			compiled, diagnostics, err := CompileForAuthoring(candidate)
			if tc.wantCode == "" {
				if err != nil || compiled.Manifest.Meta.NodeResultPageOmissions["internal"] == "" {
					t.Fatalf("reviewed outputs rejected: %v %+v", err, diagnostics)
				}
				return
			}
			for _, diagnostic := range diagnostics {
				if err != nil && diagnostic.Code == tc.wantCode {
					return
				}
			}
			t.Fatalf("missing diagnostic %s: %v %+v", tc.wantCode, err, diagnostics)
		})
	}
}

func TestWorkerTopicIncludesPresentationDecision(t *testing.T) {
	for _, topic := range []string{"worker"} {
		content := string(SDKTopicOutputs()["sdk-"+topic+".md"])
		if !strings.Contains(content, constants.WorkflowResultPageDecisionInstructions) {
			t.Fatalf("presentation requirement missing from %s", topic)
		}
	}
}

func TestFileDeliveryUsesFileSchemasWithoutAPageOrDuplicateReportFields(t *testing.T) {
	for _, tc := range []struct {
		// name 标识用户选择的文件交付结构。
		name string
		// schema 是真实交付文件的静态契约。
		schema string
		// declaration 声明展示方式及其冲突场景。
		declaration string
		// wantCode 是拒绝无效文件交付时的诊断。
		wantCode string
	}{
		{name: "single HTML path", schema: `{type:'string',format:'file'}`},
		{name: "several files", schema: `{type:'array',items:{type:'string',format:'file'}}`},
		{name: "named file roles", schema: `{type:'object',properties:{report:{type:'string',format:'file'}}}`},
		{name: "referenced file", schema: `{$ref:'#/$defs/report',$defs:{report:{type:'string',format:'file'}}}`},
		{name: "ordinary path is not a file", schema: `{type:'string'}`, wantCode: "RESULT_FILES_SCHEMA_REQUIRED"},
		{name: "unused file definition", schema: `{type:'string',$defs:{report:{type:'string',format:'file'}}}`, wantCode: "RESULT_FILES_SCHEMA_REQUIRED"},
		{name: "missing schema", schema: `{}`, wantCode: "RESULT_FILES_SCHEMA_REQUIRED"},
		{name: "empty title", schema: `{type:'string',format:'file'}`, declaration: `nodeResultFiles:{report:{title:' '}},`, wantCode: "RESULT_FILES_INVALID"},
		{name: "unknown worker", schema: `{type:'string',format:'file'}`, declaration: `nodeResultFiles:{missing:{title:'报告'}},`, wantCode: "RESULT_FILES_INVALID"},
		{name: "conflicts with omission", schema: `{type:'string',format:'file'}`, declaration: `nodeResultFiles:{report:{title:'报告'}},nodeResultPageOmissions:{report:'不展示'},`, wantCode: "RESULT_PAGE_DECISION_CONFLICT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			declaration := tc.declaration
			if declaration == "" {
				declaration = `nodeResultFiles:{report:{title:'HTML 报告'}},`
			}
			source := `export const meta={sdk:'ally.workflow/1',title:'文件交付',description:'生成报告文件',` + declaration + `phases:[{id:'work',title:'报告',detail:'实际写文件',canExit:true}]};return await worker('写出报告并提交真实路径',{id:'report',label:'生成报告',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:` + tc.schema + `});`
			compiled, diagnostics, err := CompileForAuthoring(source)
			if tc.wantCode == "" {
				if err != nil || len(compiled.Manifest.Meta.NodeResultPages) != 0 || compiled.Manifest.Meta.NodeResultFiles["report"].Title != "HTML 报告" {
					t.Fatalf("file delivery rejected or forced into a page: %v %+v", err, diagnostics)
				}
				return
			}
			for _, diagnostic := range diagnostics {
				if err != nil && diagnostic.Code == tc.wantCode {
					return
				}
			}
			t.Fatalf("missing diagnostic %s: %v %+v", tc.wantCode, err, diagnostics)
		})
	}
	page, _ := json.Marshal(presentationFixture())
	source := `export const meta={sdk:'ally.workflow/1',title:'冲突',description:'不能同时声明',nodeResultFiles:{report:{title:'报告'}},nodeResultPages:{report:` + string(page) + `},phases:[{id:'work',title:'报告',detail:'报告',canExit:true}]};return await worker('报告',{id:'report',label:'报告',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'object',properties:{file:{type:'string',format:'file'},items:{type:'array',items:{type:'object',properties:{title:{type:'string'}}}}}}});`
	_, diagnostics, err := CompileForAuthoring(source)
	for _, diagnostic := range diagnostics {
		if err != nil && diagnostic.Code == "RESULT_PAGE_DECISION_CONFLICT" {
			return
		}
	}
	t.Fatalf("page and file view conflict accepted: %v %+v", err, diagnostics)
}
