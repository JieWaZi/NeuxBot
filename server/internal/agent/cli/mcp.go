package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// mcpWriter 将命令注入的输出流交给协议传输，不关闭宿主拥有的流。
type mcpWriter struct {
	// Writer 是只承载 MCP 帧的输出流。
	io.Writer
}

func (mcpWriter) Close() error { return nil }

func (r *runner) mcpCommand() *cobra.Command {
	root := &cobra.Command{Use: "mcp", Short: "Expose the current execution's capabilities over MCP"}
	root.AddCommand(&cobra.Command{Use: "serve", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		r.entered = true
		server, err := newMCPServer(cmd.Context(), r.streams.ContextFile)
		if err != nil {
			return err
		}
		reader, ok := r.streams.In.(io.ReadCloser)
		if !ok {
			reader = io.NopCloser(r.streams.In)
		}
		if err := server.Run(cmd.Context(), &mcp.IOTransport{Reader: reader, Writer: mcpWriter{r.streams.Out}}); err != nil && cmd.Context().Err() == nil {
			return status_error.AgentConnectionUnavailable
		}
		return nil
	}})
	return root
}

// newMCPServer 固定会话入口和工具目录；每次调用只读取一次当前轮授权，不在失败后换凭据重试。
func newMCPServer(ctx context.Context, path string) (*mcp.Server, error) {
	connection, err := capability.ReadConnection(path)
	if err != nil {
		return nil, err
	}
	bound := client{connection: connection}
	response, err := bound.request(ctx, http.MethodGet, "/capabilities", nil, false, 5*time.Second)
	if err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, status_error.AgentContextUnauthorized
	}
	var available struct {
		// Operations 是 Core 对当前 Grant 冻结的可调用身份。
		Operations []capability.Operation `json:"operations"`
	}
	encoded, err := json.Marshal(response.Data)
	if err != nil || json.Unmarshal(encoded, &available) != nil {
		return nil, status_error.AgentConnectionUnavailable
	}
	// 原生热会话保留桥进程；同一路径轮转 Grant，但不能跨 Core 或入口接管授权。
	requestCurrent := func(ctx context.Context, method, route string, payload any, mutation bool, timeout time.Duration) (capability.Response, error) {
		current, err := capability.ReadConnection(path)
		if err != nil {
			return capability.Response{}, err
		}
		if current.Endpoint != connection.Endpoint || current.CoreEpoch != connection.CoreEpoch {
			return capability.Response{}, status_error.AgentContextUnauthorized
		}
		return (client{connection: current}).request(ctx, method, route, payload, mutation, timeout)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "NeuxBot", Version: constants.ApplicationVersion}, nil)
	for _, entry := range available.Operations {
		op, ok := capability.Find(entry.ID)
		if !ok {
			return nil, status_error.AgentConnectionUnavailable
		}
		server.AddTool(&mcp.Tool{Name: strings.ReplaceAll(op.ID, ".", "_"), Description: op.Summary, InputSchema: op.MCPInputSchema(), OutputSchema: op.MCPOutputSchema(), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: op.Effect == constants.AgentCLIEffectRead}}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			params := map[string]any{}
			raw := request.Params.Arguments
			if len(raw) == 0 {
				raw = json.RawMessage(`{}`)
			}
			if json.Unmarshal(raw, &params) != nil || params == nil {
				return mcpResponse(capability.ErrorResponse(op.ID, status_error.AgentInputInvalid)), nil
			}
			requestID, _ := params["requestId"].(string)
			mutation := op.Effect != constants.AgentCLIEffectRead
			if mutation {
				delete(params, "requestId")
				if !capability.ValidRequestID(requestID) {
					return mcpResponse(capability.ErrorResponse(op.ID, status_error.AgentInputInvalid)), nil
				}
			}
			if err := op.Validate(params); err != nil {
				return mcpResponse(capability.ErrorResponse(op.ID, err)), nil
			}
			input := capability.Request{Operation: op.ID, RequestID: requestID, Params: params}
			if op.ID == "workflow.runs.start" {
				cwd, err := os.Getwd()
				if err != nil {
					return mcpResponse(capability.ErrorResponse(op.ID, status_error.AgentInputInvalid)), nil
				}
				input.CWD = cwd
			}
			result, err := requestCurrent(ctx, http.MethodPost, "/commands", input, mutation, 65*time.Second)
			if err != nil {
				result = capability.ErrorResponse(op.ID, err)
			}
			// Core 的前置授权校验可能尚未解析命令；桥接已知工具身份，不改写原错误与效果状态。
			if result.Operation == "" {
				result.Operation = op.ID
			}
			return mcpResponse(result), nil
		})
	}
	// 发现和回执只是原 Core 读取端口，不建立新的业务操作或授权。
	for _, entry := range []struct {
		// name 是稳定协议工具名。
		name string
		// description 是工具用途。
		description string
		// schema 是传输读取参数。
		schema map[string]any
		// path 将已经验证的参数映射到唯一 Core 读取入口。
		path func(map[string]any) string
	}{
		{"capabilities", "List operations authorized for this execution.", map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, func(map[string]any) string { return "/capabilities" }},
		{"guide", "Read one authorized task guide.", map[string]any{"type": "object", "properties": map[string]any{"guideId": map[string]any{"type": "string", "minLength": 1}}, "required": []string{"guideId"}, "additionalProperties": false}, func(p map[string]any) string { return "/guides/" + url.PathEscape(p["guideId"].(string)) }},
		{"receipts_get", "Query the original mutation receipt; never start a replacement operation after a timeout.", map[string]any{"type": "object", "properties": map[string]any{"operation": map[string]any{"type": "string", "minLength": 1}, "requestId": capability.RequestIDSchema()}, "required": []string{"operation", "requestId"}, "additionalProperties": false}, func(p map[string]any) string { return receiptPath(p["operation"].(string), p["requestId"].(string)) }},
	} {
		server.AddTool(&mcp.Tool{Name: entry.name, Description: entry.description, InputSchema: entry.schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			params := map[string]any{}
			raw := request.Params.Arguments
			if len(raw) == 0 {
				raw = json.RawMessage(`{}`)
			}
			op := capability.Operation{InputSchema: entry.schema}
			if json.Unmarshal(raw, &params) != nil || params == nil || op.Validate(params) != nil {
				return mcpResponse(capability.ErrorResponse(entry.name, status_error.AgentInputInvalid)), nil
			}
			result, err := requestCurrent(ctx, http.MethodGet, entry.path(params), nil, false, 35*time.Second)
			if err != nil {
				result = capability.ErrorResponse(entry.name, err)
			}
			return mcpResponse(result), nil
		})
	}
	return server, nil
}

func mcpResponse(response capability.Response) *mcp.CallToolResult {
	data, _ := json.Marshal(response)
	return &mcp.CallToolResult{StructuredContent: response, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}, IsError: !response.OK}
}
