package capability

import "github.com/JieWaZi/NeuxBot/server/constants"

// RequestIDSchema publishes the idempotency identifier format actually accepted by Core commands and receipts.
func RequestIDSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": constants.AgentCLIRequestIDMaximumLength, "pattern": requestIDPattern.String(), "description": "Stable mutation identity. Reuse after uncertain outcomes; independent of JSON-RPC id."}
}

// MCPInputSchema derives MCP parameters from the sole operation contract, adding only transport idempotency identity.
func (o Operation) MCPInputSchema() map[string]any {
	out := map[string]any{}
	for key, value := range o.InputSchema {
		out[key] = value
	}
	properties := map[string]any{}
	for key, value := range o.InputSchema["properties"].(map[string]any) {
		properties[key] = value
	}
	out["properties"] = properties
	if o.Effect != constants.AgentCLIEffectRead {
		properties["requestId"] = RequestIDSchema()
		required := append([]string{}, o.InputSchema["required"].([]string)...)
		out["required"] = append(required, "requestId")
	}
	return out
}

// MCPOutputSchema wraps the response envelope from the same source without duplicating domain result schemas.
func (o Operation) MCPOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"protocolVersion": map[string]any{"type": "integer"}, "ok": map[string]any{"type": "boolean"}, "operation": map[string]any{"type": "string"}, "completion": map[string]any{"type": "string"}, "commandId": map[string]any{"type": "string"}, "replayed": map[string]any{"type": "boolean"}, "data": map[string]any{"anyOf": []any{o.OutputSchema, map[string]any{"type": "null"}}}, "error": map[string]any{"type": "object"},
	}, "required": []string{"protocolVersion", "ok", "operation", "replayed", "data"}, "additionalProperties": false}
}
