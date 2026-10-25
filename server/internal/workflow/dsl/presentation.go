package dsl

import (
	"embed"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/dop251/goja/ast"
)

// PageDefinition 冻结创作时确定的组件目录和原生 json-render Spec。
type PageDefinition struct {
	// FormatVersion 是宿主支持的页面文档版本。
	FormatVersion int `json:"formatVersion"`
	// CatalogVersion 是只读产出或输入组件的冻结目录版本。
	CatalogVersion string `json:"catalogVersion"`
	// CatalogDigest 冻结编译时的组件属性契约，防止同版本目录漂移。
	CatalogDigest string `json:"catalogDigest,omitempty"`
	// Title 是页面标题。
	Title string `json:"title,omitempty"`
	// Description 是面向用户的页面说明。
	Description string `json:"description,omitempty"`
	// Spec 是原生 root/elements 文档，禁止执行脚本或声明外部操作。
	Spec map[string]any `json:"spec"`
}

// FileResultDefinition 声明文件产出入口，文件清单由节点 Schema 和成功结果派生。
type FileResultDefinition struct {
	// Title 是产出卡片和文件查看标签的名称。
	Title string `json:"title"`
	// Description 是文件交付的简短说明，不复制文件正文。
	Description string `json:"description,omitempty"`
}

//go:embed presentation-catalog.generated.json
var presentationCatalogFiles embed.FS

// presentationContracts 是由前端目录生成且供编译器消费的冻结契约。
type presentationContracts struct {
	// Catalogs 保存各版本的组件属性 Schema。
	Catalogs map[string]map[string]map[string]any `json:"catalogs"`
	// Digests 保存各目录属性契约的确定摘要。
	Digests map[string]string `json:"digests"`
}

func presentationContract() presentationContracts {
	data, _ := presentationCatalogFiles.ReadFile("presentation-catalog.generated.json")
	var contract presentationContracts
	_ = json.Unmarshal(data, &contract)
	return contract
}

// CompileForAuthoring 在完整编译后检查每个伙伴输出的展示决策，供创作校验和保存共用。
func CompileForAuthoring(source string) (Compilation, []Diagnostic, error) {
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		return compiled, diagnostics, err
	}
	for _, node := range compiled.Manifest.Nodes {
		if node.Kind != "worker" {
			continue
		}
		_, page := compiled.Manifest.Meta.NodeResultPages[node.ID]
		_, files := compiled.Manifest.Meta.NodeResultFiles[node.ID]
		_, omitted := compiled.Manifest.Meta.NodeResultPageOmissions[node.ID]
		if !page && !files && !omitted {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "RESULT_PAGE_DECISION_REQUIRED", Severity: "error", Range: node.Range,
				Message: "declare meta.nodeResultPages, meta.nodeResultFiles or a concrete meta.nodeResultPageOmissions reason for worker: " + node.ID,
				NodeID:  node.ID, Field: "meta",
				Hint: "query SDK presentation, then choose a result page, file delivery or a concrete omission reason",
			})
		}
	}
	if len(diagnostics) > 0 {
		return Compilation{}, diagnostics, status_error.WorkflowSourceInvalid
	}
	return compiled, diagnostics, nil
}

