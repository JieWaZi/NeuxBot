// Package input 集中处理自动任务的参数来源、调用契约与实际输入。
package input

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// Binding 保存一个工作流输入字段的唯一来源。
type Binding struct {
	// Source 是固定、API 或工作流默认来源。
	Source constants.WorkflowInputSource `json:"source"`
	// Value 是固定来源的原始 JSON 值，允许显式 null。
	Value any `json:"value,omitempty"`
	// provided 区分未填写固定值与显式 null。
	provided bool
}

// Fixed 创建具有明确值的固定来源，Go 调用方也可以准确表达显式 null。
func Fixed(value any) Binding {
	return Binding{Source: constants.WorkflowInputSourceFixed, Value: value, provided: true}
}

// UnmarshalJSON 保留固定值的存在性，避免把 null 当作缺失。
func (b *Binding) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*b = Binding{}
	for key, value := range fields {
		switch key {
		case "source":
			if err := json.Unmarshal(value, &b.Source); err != nil {
				return err
			}
		case "value":
			b.provided = true
			if err := json.Unmarshal(value, &b.Value); err != nil {
				return err
			}
		default:
			return status_error.AutomaticTaskInputInvalid.WithField("constraint", "binding_field")
		}
	}
	return nil
}

// MarshalJSON 仅在值确实存在时保存 value，保留显式 null。
func (b Binding) MarshalJSON() ([]byte, error) {
	fields := map[string]any{"source": b.Source}
	if b.provided || b.Value != nil {
		fields["value"] = b.Value
	}
	return json.Marshal(fields)
}

// Field 是一个参数的只读展示与调用说明。
type Field struct {
	// Key 是工作流定义中的稳定字段名。
	Key string `json:"key"`
	// Title 是面向用户的参数名称。
	Title string `json:"title"`
	// Description 说明参数用途与格式。
	Description string `json:"description"`
	// Type 是明确的 JSON 类型及数组元素类型。
	Type string `json:"type"`
	// Required 表示工作流要求此字段存在。
	Required bool `json:"required"`
	// Source 是此任务选择的参数来源。
	Source constants.WorkflowInputSource `json:"source"`
	// Schema 是保留局部引用的单字段契约。
	Schema map[string]any `json:"schema"`
}

// Contract 是从权威工作流版本与任务来源派生的调用契约。
type Contract struct {
	// Fields 保留全部参数的来源、类型与说明。
	Fields []Field `json:"fields"`
	// Schema 只包含调用方可提供的参数。
	Schema map[string]any `json:"schema"`
	// Example 是仅包含开放字段的示例对象。
	Example map[string]any `json:"example"`
	// ExampleValid 表示示例通过开放参数及完整输入校验。
	ExampleValid bool `json:"exampleValid"`
}

func invalid(key, constraint string) error {
	return status_error.AutomaticTaskInputInvalid.WithField("jsonPointer", "/args/"+escape(key)).WithField("constraint", constraint)
}
func escape(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func normalized(schema map[string]any) (map[string]any, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, status_error.WorkflowSchemaInvalid
	}
	var root map[string]any
	if json.Unmarshal(data, &root) != nil {
		return nil, status_error.WorkflowSchemaInvalid
	}
	return root, nil
}

func resolved(schema, root map[string]any, seen map[string]bool) map[string]any {
	ref, _ := schema["$ref"].(string)
	if !strings.HasPrefix(ref, "#/") || seen[ref] {
		return schema
	}
	seen[ref] = true
	var value any = root
	for _, part := range strings.Split(ref[2:], "/") {
		object, ok := value.(map[string]any)
		if !ok {
			return schema
		}
		value = object[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
	}
	target, ok := value.(map[string]any)
	if !ok {
		return schema
	}
	result := map[string]any{}
	for key, value := range resolved(target, root, seen) {
		result[key] = value
	}
	for key, value := range schema {
		if key != "$ref" {
			result[key] = value
		}
	}
	return result
}

func typeName(schema, root map[string]any, depth int) string {
	if depth > 16 {
		return "json"
	}
	schema = resolved(schema, root, map[string]bool{})
	if schema["format"] == constants.WorkflowFileFormat {
		return constants.WorkflowFileFormat
	}
	if types, ok := schema["type"].([]any); ok {
		parts := []string{}
		for _, entry := range types {
			if value, ok := entry.(string); ok {
				if value == "array" {
					items, _ := schema["items"].(map[string]any)
					value = "array<" + typeName(items, root, depth+1) + ">"
				}
				parts = append(parts, value)
			}
		}
		return strings.Join(parts, " | ")
	}
	kind, _ := schema["type"].(string)
	if kind == "array" {
		items, _ := schema["items"].(map[string]any)
		return "array<" + typeName(items, root, depth+1) + ">"
	}
	if kind == "" {
		return "json"
	}
	return kind
}

func objectSchema(root map[string]any) (map[string]any, error) {
	if len(root) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}, nil
	}
	result := resolved(root, root, map[string]bool{})
	if result["type"] != "object" || result["oneOf"] != nil || result["anyOf"] != nil || result["allOf"] != nil {
		return nil, status_error.AutomaticTaskInputInvalid.WithField("constraint", "object_schema_required")
	}
	return result, nil
}

