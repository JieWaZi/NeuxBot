package dsl

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// canvasSchemaBuilder 从公开 Go 类型生成定义，仅为动态 execution 指定封闭原语契约。
type canvasSchemaBuilder struct {
	// definitions 保存当前文档内的类型引用，禁止外部 Schema 来源。
	definitions map[string]any
}

func (b *canvasSchemaBuilder) schema(value reflect.Type) map[string]any {
	if value.Kind() == reflect.Pointer {
		return b.schema(value.Elem())
	}
	switch value.Kind() {
	case reflect.Struct:
		name := value.Name()
		if _, known := b.definitions[name]; known {
			return map[string]any{"$ref": "#/$defs/" + name}
		}
		b.definitions[name] = map[string]any{}
		properties := map[string]any{}
		required := []any{}
		for i := 0; i < value.NumField(); i++ {
			field := value.Field(i)
			tag := field.Tag.Get("json")
			if field.PkgPath != "" || tag == "-" {
				continue
			}
			parts := strings.Split(tag, ",")
			name := parts[0]
			if name == "" {
				name = field.Name
			}
			properties[name] = b.schema(field.Type)
			optional := false
			for _, part := range parts[1:] {
				optional = optional || part == "omitempty"
			}
			if !optional {
				required = append(required, name)
			}
		}
		b.definitions[name] = closedCanvasObject(properties, required...)
		return map[string]any{"$ref": "#/$defs/" + name}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": b.schema(value.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": b.schema(value.Elem())}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	default:
		return map[string]any{}
	}
}

func closedCanvasObject(properties map[string]any, required ...any) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func canvasStringEnum(values ...any) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

// CanvasManifestSchema 是公开画布与 API 生成共用的本地封闭 JSON Schema。
func CanvasManifestSchema() map[string]any {
	builder := &canvasSchemaBuilder{definitions: map[string]any{}}
	schema := builder.schema(reflect.TypeFor[CanvasManifest]())
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["$defs"] = builder.definitions
	manifest := builder.definitions["CanvasManifest"].(map[string]any)
	fields := manifest["properties"].(map[string]any)
	fields["sdk"] = map[string]any{"enum": []any{constants.WorkflowSDKIdentifier}}
	base := builder.definitions["CanvasNode"].(map[string]any)
	baseFields := base["properties"].(map[string]any)
	baseFields["description"] = map[string]any{"type": "string", "maxLength": constants.WorkflowNodeDescriptionMaxRunes}
	text := map[string]any{"type": "string"}
	boolean := map[string]any{"type": "boolean"}
	integer := map[string]any{"type": "integer", "minimum": 0}
	object := map[string]any{"type": "object", "additionalProperties": true}
	stringsSchema := map[string]any{"type": "array", "items": text}
	resolve := closedCanvasObject(map[string]any{"kind": map[string]any{"enum": []any{"waker"}}, "wakerId": text, "runtimeId": text, "accountId": text, "model": text, "reasoning": text, "permissionLevel": canvasStringEnum("default", "auto", "full-access")}, "kind", "wakerId")
	connectorPolicy := map[string]any{"oneOf": []any{closedCanvasObject(map[string]any{"mode": map[string]any{"enum": []any{"inherit"}}}, "mode"), closedCanvasObject(map[string]any{"mode": map[string]any{"enum": []any{"allowlist"}}, "connectorIds": stringsSchema}, "mode", "connectorIds")}}
	nativePolicy := map[string]any{"oneOf": []any{closedCanvasObject(map[string]any{"mode": map[string]any{"enum": []any{"inherit"}}}, "mode"), closedCanvasObject(map[string]any{"mode": map[string]any{"enum": []any{"allowlist"}}, "capabilityIds": stringsSchema}, "mode", "capabilityIds")}}
	toolPolicy := closedCanvasObject(map[string]any{"connectors": connectorPolicy, "nativeTools": nativePolicy})
	executions := map[string]map[string]any{
		"input":         closedCanvasObject(map[string]any{"target": map[string]any{"enum": []any{"args"}}}, "target"),
		"result":        closedCanvasObject(map[string]any{"target": map[string]any{"enum": []any{"result"}}}, "target"),
		"worker":        closedCanvasObject(map[string]any{"resolve": resolve, "promptTemplate": text, "toolPolicy": toolPolicy}, "resolve"),
		"askUser":       closedCanvasObject(map[string]any{"kind": canvasStringEnum("text", "select", "multiselect", "form", "confirm"), "promptTemplate": text, "allowCustom": boolean, "allowSkip": boolean, "schema": object, "minSelections": integer, "maxSelections": integer, "timeoutMs": integer, "onTimeout": canvasStringEnum("fail", "skip", "default")}, "kind"),
		"functionTask":  closedCanvasObject(map[string]any{"language": canvasStringEnum("javascript", "python"), "executionMode": canvasStringEnum("pure", "trusted_local"), "codeDigest": text}, "language", "executionMode", "codeDigest"),
		"workflow":      closedCanvasObject(map[string]any{"definitionId": text, "revisionId": text, "mode": canvasStringEnum("joined", "detached")}, "definitionId", "mode"),
		"startWorkflow": closedCanvasObject(map[string]any{"definitionId": text, "revisionId": text, "mode": canvasStringEnum("joined", "detached")}, "definitionId", "mode"),
		"joinWorkflow":  closedCanvasObject(map[string]any{"handleExpression": text}, "handleExpression"),
	}
	actionVariants := []any{}
	actionVariant := func(properties map[string]any, required ...any) {
		actionVariants = append(actionVariants, closedCanvasObject(properties, required...))
	}
	actionVariant(map[string]any{"actionId": text, "actionType": map[string]any{"enum": []any{"script"}}, "scriptId": text}, "actionId", "actionType", "scriptId")
	actionVariant(map[string]any{"actionId": text, "actionType": map[string]any{"enum": []any{"script"}}, "executable": text}, "actionId", "actionType", "executable")
	actionVariant(map[string]any{"actionId": text, "actionType": map[string]any{"enum": []any{"http"}}, "allowedOrigins": stringsSchema, "methods": stringsSchema}, "actionId", "actionType", "allowedOrigins", "methods")
	actionVariant(map[string]any{"actionId": text, "actionType": map[string]any{"enum": []any{"builtin"}}, "handler": text}, "actionId", "actionType", "handler")
	executions["action"] = map[string]any{"oneOf": actionVariants}
	variants := []any{}
	for _, kind := range []string{"input", "result", "worker", "askUser", "action", "functionTask", "workflow", "startWorkflow", "joinWorkflow"} {
		executionName := "CanvasExecution" + strings.ToUpper(kind[:1]) + kind[1:]
		builder.definitions[executionName] = executions[kind]
		properties := map[string]any{}
		for key, value := range baseFields {
			properties[key] = value
		}
		properties["kind"] = map[string]any{"enum": []any{kind}}
		properties["execution"] = map[string]any{"$ref": "#/$defs/" + executionName}
		if kind != "worker" && kind != "action" && kind != "functionTask" {
			delete(properties, "successCondition")
		}
		variants = append(variants, closedCanvasObject(properties, "id", "kind", "label", "inputs", "execution"))
	}
	builder.definitions["CanvasNode"] = map[string]any{"oneOf": variants}
	control := builder.definitions["CanvasControl"].(map[string]any)["properties"].(map[string]any)
	control["kind"] = canvasStringEnum("call", "sequence", "decision", "parallel", "pipeline", "loop", "try")
	edge := builder.definitions["CanvasEdge"].(map[string]any)["properties"].(map[string]any)
	edge["kind"] = canvasStringEnum("control", "data")
	script := builder.definitions["ScriptSummary"].(map[string]any)["properties"].(map[string]any)
	script["language"] = map[string]any{"enum": []any{"python"}}
	script["protocol"] = canvasStringEnum("json", "text")
	return schema
}