func (c *compiler) validatePages(metaNode ast.Node) {
	nodes := make(map[string]Node, len(c.manifest.Nodes))
	for _, node := range c.manifest.Nodes {
		nodes[node.ID] = node
	}
	for id, reason := range c.manifest.Meta.NodeResultPageOmissions {
		node, exists := nodes[id]
		if !exists || node.Kind != "worker" || strings.TrimSpace(reason) == "" {
			c.fail(metaNode, "RESULT_PAGE_OMISSION_INVALID", "page omission requires a worker identity and a concrete reason: "+id)
		}
		_, page := c.manifest.Meta.NodeResultPages[id]
		_, files := c.manifest.Meta.NodeResultFiles[id]
		if page || files {
			c.fail(metaNode, "RESULT_PAGE_DECISION_CONFLICT", "worker cannot declare both a presentation and an omission: "+id)
		}
	}
	for id, files := range c.manifest.Meta.NodeResultFiles {
		node, exists := nodes[id]
		if !exists || node.Kind != "worker" || strings.TrimSpace(files.Title) == "" {
			c.fail(metaNode, "RESULT_FILES_INVALID", "file delivery requires a worker identity and a title: "+id)
			continue
		}
		if _, page := c.manifest.Meta.NodeResultPages[id]; page {
			c.fail(metaNode, "RESULT_PAGE_DECISION_CONFLICT", "worker must choose a result page or file delivery: "+id)
		}
		schema, _ := node.Options["schema"].(map[string]any)
		if !schemaDeclaresFiles(schema) {
			c.fail(metaNode, "RESULT_FILES_SCHEMA_REQUIRED", "file delivery requires a static schema with format:file: "+id)
		}
	}
	encoded, _ := json.Marshal(c.manifest.Meta.NodeResultPages)
	if len(encoded) > constants.WorkflowPagesMaximumBytes {
		c.fail(metaNode, "PAGES_BUDGET_EXCEEDED", "node result pages exceed 512 KiB")
	}
	for id, page := range c.manifest.Meta.NodeResultPages {
		page.CatalogDigest = presentationContract().Digests[page.CatalogVersion]
		c.manifest.Meta.NodeResultPages[id] = page
		node, exists := nodes[id]
		if !exists || node.Kind != "worker" {
			c.fail(metaNode, "RESULT_PAGE_NODE_INVALID", "result page must reference a worker node: "+id)
			continue
		}
		schema, _ := node.Options["schema"].(map[string]any)
		if len(schema) == 0 {
			c.fail(metaNode, "RESULT_PAGE_SCHEMA_REQUIRED", "result page requires a static worker output schema: "+id)
			continue
		}
		if reason := validatePage(page, constants.WorkflowResultCatalogVersion, schema); reason != "" {
			c.fail(metaNode, "RESULT_PAGE_INVALID", id+": "+reason)
		}
	}
	if page := c.manifest.Meta.InputPage; page != nil {
		page.CatalogDigest = presentationContract().Digests[page.CatalogVersion]
		if reason := validatePage(*page, constants.WorkflowInputCatalogVersion, c.manifest.Meta.InputSchema); reason != "" {
			c.fail(metaNode, "INPUT_PAGE_INVALID", reason)
		}
	}
}

