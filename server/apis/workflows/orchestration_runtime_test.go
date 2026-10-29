package workflows

import (
	workspace "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/workspace"

	catalog "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/catalog"

	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimehost"

	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// orchestrationDriver 仅替代外部模型，真实宿主与独立 Runtime 负责会话、事件和终态。
type orchestrationDriver struct {
	// prompt 在仍有效的原生会话内调用真实业务 CLI。
	prompt func(context.Context, c.OpenRequest, c.TurnRequest) error
	// opens 记录独立 Runtime 实际准备的原生会话。
	opens chan c.OpenRequest
	// calls 计数真实 Prompt，检查重放不会重复派发。
	calls atomic.Int32
}

// orchestrationSession 保存每个工作 Chat 各自的静态启动事实。
type orchestrationSession struct {
	// driver 是外部测试边界。
	driver *orchestrationDriver
	// request 是此原生会话的冻结配置。
	request c.OpenRequest
}

func (*orchestrationDriver) ID() string { return "codex" }
func (*orchestrationDriver) Capabilities() c.DriverCapabilities {
	return c.DriverCapabilities{Version: "orchestration-fixture", HotResume: true, ColdResume: true}
}
func (d *orchestrationDriver) Open(ctx context.Context, request c.OpenRequest) (c.DriverSession, error) {
	select {
	case d.opens <- request:
		return &orchestrationSession{driver: d, request: request}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (*orchestrationDriver) Close(context.Context) error  { return nil }
func (*orchestrationSession) Close(context.Context) error { return nil }
func (s *orchestrationSession) Prompt(ctx context.Context, turn c.TurnRequest, sink c.DriverSink) c.DriverResult {
	s.driver.calls.Add(1)
	failed := func(err error) c.DriverResult {
		return c.DriverResult{Status: c.StatusFailed, Fault: &c.Fault{Code: c.FaultDriver, Cause: err}}
	}
	if err := sink.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted}); err != nil {
		return failed(err)
	}
	if err := s.driver.prompt(ctx, s.request, turn); err != nil {
		return failed(err)
	}
	if err := sink.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &c.AssistantMessage{MessageID: "answer", Phase: c.PhaseFinal, Delta: "已提交", Complete: true}}); err != nil {
		return failed(err)
	}
	return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn, NativeSession: c.NativeSessionInfo{Handle: "native-" + s.request.Invocation.ID, Resumable: true}}
}