func required(schema map[string]any, key string) bool {
	values, _ := schema["required"].([]any)
	for _, value := range values {
		if value == key {
			return true
		}
	}
	return false
}

// fieldSchema 复制必要的引用上下文，保持单字段及开放契约中的本地引用有效。
func fieldSchema(root, property map[string]any) map[string]any {
	result := rewriteReferences(property)
	if !containsReference(property) {
		return result
	}
	result["$defs"] = map[string]any{"__ally_input_root": rewriteReferences(root)}
	return result
}

// containsReference 避免无引用的普通字段携带整份输入定义，控制参数说明的大小。
func containsReference(value any) bool {
	switch item := value.(type) {
	case map[string]any:
		if _, ok := item["$ref"].(string); ok {
			return true
		}
		for _, child := range item {
			if containsReference(child) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if containsReference(child) {
				return true
			}
		}
	}
	return false
}

func rewriteReferences(schema map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range schema {
		switch key {
		case "$ref":
			if ref, ok := value.(string); ok && strings.HasPrefix(ref, "#/") {
				result[key] = "#/$defs/__ally_input_root/" + ref[2:]
			} else {
				result[key] = value
			}
		case "properties", "$defs":
			fields, _ := value.(map[string]any)
			next := map[string]any{}
			for name, child := range fields {
				if object, ok := child.(map[string]any); ok {
					next[name] = rewriteReferences(object)
				}
			}
			result[key] = next
		case "items", "not", "additionalProperties":
			if child, ok := value.(map[string]any); ok {
				result[key] = rewriteReferences(child)
			} else {
				result[key] = value
			}
		case "oneOf", "anyOf", "allOf", "prefixItems":
			children, _ := value.([]any)
			next := []any{}
			for _, child := range children {
				if object, ok := child.(map[string]any); ok {
					next = append(next, rewriteReferences(object))
				}
			}
			result[key] = next
		case "$id": // 迁移到局部上下文后引用继续相对契约根解析。
		default:
			result[key] = value
		}
	}
	return result
}

func schemaFailure(schema map[string]any, cause error) error {
	pointer := ""
	var detail *statuserror.StatusErr
	if errors.As(cause, &detail) {
		pointer = detail.Fields["jsonPointer"]
	}
	field := schema
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		field = resolved(field, schema, map[string]bool{})
		if field["type"] == "array" {
			field, _ = field["items"].(map[string]any)
		} else {
			properties, _ := field["properties"].(map[string]any)
			field, _ = properties[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")].(map[string]any)
		}
	}
	return status_error.AutomaticTaskInputInvalid.WithField("jsonPointer", "/args"+pointer).WithField("constraint", "schema").WithField("expectedType", typeName(field, schema, 0))
}