// validatePage 只校验文档、目录、引用和权限，不解释 json-render 表达式。
func validatePage(page PageDefinition, version string, dataSchema map[string]any) string {
	if page.FormatVersion != constants.WorkflowPageFormatVersion || page.CatalogVersion != version {
		return "unsupported page or catalog version"
	}
	encoded, _ := json.Marshal(page)
	if len(encoded) > constants.WorkflowPageMaximumBytes {
		return "page exceeds 128 KiB"
	}
	if len(dataSchema) == 0 {
		return "data schema is required"
	}
	for key := range page.Spec {
		if key != "root" && key != "elements" {
			return "unsupported spec field: " + key
		}
	}
	root, _ := page.Spec["root"].(string)
	elements, _ := page.Spec["elements"].(map[string]any)
	if root == "" || elements[root] == nil || len(elements) > constants.WorkflowPageMaximumElements {
		return "invalid root or element budget"
	}
	catalog := presentationContract().Catalogs[version]
	visited, visiting := map[string]bool{}, map[string]bool{}
	covered := map[string]bool{}
	var walk func(string, map[string]any, int) string
	walk = func(id string, itemSchema map[string]any, depth int) string {
		item := itemSchema != nil
		childItemSchema := itemSchema
		if depth > constants.WorkflowPageMaximumDepth || visiting[id] {
			return "element cycle or depth limit"
		}
		if visited[id] {
			return "element has multiple parents: " + id
		}
		element, ok := elements[id].(map[string]any)
		if !ok {
			return "missing element: " + id
		}
		for key := range element {
			if key != "type" && key != "props" && key != "children" && key != "visible" && key != "repeat" {
				return "unsupported element field: " + key
			}
		}
		kind, _ := element["type"].(string)
		props, ok := element["props"].(map[string]any)
		if !ok {
			return "props must be an object"
		}
		propSchema := catalog[kind]
		if propSchema == nil || ValidateJSON(propSchema, props) != nil {
			return "unknown component or invalid props: " + kind
		}
		if version == constants.WorkflowInputCatalogVersion && element["visible"] != nil && element["visible"] != true {
			return "input layout cannot hide schema fields"
		}
		if kind == "Field" {
			path, _ := props["path"].(string)
			if !schemaHasPointer(dataSchema, strings.TrimPrefix(path, "/args"), 0) {
				return "input field does not exist: " + path
			}
			if !inputFieldAncestorsRequired(dataSchema, path) {
				return "optional, nullable or composed ancestors require their own Field: " + path
			}
			if element["visible"] != nil && element["visible"] != true {
				return "schema fields cannot be conditionally hidden"
			}
			for existing := range covered {
				if existing == path || strings.HasPrefix(existing, path+"/") || strings.HasPrefix(path, existing+"/") {
					return "overlapping input fields: " + path
				}
			}
			if covered[path] {
				return "duplicate input field: " + path
			}
			covered[path] = true
		}
		if repeat, exists := element["repeat"]; exists {
			if kind == "Collection" {
				return "Collection already supplies a paginated item scope; do not also repeat it"
			}
			if version == constants.WorkflowInputCatalogVersion {
				return "input fields use schema-owned arrays"
			}
			rule, ok := repeat.(map[string]any)
			if !ok || len(rule) > 2 || rule["statePath"] == nil {
				return "invalid repeat"
			}
			for key := range rule {
				if key != "statePath" && key != "key" {
					return "invalid repeat field"
				}
			}
			path := rule["statePath"]
			if text, ok := path.(string); ok {
				if (!strings.HasPrefix(text, "/result/") && text != "/result") || !schemaHasPointer(dataSchema, strings.TrimPrefix(text, "/result"), 0) {
					return "repeat must read /result"
				}
			} else {
				bound, ok := path.(map[string]any)
				if !ok || len(bound) != 1 || bound["$item"] == nil || !item {
					return "nested repeat requires an item array reference"
				}
			}
			source := path
			if text, ok := path.(string); ok {
				source = map[string]any{"$state": text}
			}
			childItemSchema = pageArrayItemSchema(dataSchema, pageValueSchema(dataSchema, itemSchema, source), 0)
			if childItemSchema == nil {
				return "repeat source must be a declared array"
			}
			if key, exists := rule["key"]; exists {
				name, ok := key.(string)
				if !ok || !schemaPointerExists(dataSchema, childItemSchema, "/"+pointerKey(name), 0) {
					return "repeat key must be a declared item field"
				}
			}
		}
		if reason := validatePageBindings(props, dataSchema, version, 0); reason != "" {
			return reason
		}
		if reason := validatePageValues(props, item, version, 0); reason != "" {
			return reason
		}
		if reason := validatePageBindings(element["visible"], dataSchema, version, 0); reason != "" {
			return reason
		}
		if reason := validatePageCondition(element["visible"], item, version, 0); reason != "" {
			return reason
		}
		if reason := validatePageItemBindings(props, dataSchema, itemSchema, 0); reason != "" {
			return reason
		}
		if reason := validatePageItemBindings(element["visible"], dataSchema, itemSchema, 0); reason != "" {
			return reason
		}
		if kind == "Collection" {
			childItemSchema = pageArrayItemSchema(dataSchema, pageValueSchema(dataSchema, itemSchema, props["items"]), 0)
			if childItemSchema == nil {
				return "Collection items must bind a declared array"
			}
		}
		visiting[id], visited[id] = true, true
		children, exists := element["children"]
		if exists {
			values, ok := children.([]any)
			if !ok {
				return "children must be element IDs"
			}
			for _, child := range values {
				name, ok := child.(string)
				if !ok {
					return "child must be an element ID"
				}
				if reason := walk(name, childItemSchema, depth+1); reason != "" {
					return reason
				}
			}
		}
		visiting[id] = false
		return ""
	}
	if reason := walk(root, nil, 0); reason != "" {
		return reason
	}
	if len(visited) != len(elements) {
		return "unreachable elements are not allowed"
	}
	if version == constants.WorkflowInputCatalogVersion {
		if covered["/args"] {
			return ""
		}
		properties, _ := dataSchema["properties"].(map[string]any)
		for name := range properties {
			child, _ := properties[name].(map[string]any)
			if !inputFieldsCovered(child, "/args/"+pointerKey(name), covered, 0) {
				return "input page must cover every input field: " + name
			}
		}
		if len(properties) == 0 {
			return "input page must include Field /args for composed or scalar schemas"
		}
	}
	return ""
}

