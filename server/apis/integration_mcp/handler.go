// Package integration_mcp 将同源知识命令适配为无状态 Streamable HTTP MCP，不拥有知识或授权事实。
package integration_mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// principalKey 只携带边界认证产生的受信身份。
type principalKey struct{}

// NewHandler 复用唯一发布 Key 和知识服务，协议方法与会话语义交由官方 SDK。
func NewHandler(knowledge *knowledgeservice.Service, authority *integrationservice.Service) http.Handler {
	server := sdk.NewServer(&sdk.Implementation{Name: "NeuxBot Knowledge", Version: "1"}, nil)
	for _, op := range knowledgeOperations() {
		schema := toolSchema(op)
		server.AddTool(&sdk.Tool{Name: strings.ReplaceAll(op.ID, ".", "_"), Description: op.Summary, InputSchema: schema, OutputSchema: responseSchema(op), Annotations: &sdk.ToolAnnotations{ReadOnlyHint: op.Effect == constants.AgentCLIEffectRead}}, func(ctx context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			params := map[string]any{}
			arguments := request.Params.Arguments
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			if json.Unmarshal(arguments, &params) != nil || params == nil {
				return toolResult(toolFailure(op, status_error.AgentInputInvalid)), nil
			}
			requestID, _ := params["requestId"].(string)
			if op.Effect != constants.AgentCLIEffectRead {
				delete(params, "requestId")
			}
			if op.Effect != constants.AgentCLIEffectRead && (requestID == "" || len(requestID) > 200 || strings.TrimSpace(requestID) != requestID) {
				return toolResult(toolFailure(op, status_error.AgentInputInvalid)), nil
			}
			if err := op.Validate(params); err != nil {
				return toolResult(toolFailure(op, err)), nil
			}
			identity, ok := ctx.Value(principalKey{}).(models.IntegrationIdentity)
			if !ok {
				return toolResult(toolFailure(op, status_error.IntegrationUnauthorized)), nil
			}
			bindings, err := knowledge.PublishedBindings(ctx, authority, identity)
			if err != nil {
				return toolResult(toolFailure(op, err)), nil
			}
			for _, binding := range bindings {
				if binding.Operation != op.ID {
					continue
				}
				result, err := binding.Call(ctx, agentcapability.Request{Operation: op.ID, RequestID: requestID, Params: params})
				if err != nil {
					return toolResult(toolFailure(op, err)), nil
				}
				return toolResult(agentcapability.Response{ProtocolVersion: constants.AgentCLIProtocolVersion, OK: true, Operation: op.ID, Completion: op.Completion, Result: result}), nil
			}
			return toolResult(toolFailure(op, status_error.IntegrationForbidden)), nil
		})
	}
	transport := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		reject := func(err error) {
			status := http.StatusInternalServerError
			var coded interface{ StatusCode() int }
			if errors.As(err, &coded) {
				status = coded.StatusCode()
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			var keyed interface{ Key() string }
			code := status_error.IntegrationForbidden.Key()
			if errors.As(err, &keyed) {
				code = keyed.Key()
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"errorCode": code})
		}
		if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			reject(status_error.IntegrationForbidden)
			return
		}
		if knowledge == nil || authority == nil {
			reject(status_error.KnowledgeUnsupported)
			return
		}
		if len(r.Header.Values("X-API-Key")) != 1 || r.Header.Get("Authorization") != "" {
			reject(status_error.IntegrationUnauthorized)
			return
		}
		identity, err := authority.Authenticate(r.Context(), r.Header.Get("X-API-Key"))
		if err != nil {
			reject(err)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, constants.KnowledgeMCPMaxRequestBytes)
		transport.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, identity)))
	})
}

// toolSchema 只为传输信封补充 requestId；所有业务字段仍引用唯一命令合同。
func toolSchema(op contract.Operation) map[string]any {
	out := map[string]any{}
	for key, value := range op.InputSchema {
		out[key] = value
	}
	properties := map[string]any{}
	for key, value := range op.InputSchema["properties"].(map[string]any) {
		properties[key] = value
	}
	out["properties"] = properties
	if op.Effect != constants.AgentCLIEffectRead {
		properties["requestId"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "description": "Stable write identity; reuse for retries. Independent of JSON-RPC id."}
		required := append([]string{}, op.InputSchema["required"].([]string)...)
		out["required"] = append(required, "requestId")
	}
	return out
}

func toolResult(value agentcapability.Response) *sdk.CallToolResult {
	raw, _ := json.Marshal(value)
	return &sdk.CallToolResult{StructuredContent: value, Content: []sdk.Content{&sdk.TextContent{Text: string(raw)}}, IsError: !value.OK}
}

// responseSchema 仅包装同源输出到传输信封，业务 Data 的字段保持唯一合同。
func responseSchema(op contract.Operation) map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"protocolVersion": map[string]any{"type": "integer"}, "ok": map[string]any{"type": "boolean"},
		"operation": map[string]any{"type": "string"}, "completion": map[string]any{"type": "string"},
		"commandId": map[string]any{"type": "string"}, "replayed": map[string]any{"type": "boolean"},
		"data":  map[string]any{"anyOf": []any{op.OutputSchema, map[string]any{"type": "null"}}},
		"error": map[string]any{"type": "object"},
	}, "required": []string{"protocolVersion", "ok", "operation", "replayed", "data"}, "additionalProperties": false}
}

func toolFailure(op contract.Operation, err error) agentcapability.Response {
	value := agentcapability.ErrorResponse(op.ID, err)
	if errors.Is(err, status_error.AgentInputInvalid) {
		value.Error.Details = map[string]any{"toolName": strings.ReplaceAll(op.ID, ".", "_"), "inputSchema": toolSchema(op)}
		value.Error.RetryAdvice = "inspect_tools_list"
	}
	return value
}

// knowledgeOperations 从同源命令目录发现全部知识工具，不维护第二份操作名单。
func knowledgeOperations() []contract.Operation {
	out := []contract.Operation{}
	for _, op := range contract.Operations() {
		if strings.HasPrefix(op.ID, "knowledge.") {
			out = append(out, op)
		}
	}
	return out
}