// Analyze 验证来源配置并派生唯一的参数说明，不要求 API 参数已有本次值。
func Analyze(schema map[string]any, bindings map[string]Binding, allowAPI bool, order []string) (Contract, error) {
	root, err := normalized(schema)
	if err != nil {
		return Contract{}, err
	}
	if _, err := workflowdsl.ResolveSchema(root); err != nil {
		return Contract{}, err
	}
	object, err := objectSchema(root)
	if err != nil {
		return Contract{}, err
	}
	properties, _ := object["properties"].(map[string]any)
	for key := range bindings {
		if _, exists := properties[key]; !exists {
			return Contract{}, invalid(key, "unknown_field")
		}
	}
	keys := []string{}
	seen := map[string]bool{}
	for _, key := range order {
		if _, exists := properties[key]; exists && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	rest := []string{}
	for key := range properties {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)
	contract := Contract{Fields: []Field{}, Schema: map[string]any{"type": "object", "additionalProperties": false}, Example: map[string]any{}}
	apiProperties := map[string]any{}
	apiRequired := []string{}
	for _, key := range keys {
		property, ok := properties[key].(map[string]any)
		if !ok {
			return Contract{}, invalid(key, "schema")
		}
		presentation := resolved(property, root, map[string]bool{})
		binding, exists := bindings[key]
		if !exists {
			binding.Source = constants.WorkflowInputSourceDefault
		}
		field := Field{Key: key, Required: required(object, key), Source: binding.Source, Schema: fieldSchema(root, property), Type: typeName(property, root, 0)}
		field.Title, _ = presentation["title"].(string)
		if field.Title == "" {
			field.Title = key
		}
		field.Description, _ = presentation["description"].(string)
		switch binding.Source {
		case constants.WorkflowInputSourceFixed:
			if !binding.provided && binding.Value == nil {
				return Contract{}, invalid(key, "fixed_value_required")
			}
			if err := workflowdsl.ValidateJSON(field.Schema, workflowdsl.ApplyDefaults(field.Schema, binding.Value)); err != nil {
				return Contract{}, invalid(key, "fixed_value_schema")
			}
		case constants.WorkflowInputSourceAPI:
			if !allowAPI {
				return Contract{}, invalid(key, "api_source_unavailable")
			}
			if field.Type == "json" {
				return Contract{}, invalid(key, "type_required")
			}
			apiProperties[key] = rewriteReferences(property)
			if field.Required {
				apiRequired = append(apiRequired, key)
			}
			if value, ok := example(root, property, 0); ok || field.Required {
				contract.Example[key] = value
			}
		case constants.WorkflowInputSourceDefault:
			defaults := workflowdsl.ApplyDefaults(root, map[string]any{}).(map[string]any)
			if field.Required {
				if _, exists := defaults[key]; !exists {
					return Contract{}, invalid(key, "default_required")
				}
			}
		default:
			return Contract{}, invalid(key, "source_required")
		}
		if binding.Source != constants.WorkflowInputSourceFixed && (binding.provided || binding.Value != nil) {
			return Contract{}, invalid(key, "unexpected_fixed_value")
		}
		contract.Fields = append(contract.Fields, field)
	}
	contract.Schema["properties"] = apiProperties
	contract.Schema["required"] = apiRequired
	if containsReference(apiProperties) {
		contract.Schema["$defs"] = map[string]any{"__ally_input_root": rewriteReferences(root)}
	}
	if !allowAPI {
		if _, err := Resolve(schema, bindings, map[string]any{}); err != nil {
			return Contract{}, err
		}
	}
	_, exampleError := Resolve(schema, bindings, contract.Example)
	contract.ExampleValid = exampleError == nil
	return contract, nil
}

// Resolve 拒绝未开放字段、检查 API 必填项并校验合成后的完整输入。
func Resolve(schema map[string]any, bindings map[string]Binding, args map[string]any) (map[string]any, error) {
	root, err := normalized(schema)
	if err != nil {
		return nil, err
	}
	object, err := objectSchema(root)
	if err != nil {
		return nil, err
	}
	properties, _ := object["properties"].(map[string]any)
	result := map[string]any{}
	for key, binding := range bindings {
		if binding.Source == constants.WorkflowInputSourceFixed {
			result[key] = binding.Value
		}
	}
	for key, value := range args {
		if bindings[key].Source != constants.WorkflowInputSourceAPI {
			return nil, invalid(key, "field_not_exposed")
		}
		result[key] = value
	}
	for key := range properties {
		if bindings[key].Source == constants.WorkflowInputSourceAPI && required(object, key) {
			if _, exists := args[key]; !exists {
				return nil, invalid(key, "required")
			}
		}
	}
	result, _ = workflowdsl.ApplyDefaults(root, result).(map[string]any)
	if err := workflowdsl.ValidateJSON(root, result); err != nil {
		return nil, schemaFailure(root, err)
	}
	return result, nil
}

// Compatible 保守比较已开放字段的契约，并校验新增参数及固定值。
func Compatible(before, after map[string]any, bindings map[string]Binding, allowAPI bool) error {
	old, err := normalized(before)
	if err != nil {
		return err
	}
	current, err := normalized(after)
	if err != nil {
		return err
	}
	oldObject, err := objectSchema(old)
	if err != nil {
		return err
	}
	newObject, err := objectSchema(current)
	if err != nil {
		return err
	}
	oldProperties, _ := oldObject["properties"].(map[string]any)
	newProperties, _ := newObject["properties"].(map[string]any)
	for key, value := range oldProperties {
		next, exists := newProperties[key]
		if !exists {
			return status_error.AutomaticTaskInputChanged
		}
		if bindings[key].Source == constants.WorkflowInputSourceAPI && (!reflect.DeepEqual(validationShape(value), validationShape(next)) || required(oldObject, key) != required(newObject, key) || !reflect.DeepEqual(validationShape(old["$defs"]), validationShape(current["$defs"]))) {
			return status_error.AutomaticTaskInputChanged
		}
	}
	if !reflect.DeepEqual(rootConstraints(oldObject), rootConstraints(newObject)) {
		return status_error.AutomaticTaskInputChanged
	}
	if _, err := Analyze(after, bindings, allowAPI, nil); err != nil {
		return status_error.AutomaticTaskInputChanged
	}
	return nil
}

func rootConstraints(schema map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range schema {
		if key != "properties" && key != "required" && key != "$defs" {
			result[key] = value
		}
	}
	return validationShape(result).(map[string]any)
}
func validationShape(value any) any {
	switch entry := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, child := range entry {
			if key != "title" && key != "description" && key != "examples" && !strings.HasPrefix(key, "x-ui-") {
				result[key] = validationShape(child)
			}
		}
		return result
	case []any:
		result := make([]any, len(entry))
		for index, child := range entry {
			result[index] = validationShape(child)
		}
		return result
	default:
		return value
	}
}