// ValidateCanvas 检查版本、封闭节点实现、稳定引用及拓扑身份，不根据图执行任何操作。
func ValidateCanvas(manifest CanvasManifest) error {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return status_error.WorkflowStateInvalid.WithField("reason", "canvas is not serializable JSON")
	}
	var value any
	if json.Unmarshal(encoded, &value) != nil {
		return status_error.WorkflowStateInvalid
	}
	if err := ValidateJSON(CanvasManifestSchema(), value); err != nil {
		return err
	}
	nodes := map[string]bool{}
	phases := map[string]bool{}
	for _, phase := range manifest.Phases {
		if phase.ID == "" || phases[phase.ID] {
			return status_error.WorkflowStateInvalid.WithField("reason", "duplicate canvas phase identity")
		}
		phases[phase.ID] = true
	}
	for _, node := range manifest.Nodes {
		if node.ID == "" || nodes[node.ID] {
			return status_error.WorkflowStateInvalid.WithField("reason", "duplicate canvas node identity")
		}
		nodes[node.ID] = true
		if node.PhaseID != "" && !phases[node.PhaseID] {
			return status_error.WorkflowStateInvalid.WithField("reason", "unknown canvas phase reference")
		}
	}
	for _, node := range manifest.Nodes {
		for _, input := range node.Inputs {
			for _, dependency := range input.DependsOn {
				if !nodes[dependency] {
					return status_error.WorkflowStateInvalid.WithField("reason", "dangling canvas data dependency")
				}
			}
		}
	}
	edges := map[string]bool{}
	for _, edge := range manifest.Edges {
		if edge.ID == "" || edges[edge.ID] || !nodes[edge.From] || !nodes[edge.To] {
			return status_error.WorkflowStateInvalid.WithField("reason", "invalid canvas edge identity or endpoint")
		}
		edges[edge.ID] = true
	}
	controls := map[string]bool{}
	var validateControls func([]CanvasControl) bool
	validateControls = func(items []CanvasControl) bool {
		for _, item := range items {
			if item.ID == "" || controls[item.ID] || item.NodeID != "" && !nodes[item.NodeID] {
				return false
			}
			controls[item.ID] = true
			for _, branch := range item.Branches {
				if !validateControls(branch.Children) {
					return false
				}
			}
		}
		return true
	}
	if !validateControls(manifest.Controls) {
		return status_error.WorkflowStateInvalid.WithField("reason", "invalid canvas control identity or node reference")
	}
	return nil
}
