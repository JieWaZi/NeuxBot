package dsl

import (
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/google/jsonschema-go/jsonschema"
)

// ResolveSchema 校验受支持的 JSON Schema；引用只能留在当前文档内。
func ResolveSchema(value map[string]any) (*jsonschema.Resolved, error) {
	if len(value) == 0 {
		return nil, nil
	}
	if !schemaKeywords(value) {
		return nil, status_error.WorkflowSchemaInvalid
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, status_error.WorkflowSchemaInvalid
	}
	var schema jsonschema.Schema
	if json.Unmarshal(encoded, &schema) != nil {
		return nil, status_error.WorkflowSchemaInvalid
	}
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		return nil, status_error.WorkflowSchemaInvalid
	}
	return resolved, nil
}

func schemaKeywords(schema map[string]any) bool {
	if schema["format"] == constants.WorkflowFileFormat {
		validType := schema["type"] == "string"
		if types, ok := schema["type"].([]any); ok {
			validType = false
			for _, entry := range types {
				if entry == "string" {
					validType = true
				} else if entry != "null" {
					return false
				}
			}
		}
		if !validType {
			return false
		}
	}
	for key, value := range schema {
		switch key {
		case "$schema", "$id", "title", "description", "default", "examples", "type", "enum", "const", "required", "additionalProperties", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "minLength", "maxLength", "pattern", "format", "minItems", "maxItems", "uniqueItems", "minProperties", "maxProperties":
			if key == "$schema" && value != "https://json-schema.org/draft/2020-12/schema" {
				return false
			}
			if key == "additionalProperties" {
				if child, ok := value.(map[string]any); ok && !schemaKeywords(child) {
					return false
				}
			}
		case "$ref":
			reference, ok := value.(string)
			if !ok || !strings.HasPrefix(reference, "#/") {
				return false
			}
		case "x-ui-control":
			control, ok := value.(string)
			if !ok {
				return false
			}
			switch control {
			case "input", "textarea", "radio", "select", "switch", "checkbox", "checkboxes", constants.WorkflowFileFormat:
			default:
				return false
			}
		case "x-ui-labels":
			if !stringArray(value) {
				return false
			}
		case "properties", "$defs":
			fields, ok := value.(map[string]any)
			if !ok {
				return false
			}
			for _, field := range fields {
				child, ok := field.(map[string]any)
				if !ok || !schemaKeywords(child) {
					return false
				}
			}
		case "items", "not":
			child, ok := value.(map[string]any)
			if !ok || !schemaKeywords(child) {
				return false
			}
		case "oneOf", "anyOf", "allOf", "prefixItems":
			children, ok := value.([]any)
			if !ok {
				return false
			}
			for _, item := range children {
				child, ok := item.(map[string]any)
				if !ok || !schemaKeywords(child) {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

// ValidateJSON 返回可供 HTTP 和 MCP 共同使用的契约诊断。
func ValidateJSON(schema map[string]any, value any) error {
	resolved, err := ResolveSchema(schema)
	if err != nil {
		return err
	}
	if resolved != nil {
		if err := resolved.Validate(value); err != nil {
			location := newSchemaInspection(schema)
			pointer := location.failurePointer("", "", value, 0)
			return status_error.WorkflowSchemaInvalid.WithField("reason", err.Error()).WithField("jsonPointer", pointer)
		}
	}
	return nil
}

// ApplyDefaults 只填充缺失字段，保留明确 null；组合契约仅采用唯一合法分支的默认值。
func ApplyDefaults(schema map[string]any, value any) any {
	return newSchemaInspection(schema).defaults("", cloneJSON(value), 0)
}