func validatePageValues(value any, item bool, version string, depth int) string {
	if depth > constants.WorkflowPageMaximumDepth {
		return "expression depth limit"
	}
	switch value := value.(type) {
	case []any:
		for _, child := range value {
			if reason := validatePageValues(child, item, version, depth+1); reason != "" {
				return reason
			}
		}
	case map[string]any:
		for _, directive := range []string{"$state", "$item", "$index"} {
			if _, exists := value[directive]; exists && len(value) != 1 {
				return "invalid expression shape"
			}
		}
		if condition, exists := value["$cond"]; exists {
			if len(value) != 3 {
				return "conditional props require $cond/$then/$else"
			}
			if _, ok := value["$then"]; !ok {
				return "conditional props require $then"
			}
			if _, ok := value["$else"]; !ok {
				return "conditional props require $else"
			}
			if reason := validatePageCondition(condition, item, version, depth+1); reason != "" {
				return reason
			}
		}
		for key, child := range value {
			if key == "$cond" {
				continue
			}
			if strings.HasPrefix(key, "$") {
				switch key {
				case "$state":
					path, ok := child.(string)
					prefix := "/result"
					if version == constants.WorkflowInputCatalogVersion {
						prefix = "/args"
					}
					if !ok || (path != prefix && !strings.HasPrefix(path, prefix+"/")) {
						return "state path outside page data"
					}
				case "$item", "$index":
					if key == "$index" && child != true {
						return "index expression requires true"
					}
					if key == "$item" {
						if _, ok := child.(string); !ok {
							return "item expression requires a string"
						}
					}
					if !item {
						return "item expression outside a collection or repeat"
					}
				case "$then", "$else":
					if value["$cond"] == nil {
						return "conditional branch outside $cond"
					}
				default:
					return "unsupported directive: " + key
				}
			}
			if reason := validatePageValues(child, item, version, depth+1); reason != "" {
				return reason
			}
		}
	}
	return ""
}

func inputFieldsCovered(schema map[string]any, path string, covered map[string]bool, depth int) bool {
	if covered[path] {
		return true
	}
	if depth > constants.WorkflowPageMaximumDepth {
		return false
	}
	properties, _ := schema["properties"].(map[string]any)
	if schema["type"] != "object" || len(properties) == 0 || schema["oneOf"] != nil || schema["anyOf"] != nil || schema["allOf"] != nil || schema["$ref"] != nil {
		return false
	}
	for key, child := range properties {
		node, _ := child.(map[string]any)
		if !inputFieldsCovered(node, path+"/"+pointerKey(key), covered, depth+1) {
			return false
		}
	}
	return true
}

// inputFieldAncestorsRequired 保留父对象的省略、null 和分支控件，避免叶字段绕过其语义。
func inputFieldAncestorsRequired(root map[string]any, path string) bool {
	if path == "/args" {
		return true
	}
	parts := strings.Split(strings.TrimPrefix(path, "/args/"), "/")
	current := pageSchemaAtPointer(root, root, "", 0)
	for _, part := range parts[:len(parts)-1] {
		key := strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		required, _ := current["required"].([]any)
		found := false
		for _, name := range required {
			if name == key {
				found = true
			}
		}
		if !found {
			return false
		}
		current = pageSchemaAtPointer(root, current, "/"+part, 0)
		if current == nil || current["type"] != "object" || current["oneOf"] != nil || current["anyOf"] != nil || current["allOf"] != nil {
			return false
		}
	}
	return true
}