func TestWorkflowAuthoringAndWorkerUseIndependentRuntimeHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	directory := t.TempDir()
	source := `export const meta={sdk:'ally.workflow/1',title:'独立宿主',description:'通过真实CLI提交节点成果',nodeResultPageOmissions:{facts:'只返回本次执行的检查结果'},phases:[{id:'work',title:'执行',detail:'提交结果',canExit:true}]};return await worker('核对冻结版本',{id:'facts',label:'核对',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker'},schema:{type:'object',properties:{verified:{type:'boolean'}},required:['verified']},toolPolicy:{connectors:{mode:'allowlist',connectorIds:[]}}});`
	workerStarted := make(chan struct{}, 1)
	releaseWorker := make(chan struct{})
	outcomes := make(chan error, 2)
	driver := &orchestrationDriver{opens: make(chan c.OpenRequest, 2)}
	driver.prompt = func(ctx context.Context, open c.OpenRequest, turn c.TurnRequest) error {
		call := func(args []string, input map[string]any) (map[string]any, error) {
			return fixtureCLIEnvironment(ctx, open.Workspace.Root, open.Credential.Environment, args, input)
		}
		var err error
		if strings.Contains(turn.Input.Text, "创作宿主测试") {
			_, err = call([]string{"workflow", "source", "save", "--request-id", "driver-save"}, map[string]any{"source": source, "sdkVersion": constants.WorkflowSDKVersion, "expectedRevisionId": ""})
			if err == nil {
				if _, denied := call([]string{"workflow", "results", "schema"}, nil); denied == nil {
					err = status_error.WorkflowStateInvalid
				}
			}
		} else {
			var schema map[string]any
			schema, err = call([]string{"workflow", "results", "schema"}, nil)
			if err == nil && schema["nodeId"] != "facts" {
				err = status_error.WorkflowStateInvalid
			}
			if err == nil {
				workerStarted <- struct{}{}
				select {
				case <-releaseWorker:
				case <-ctx.Done():
					return ctx.Err()
				}
				_, err = call([]string{"workflow", "results", "submit", "--request-id", "driver-result"}, map[string]any{"data": map[string]any{"verified": true}})
			}
		}
		outcomes <- err
		return err
	}
	sdk, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatal(err)
	}
	host := runtimehost.NewChat(runtimehost.ChatConfig{PrepareWorkspace: workspace.New(sdk.Runtimes()).Prepare, Launch: sdk.PrepareLaunch, Runtimes: sdk.Runtimes, Fork: sdk.ForkSession,
		Driver: func(runtimehost.Environment, acpdriver.Factory) (c.Driver, error) { return driver, nil }})
	t.Cleanup(func() {
		if err := host.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	s, _ := openCoreWorkflowPorts(t, directory, host, fixtureEnvironment{})
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "runtime-authoring", CreationWakerID: "fixture-waker", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	defaults := models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelFullAccess}
	command := SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "runtime-message", Content: "创作宿主测试", Defaults: defaults}
	creation, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-outcomes:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// 创作授权跟随真实 Execution，节点授权不能被创作会话借用。
	for {
		state, err := s.Chats.GetChatSummary(ctx, creation.ChatID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Chat.ActiveExecutionID == "" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	replay, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if err != nil || replay.AcceptedExecutionID != creation.AcceptedExecutionID || driver.calls.Load() != 1 {
		t.Fatalf("创作重放重复派发: %+v calls=%d error=%v", replay, driver.calls.Load(), err)
	}
	definition, err = s.Store.GetDefinition(ctx, definition.ID)
	if err != nil || definition.CurrentRevisionID == "" {
		t.Fatalf("真实创作未提交源码: %+v %v", definition, err)
	}
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "runtime-worker", Defaults: defaults})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerStarted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pending, err := s.Runs.LoadRunState(ctx, run.ID)
	if err != nil || pending.Run.Status == constants.WorkflowRunStatusSucceeded {
		t.Fatalf("接纳被误判成成果完成: %+v %v", pending.Run, err)
	}
	newRevision, diagnostics, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, ExpectedRevisionID: definition.CurrentRevisionID, RequestID: "later-revision", Source: strings.Replace(source, "独立宿主", "后续版本", 1), SDKVersion: constants.WorkflowSDKVersion})
	if err != nil || newRevision.RevisionID == run.RevisionID {
		t.Fatalf("后续版本保存失败: %+v %v %v", newRevision, diagnostics, err)
	}
	close(releaseWorker)
	final := awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusSucceeded
	})
	result, _ := final.Run.Result.(map[string]any)
	if final.Run.RevisionID != run.RevisionID || result["verified"] != true || len(final.Effects) != 1 || final.Effects[0].Candidate == nil || driver.calls.Load() != 2 {
		t.Fatalf("冻结版本或真实成果终态丢失: %+v calls=%d", final, driver.calls.Load())
	}
	first, second := <-driver.opens, <-driver.opens
	canonicalDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	if first.Workspace.Root == second.Workspace.Root || len(first.Workspace.Directories) != 1 || len(second.Workspace.Directories) != 1 || first.Workspace.Directories[0] != canonicalDirectory || second.Workspace.Directories[0] != canonicalDirectory || first.Profile.Model != "fixture" || second.Profile.Model != "fixture" {
		t.Fatalf("创作与节点原生会话/批准范围错误: first=%+v second=%+v", first.Workspace, second.Workspace)
	}
	for _, executionID := range []struct {
		// chat 是业务创建的真实 Chat。
		chat string
		// id 是该 Chat 已接纳的真实 Execution。
		id string
	}{{creation.ChatID, creation.AcceptedExecutionID}, {final.Effects[0].ChatID, final.Effects[0].ExecutionID}} {
		actual, err := s.Chats.ReadExecution(ctx, executionID.chat, executionID.id)
		if err != nil || actual.Status != constants.ExecutionStatusCompleted || actual.CompletedAt == nil {
			t.Fatalf("Execution 持久终态缺失: %+v %v", actual, err)
		}
	}
}
