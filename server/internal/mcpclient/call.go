package mcpclient

import (
	"context"
	"encoding/json"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Call 在既有获准连接器的真实传输上调用工具，生命周期由调用方 context 拥有。
func Call(ctx context.Context, connector models.MCPConnector, tool string, input any) (any, error) {
	timeout := time.Duration(connector.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	child, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	transport, err := newTransport(child, connector, timeout)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "ally", Version: "1"}, nil)
	session, err := client.Connect(child, transport, nil)
	if err != nil {
		logx.Errorf("connect action MCP failed: connector_id=%s error=%v", connector.ID, err)
		return nil, status_error.WorkflowEffectFailed
	}
	defer session.Close()
	result, err := session.CallTool(child, &mcp.CallToolParams{Name: tool, Arguments: input})
	if err != nil {
		logx.Errorf("call action MCP failed: connector_id=%s tool=%s error=%v", connector.ID, tool, err)
		return nil, status_error.WorkflowEffectFailed
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 1<<20 {
		return nil, status_error.WorkflowBudgetExceeded
	}
	var value any
	if err = json.Unmarshal(encoded, &value); err != nil {
		logx.Errorf("decode action MCP result failed: error=%v", err)
		return nil, status_error.WorkflowEffectFailed
	}
	if result.IsError {
		return value, status_error.WorkflowEffectFailed.WithField("code", "TOOL_FAILED")
	}
	return value, nil
}