func validatePageCondition(value any, item bool, version string, depth int) string {
	if depth > constants.WorkflowPageMaximumDepth {
		return "condition depth limit"
	}
	if value == nil {
		return ""
	}
	if _, ok := value.(bool); ok {
		return ""
	}
	if values, ok := value.([]any); ok {
		for _, child := range values {
			if reason := validatePageCondition(child, item, version, depth+1); reason != "" {
				return reason
			}
		}
		return ""
	}
	condition, ok := value.(map[string]any)
	if !ok {
		return "invalid visibility condition"
	}
	for _, operator := range []string{"$and", "$or"} {
		if children, exists := condition[operator]; exists {
			if len(condition) != 1 {
				return "invalid logical condition"
			}
			values, ok := children.([]any)
			if !ok {
				return "logical condition requires an array"
			}
			return validatePageCondition(values, item, version, depth+1)
		}
	}
	scopes, comparisons := 0, 0
	for key, child := range condition {
		switch key {
		case "$state", "$item", "$index":
			scopes++
			if reason := validatePageValues(map[string]any{key: child}, item, version, depth+1); reason != "" {
				return reason
			}
		case "eq", "neq":
			comparisons++
			if reason := validatePageValues(child, item, version, depth+1); reason != "" {
				return reason
			}
		case "gt", "gte", "lt", "lte":
			comparisons++
			if _, ok := child.(float64); !ok {
				if reason := validatePageValues(child, item, version, depth+1); reason != "" {
					return reason
				}
				bound, ok := child.(map[string]any)
				if !ok || len(bound) != 1 || bound["$state"] == nil {
					return "numeric comparison requires a number or state reference"
				}
			}
		case "not":
			if child != true {
				return "not requires true"
			}
		default:
			return "unknown visibility operator: " + key
		}
	}
	if scopes != 1 || comparisons > 1 {
		return "visibility requires one scope and at most one comparison"
	}
	return ""
}

func validatePageBindings(value any, schema map[string]any, version string, depth int) string {
	if depth > constants.WorkflowPageMaximumDepth {
		return "binding depth limit"
	}
	switch value := value.(type) {
	case []any:
		for _, child := range value {
			if reason := validatePageBindings(child, schema, version, depth+1); reason != "" {
				return reason
			}
		}
	case map[string]any:
		if path, ok := value["$state"].(string); ok {
			prefix := "/result"
			if version == constants.WorkflowInputCatalogVersion {
				prefix = "/args"
			}
			if !schemaHasPointer(schema, strings.TrimPrefix(path, prefix), 0) {
				return "state binding absent from schema: " + path
			}
		}
		for _, child := range value {
			if reason := validatePageBindings(child, schema, version, depth+1); reason != "" {
				return reason
			}
		}
	}
	return ""
}

func schemaHasPointer(schema map[string]any, pointer string, depth int) bool {
	return schemaPointerExists(schema, schema, pointer, depth)
}

func schemaPointerExists(root, schema map[string]any, pointer string, depth int) bool {
	return pageSchemaAtPointer(root, schema, pointer, depth) != nil
}

// pageSchemaAtPointer 查询声明的结构，仅作契约校验，不参与运行时表达式求值。
func pageSchemaAtPointer(root, schema map[string]any, pointer string, depth int) map[string]any {
	if depth > constants.WorkflowPageMaximumDepth || schema == nil {
		return nil
	}
	if ref, ok := schema["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
		current := root
		for _, key := range strings.Split(ref[2:], "/") {
			key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
			current, _ = current[key].(map[string]any)
		}
		if resolved := pageSchemaAtPointer(root, current, pointer, depth+1); resolved != nil {
			return resolved
		}
	}
	if pointer == "" || pointer == "/" {
		return schema
	}
	for _, keyword := range []string{"allOf", "oneOf", "anyOf"} {
		branches, _ := schema[keyword].([]any)
		for _, branch := range branches {
			object, _ := branch.(map[string]any)
			if resolved := pageSchemaAtPointer(root, object, pointer, depth+1); resolved != nil {
				return resolved
			}
		}
	}
	parts := strings.SplitN(strings.TrimPrefix(pointer, "/"), "/", 2)
	key := strings.ReplaceAll(strings.ReplaceAll(parts[0], "~1", "/"), "~0", "~")
	properties, _ := schema["properties"].(map[string]any)
	child, _ := properties[key].(map[string]any)
	if schema["type"] == "array" {
		if _, err := strconv.Atoi(key); err == nil {
			child, _ = schema["items"].(map[string]any)
		}
	}
	rest := ""
	if len(parts) == 2 {
		rest = "/" + parts[1]
	}
	return pageSchemaAtPointer(root, child, rest, depth+1)
}

