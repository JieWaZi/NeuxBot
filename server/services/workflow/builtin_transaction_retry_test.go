package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// busyBuiltinLogStore 在实际日志写入后模拟 busy，要求整个事务回滚重试。
type busyBuiltinLogStore struct {
	// WorkflowController 提供真实 SQLite 仓储。
	controllers.WorkflowController
	// injected 保证日志事务只失败一次。
	injected atomic.Bool
}

func (s *busyBuiltinLogStore) AppendEvent(ctx context.Context, event models.WorkflowEvent) (models.WorkflowEvent, error) {
	value, err := s.WorkflowController.AppendEvent(ctx, event)
	if err == nil && event.Type == constants.WorkflowLogEvent && s.injected.CompareAndSwap(false, true) {
		return value, status_error.DatabaseBusy
	}
	return value, err
}

// busyBuiltinReceiptRuns 在工具结果与帧写入后模拟 busy，验证回执不会重发工具。
type busyBuiltinReceiptRuns struct {
	// WorkflowRunController 提供真实 SQLite 运行仓储。
	controllers.WorkflowRunController
	// injected 保证工具回执事务只失败一次。
	injected atomic.Bool
}

func (s *busyBuiltinReceiptRuns) CommitRunState(ctx context.Context, state controllers.WorkflowRunState, expected, now int64) error {
	if err := s.WorkflowRunController.CommitRunState(ctx, state, expected, now); err != nil {
		return err
	}
	for _, call := range state.ActionCalls {
		if call.Status == constants.WorkflowStepStatusSucceeded && s.injected.CompareAndSwap(false, true) {
			return status_error.DatabaseBusy
		}
	}
	return nil
}

func TestBuiltinTransactionRetryPreservesFrameAndDoesNotRepeatTool(t *testing.T) {
	var calls atomic.Int64
	server := mcp.NewServer(&mcp.Implementation{Name: "workflow-builtin-retry", Version: "1"}, nil)
	mcp.AddTool[map[string]any, map[string]any](server, &mcp.Tool{Name: "echo", Description: "actual MCP receipt"}, func(_ context.Context, _ *mcp.CallToolRequest, input map[string]any) (*mcp.CallToolResult, map[string]any, error) {
		calls.Add(1)
		return nil, map[string]any{"value": input["value"]}, nil
	})
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	s, _ := openRecoveryWorkflow(t, filepath.Join(t.TempDir(), "workflow.db"))
	s.connectors = fixtureConnectors{connector: models.MCPConnector{ID: "fixture", Transport: constants.MCPTransportStreamableHTTP, URL: httpServer.URL, TimeoutMS: 30000}}
	source := `export const meta={sdk:'ally.workflow/1',title:'重试帧',description:'数据库回滚不重发工具',actions:[{id:'echo-action',label:'echo',description:'真实工具回执',risk:'external',type:'builtin',handler:'echo',connectorIds:['fixture']}],phases:[{id:'work',title:'执行',detail:'工具和日志',canExit:true}]};export const actions={echo:async(ctx,input)=>{const first=await ctx.callTool({callId:'first',connectorId:'fixture',toolName:'echo',input:{value:input}});ctx.log(first);const second=await ctx.callTool({callId:'second',connectorId:'fixture',toolName:'echo',input:{value:input}});return {first,second}}};return await action('echo-action',{id:'echo',label:'双调用',phase:'work',input:'actual'});`
	definition := savedTestWorkflow(t, s, source)
	ctx := context.Background()
	run, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 1)
	state, err := s.runs.LoadRunState(ctx, run.ID)
	if err != nil || len(state.Effects) != 1 {
		t.Fatalf("prepare effect: %v %#v", err, state.Effects)
	}
	store := &busyBuiltinLogStore{WorkflowController: s.store}
	runs := &busyBuiltinReceiptRuns{WorkflowRunController: s.runs}
	s.store, s.runs = store, runs
	owner := s.state.Owner
	s.state = workflowstate.New(store, runs, s.transactions)
	s.state.Owner = owner
	s.dispatchDeterministic(ctx, state.Run, state.Effects[0], false)
	result := runTicks(t, s, run.ID, 2)
	if result.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("frame did not recover after rollback: status=%s failure=%#v", result.Status, result.Failure)
	}
	output, _ := result.Result.(map[string]any)
	data, _ := output["data"].(map[string]any)
	for _, name := range []string{"first", "second"} {
		tool, _ := data[name].(map[string]any)
		structured, _ := tool["structuredContent"].(map[string]any)
		if structured["value"] != "actual" {
			t.Fatalf("retried frame lost returned value %s: %#v", name, result.Result)
		}
	}
	if !store.injected.Load() || !runs.injected.Load() || calls.Load() != 2 {
		t.Fatalf("retry coverage or external call count: log=%t receipt=%t calls=%d", store.injected.Load(), runs.injected.Load(), calls.Load())
	}
	state, err = s.runs.LoadRunState(ctx, run.ID)
	if err != nil || len(state.ActionCalls) != 2 {
		t.Fatalf("tool receipts: %v %#v", err, state.ActionCalls)
	}
	for _, call := range state.ActionCalls {
		if call.Status != constants.WorkflowStepStatusSucceeded || call.Result == nil {
			t.Fatalf("tool receipt lost: %#v", call)
		}
	}
	events, err := s.store.ListEvents(ctx, run.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	logs := 0
	for _, event := range events {
		if event.Type == constants.WorkflowLogEvent {
			logs++
		}
	}
	if logs != 1 {
		t.Fatalf("rolled-back log duplicated or lost: %d", logs)
	}
}
