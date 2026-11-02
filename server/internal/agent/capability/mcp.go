package capability

import "github.com/JieWaZi/NeuxBot/server/constants"

// MCPInputSchema 从唯一操作契约生成 MCP 参数，仅补充传输层幂等标识。
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
		properties["requestId"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "description": "Stable mutation identity. Reuse after uncertain outcomes; independent of JSON-RPC id."}
		required := append([]string{}, o.InputSchema["required"].([]string)...)
		out["required"] = append(required, "requestId")
	}
	return out
}

// MCPOutputSchema 只包装同源响应信封，不复制领域结果 Schema。
func (o Operation) MCPOutputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"protocolVersion": map[string]any{"type": "integer"}, "ok": map[string]any{"type": "boolean"}, "operation": map[string]any{"type": "string"}, "completion": map[string]any{"type": "string"}, "commandId": map[string]any{"type": "string"}, "replayed": map[string]any{"type": "boolean"}, "data": map[string]any{"anyOf": []any{o.OutputSchema, map[string]any{"type": "null"}}}, "error": map[string]any{"type": "object"},
	}, "required": []string{"protocolVersion", "ok", "operation", "replayed", "data"}, "additionalProperties": false}
}