func pageValueSchema(root, item map[string]any, value any) map[string]any {
	bound, ok := value.(map[string]any)
	if !ok || len(bound) != 1 {
		return nil
	}
	if path, ok := bound["$state"].(string); ok && (path == "/result" || strings.HasPrefix(path, "/result/")) {
		return pageSchemaAtPointer(root, root, strings.TrimPrefix(path, "/result"), 0)
	}
	if path, ok := bound["$item"].(string); ok {
		return pageSchemaAtPointer(root, item, path, 0)
	}
	return nil
}

func pageArrayItemSchema(root, schema map[string]any, depth int) map[string]any {
	if depth > constants.WorkflowPageMaximumDepth || schema == nil {
		return nil
	}
	schema = pageSchemaAtPointer(root, schema, "", depth+1)
	if schema == nil {
		return nil
	}
	if schema["type"] == "array" {
		item, _ := schema["items"].(map[string]any)
		return item
	}
	for _, keyword := range []string{"allOf", "oneOf", "anyOf"} {
		branches, _ := schema[keyword].([]any)
		for _, branch := range branches {
			object, _ := branch.(map[string]any)
			if item := pageArrayItemSchema(root, object, depth+1); item != nil {
				return item
			}
		}
	}
	return nil
}

func validatePageItemBindings(value any, root, item map[string]any, depth int) string {
	if depth > constants.WorkflowPageMaximumDepth {
		return "item binding depth limit"
	}
	switch value := value.(type) {
	case []any:
		for _, child := range value {
			if reason := validatePageItemBindings(child, root, item, depth+1); reason != "" {
				return reason
			}
		}
	case map[string]any:
		if path, ok := value["$item"].(string); ok && pageSchemaAtPointer(root, item, path, 0) == nil {
			return "item binding absent from schema: " + path
		}
		for _, child := range value {
			if reason := validatePageItemBindings(child, root, item, depth+1); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// presentationKnowledge 从实际生成目录构建创作主题，不采用包含内置业务动作的默认提示。
func presentationKnowledge() string {
	data, _ := presentationCatalogFiles.ReadFile("presentation-catalog.generated.json")
	return `# Workflow SDK: presentation

Evaluate every worker output, including intermediate steps, and choose exactly one presentation declaration. For requested file deliverables (HTML, Markdown, PDF, Office, images, source files or multiple files), declare meta.nodeResultFiles[stableWorkerId]:{title,description?}. It requires a static worker schema with format:"file" on the actual file values; a single file may use a root string schema, several files an array of file strings, or named file fields when downstream consumers need them. The host derives the file inventory from successful results. All file cards, including images, open its existing file viewer in a closable workbench tab. A single file opens directly in preview; multiple or mixed files open the first available file with a selectable, resizable file list. The viewer retains per-file code/preview choices, image zoom and scrolling. Keep canvas cards compact. Image generation uses the same file contract, without requiring JSON descriptions. No json-render Spec is needed. The worker must actually create files in the approved workflow scope at run-specific paths, verify they exist and submit their references through allycli workflow results submit. For a root file schema, data is the path itself. The result protocol is still required, but report content need not be duplicated into JSON fields or streaming JSONL.

For readable structured content composed inside NeuxBot, jointly author a static output schema and meta.nodeResultPages[stableWorkerId]. Plain text can use a page. For only internal technical/control data with no reading value, or user-declined display, write a concrete meta.nodeResultPageOmissions[stableWorkerId] reason. Intermediate status alone does not justify omission. Every worker must appear in exactly one of these three maps; allycli workflow source validate and allycli workflow source save reject missing or conflicting declarations and file views without declared file fields. When the user changes the deliverable, revise the prompt, schema, presentation declaration, downstream consumers and final return together. Trace old prompts, schemas/required lists, submitted fields, presentation bindings, downstream expressions, functions/actions and final returns. Remove abandoned fields, formatting steps and stale labels together; retain actual branch/loop consumers and agreed external contracts. Every remaining runtime read or submitted field must exist in its schema. Remove unused duplicate report fields and the old page rather than appending a FileList to it. Preserve evidence and coverage inside the actual document. File delivery should let the agent design for the requested format and reader, without imposing a fixed page template.

Each result page has formatVersion:1,catalogVersion:"workflow-result-v1",title,description and native json-render spec:{root,elements}. Presentations are frozen with the source revision and show exactly one successful attempt. They add no execution node, phase, action or model call. Never embed business data or secrets in page metadata.

For nodeResultPages, design a complete reading experience. The Agent decides the whole component tree, content hierarchy, reading width, grouping, columns, semantic surfaces, emphasis, alignment and spacing using the actual component properties. Start with Page and one Heading h1; this makes the native Spec own the full page header. Put the primary conclusion or deliverable first. Compose Card, Stack, Grid, Heading, Text, Metric, Callout, Collection and Disclosure as needed, not as a fixed template. Reports, comparisons and recommendation lists should have different layouts. Avoid flattening every schema property into KeyValue, duplicate titles or equivalent metrics, giant whitespace, nesting Card inside Card, or centered full-width source links. Use readable width for prose, grid/list Collection for repeated entries, and Disclosure only for secondary evidence. Preserve true coverage and uncertainty; bind numbers and content to the actual result. Allow empty results and long content. The renderer responds to its own pane width.

Use components in the actual catalog below and preserve the native flat root/elements format. Element fields are type, props, children, visible, repeat. children contains element IDs; all elements must be reachable and acyclic. Read output through {$state:"/result/path"}. Collection takes items (normally {$state:"/result/items"}), pageSize (1..50), and ordinary children: its public native repeat scope resolves {$item:"field"} and {$index:true}. Nested Collection uses {$item:"nestedArray"}, without a repeat on Collection itself. Text and Markdown use value; Table takes rows and columns:[{key,label}] where key is a literal row field. Keep business calculations in the workflow. Use format:file and FileList only for an actual file the user requested or agreed to receive, with a readable label. Internal JSON caches, complete API snapshots and intermediate files are implementation details: keep them out of the delivery schema and page, do not append them as downloads. Ordinary strings are not downloadable files. The user should read the output directly on the page.

Allowed read-only directives are $state, $item, $index and native conditional props ($cond/$then/$else). Native visibility may use eq/neq/gt/gte/lt/lte/not/and/or. Never use on, watch, actions, $bindState, $bindItem, $computed, $template, spec.state, style, DOM tags, JavaScript, external resources or custom directives. Do not use the library default streaming JSONL prompt: save the complete workflow source through allycli workflow source validate and allycli workflow source save.

An optional meta.inputPage uses formatVersion:1,catalogVersion:"workflow-input-v1",spec. Use layout components and Field with a literal path such as "/args/query". Every input field must be covered. Preserve optional/nullable/composed parent objects as whole Field controls; only required plain objects may be split into leaf fields. Field "/args" represents the whole schema, including composed/scalar schemas. Field type, label, required state, choices, file input and validation come from inputSchema; do not duplicate them or hide fields conditionally. Fields write only their own local draft via the host. Submit/approval/save remain business host controls. Normal forms require no inputPage: the host derives a deterministic layout.

Budgets: a page is at most 128 KiB, 256 elements and 32 levels; tables at most 20 columns. Collections paginate rather than mounting all items. Query this topic during authoring whenever the workflow has workers. Before saving, check every worker's page, file delivery or omission reason, and ensure all page bindings match the declared output schema.

## Actual component property schemas

` + "```json\n" + string(data) + "\n```\n"
}
