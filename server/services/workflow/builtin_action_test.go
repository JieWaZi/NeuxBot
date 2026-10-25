package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fixtureConnectors 仅提供已启用目录，调用仍经过实际 MCP HTTP 协议。
type fixtureConnectors struct {
	// MCPConnectorController 保留本场景不使用的仓储方法接口。
	controllers.MCPConnectorController
	// connector 是临时测试服务器的真实连接信息。
	connector models.MCPConnector
}

func (s fixtureConnectors) ListEnabled(context.Context, string) ([]models.MCPConnector, error) {
	return []models.MCPConnector{s.connector}, nil
}

func TestBuiltinRestartKeepsCompletedToolReceiptAndDoesNotResendUnknownCall(t *testing.T) {
	var calls atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseServer := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseServer)
	server := mcp.NewServer(&mcp.Implementation{Name: "workflow-builtin-acceptance", Version: "1"}, nil)
	mcp.AddTool[map[string]any, map[string]any](server, &mcp.Tool{Name: "echo", Description: "actual MCP receipt"}, func(ctx context.Context, _ *mcp.CallToolRequest, input map[string]any) (*mcp.CallToolResult, map[string]any, error) {
		if calls.Add(1) == 2 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return nil, map[string]any{"value": input["value"]}, nil
	})
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	connector := fixtureConnectors{connector: models.MCPConnector{ID: "fixture", Transport: constants.MCPTransportStreamableHTTP, URL: httpServer.URL, TimeoutMS: 30000}}
	databasePath := filepath.Join(t.TempDir(), "workflow.db")
	service, closeDB := openRecoveryWorkflow(t, databasePath)
	service.connectors = connector
	source := `export const meta={sdk:'ally.workflow/1',title:'工具回执',description:'恢复不重发',actions:[{id:'echo-action',label:'echo',description:'测试实际回执',risk:'external',type:'builtin',handler:'echo',connectorIds:['fixture']}],phases:[{id:'work',title:'执行',detail:'两个调用',canExit:true}]};export const actions={echo:async(ctx,input)=>{const first=await ctx.callTool({callId:'first',connectorId:'fixture',toolName:'echo',input:{value:input}});ctx.log(first);const second=await ctx.callTool({callId:'second',connectorId:'fixture',toolName:'echo',input:{value:input}});return {first,second}}};return await action('echo-action',{id:'echo',label:'双调用',phase:'work',input:'actual'});`
	definition := savedTestWorkflow(t, service, source)
	ctx := context.Background()
	run, err := service.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	waitWorkflow(t, service, run.ID, func(controllers.WorkflowRunState) bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})
	// 第二个真实调用已开始但没有完成回执；第一个调用的结果已经与检查点一起持久化。
	service.Close()
	releaseServer()
	closeDB()
	restored, _ := openRecoveryWorkflow(t, databasePath)
	restored.connectors = connector
	if err := restored.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	state := waitWorkflow(t, restored, run.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusFailed
	})
	if calls.Load() != 2 || len(state.ActionCalls) != 2 {
		t.Fatal("MCP effect resent or history lost", calls.Load(), len(state.ActionCalls))
	}
	for _, call := range state.ActionCalls {
		if call.CallID == "first" && (call.Status != constants.WorkflowStepStatusSucceeded || call.Result == nil) {
			t.Fatal("completed receipt lost", call.Status)
		}
		if call.CallID == "second" && call.Status != constants.WorkflowStepStatusUnknown {
			t.Fatal("unknown call automatically accepted or retried", call.Status)
		}
	}
}