func example(root, property map[string]any, depth int) (any, bool) {
	schema := fieldSchema(root, property)
	presentation := resolved(property, root, map[string]bool{})
	if presentation["format"] == constants.WorkflowFileFormat {
		return "<FILE_REFERENCE>", true
	}
	if depth > 16 {
		return nil, false
	}
	candidates := []any{}
	if values, ok := presentation["examples"].([]any); ok {
		candidates = append(candidates, values...)
	}
	if values, ok := presentation["enum"].([]any); ok {
		candidates = append(candidates, values...)
	}
	if value, ok := presentation["const"]; ok {
		candidates = append(candidates, value)
	}
	if value, ok := presentation["default"]; ok {
		candidates = append(candidates, value)
	}
	kind, _ := presentation["type"].(string)
	if types, ok := presentation["type"].([]any); ok {
		for _, entry := range types {
			if entry == "null" {
				candidates = append(candidates, nil)
			} else if kind == "" {
				kind, _ = entry.(string)
			}
		}
	}
	var placeholder any
	switch kind {
	case "string":
		placeholder = ""
		candidates = append(candidates, "示例文本", "example_id", "")
	case "integer", "number":
		placeholder = float64(0)
		value := float64(1)
		if min, ok := presentation["minimum"].(float64); ok {
			value = min
		}
		candidates = append(candidates, value)
	case "boolean":
		placeholder = false
		candidates = append(candidates, false)
	case "array":
		items, _ := presentation["items"].(map[string]any)

		count := 1
		if min, ok := presentation["minItems"].(float64); ok && min > 1 && min < 100 {
			count = int(min)
		}
		values := []any{}
		for index := 0; index < count; index++ {
			value, _ := example(root, items, depth+1)
			values = append(values, value)
		}
		placeholder = values
		candidates = append(candidates, values, []any{})
	case "object":
		value := map[string]any{}
		fields, _ := presentation["properties"].(map[string]any)
		for key, child := range fields {
			if required(presentation, key) {
				if child, ok := child.(map[string]any); ok {
					sample, _ := example(root, child, depth+1)
					value[key] = sample
				}
			}
		}
		placeholder = value
		candidates = append(candidates, workflowdsl.ApplyDefaults(schema, value))
	}
	for _, value := range candidates {
		if workflowdsl.ValidateJSON(schema, value) == nil {
			return value, true
		}
	}
	// 无法满足约束时仍展示必填字段及其 JSON 类型，由调用方替换值。
	return placeholder, false
}

// ValidateJSONEnvelope 拒绝重复字段与尾随 JSON，避免请求有多种解释。
func ValidateJSONEnvelope(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var read func(int) error
	read = func(depth int) error {
		if depth > 64 {
			return status_error.AutomaticTaskInputInvalid
		}
		token, err := decoder.Token()
		if err != nil {
			return status_error.AutomaticTaskInputInvalid
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return status_error.AutomaticTaskInputInvalid
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return status_error.AutomaticTaskInputInvalid.WithField("constraint", "duplicate_field")
				}
				seen[name] = true
				if err := read(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := read(depth + 1); err != nil {
					return err
				}
			}
		default:
			return status_error.AutomaticTaskInputInvalid
		}
		_, err = decoder.Token()
		if err != nil {
			return status_error.AutomaticTaskInputInvalid
		}
		return nil
	}
	if err := read(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return status_error.AutomaticTaskInputInvalid
	}
	return nil
}
