package workflows

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// fixtureRuntime 只替代模型协议边界，仍经过完整 Core、JSONL、MCP、SQLite 与真实文件。
type fixtureRuntime struct {
	// service 提供当前进程的真实效果查询。
	service *workflowFixture
	// mutex 保护并行 Runtime 收到的身份与顺序。
	mutex sync.Mutex
	// calls 保存节点实际调用次数。
	calls map[string]int
	// workspaces 验证不同 Chat 使用独立 cwd 并共用批准的业务目录。
	workspaces []runtimes.Workspace
}

func (*fixtureRuntime) ForkSession(context.Context, runtimes.ForkSessionRequest) (runtimes.ForkSessionResult, error) {
	return runtimes.ForkSessionResult{}, status_error.WorkflowCapabilityUnsupported
}
func (*fixtureRuntime) Runtimes() []runtimes.RuntimeSummary {
	return []runtimes.RuntimeSummary{{ID: "codex", BaseRuntimeID: "codex", InstructionFileName: "AGENTS.md", SkillDirectoryName: ".agents"}}
}
func (r *fixtureRuntime) Run(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink) runtimes.Result {
	invocation := util.New()
	started, _ := json.Marshal(runtimes.InvocationStartedData{InvocationID: invocation})
	failed := func(err error) runtimes.Result {
		return runtimes.Result{ExitCode: 1, Failure: &runtimes.Failure{Code: runtimes.FailureCodeProcessExited, Stage: "runtime", Message: err.Error(), Transience: runtimes.FailureTransiencePermanent}}
	}
	if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeInvocationStarted, Data: started}); err != nil {
		return failed(err)
	}
	contract, err := fixtureCLI(ctx, request, []string{"workflow", "results", "schema"}, nil)
	if err != nil {
		return failed(err)
	}
	nodeID, _ := contract["nodeId"].(string)
	r.mutex.Lock()
	r.calls[nodeID]++
	count := r.calls[nodeID]
	r.workspaces = append(r.workspaces, request.Workspace)
	r.mutex.Unlock()
	markdown := "## 本周进展\n- 完成导出\n## 风险与阻塞\n- 依赖待确认\n## 下周计划\n- 上线灰度\n"
	draft := map[string]any{"title": "周报", "period": "2026-09-28~2026-10-04", "oneLiner": "完成导出", "progress": []string{"完成导出"}, "risks": []any{map[string]any{"text": "依赖待确认", "impact": "拖慢灰度"}}, "nextWeek": []string{"上线灰度"}, "markdown": markdown, "omitted": []string{}}
	var data any
	switch nodeID {
	case "facts":
		data = map[string]any{"facts": []any{map[string]any{"kind": "progress", "statement": "完成导出", "evidence": "stated"}}, "periodGuess": "2026-09-28~2026-10-04", "overallSummary": "完成导出", "ambiguous": []string{}}
	case "background":
		data = map[string]any{"method": "notes-derived", "items": []any{}}
	case "draft-report":
		data = draft
	case "critique":
		data = map[string]any{"coverageGaps": []string{}, "wordingIssues": []string{}, "mustFix": []string{}, "verdict": "pass"}
	case "revise-report":
		if count == 1 {
			return failed(status_error.WorkflowEffectFailed)
		}
		data = draft
	case "write-report":
		path := filepath.Join(request.Workspace.AdditionalWritableDirectories[0], "reports", "周报-个人-2026-09-28~2026-10-04.md")
		if !strings.Contains(request.Message.Text, request.Workspace.AdditionalWritableDirectories[0]) {
			return failed(status_error.WorkflowStateInvalid.WithField("path", "write prompt omitted the approved business directory"))
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return failed(err)
		}
		if err := os.WriteFile(path, []byte(markdown), 0600); err != nil {
			return failed(err)
		}
		data = map[string]any{"reportPath": path, "written": true, "bytes": len([]byte(markdown))}
	case "verify-report":
		path := filepath.Join(request.Workspace.AdditionalWritableDirectories[0], "reports", "周报-个人-2026-09-28~2026-10-04.md")
		if !strings.Contains(request.Message.Text, path) {
			return failed(status_error.WorkflowStateInvalid.WithField("path", "read prompt omitted the shared report path"))
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return failed(err)
		}
		data = map[string]any{"exists": true, "hasAllSections": strings.Contains(string(content), "## 下周计划"), "ok": string(content) == markdown, "issues": []string{}}
	default:
		return failed(status_error.WorkflowStateInvalid.WithField("node", nodeID))
	}
	if _, err := fixtureCLI(ctx, request, []string{"workflow", "results", "submit", "--request-id", "result"}, map[string]any{"data": data}); err != nil {
		return failed(err)
	}

	return runtimes.Result{Facts: runtimes.InvocationFacts{InvocationID: invocation, RuntimeID: "codex", BaseRuntimeID: "codex"}, ProtocolHealthy: true}
}

// fixtureEnvironment 返回真实 Core 会校验的固定能力目录，不替代接纳逻辑。
type fixtureEnvironment struct{}

func (fixtureEnvironment) Get(context.Context, string) (models.RuntimeEnvironment, error) {
	// Runtime.Run 已由测试替身接管，仅用测试进程满足 Core 的可执行路径校验。
	binary, err := os.Executable()
	if err != nil {
		return models.RuntimeEnvironment{}, err
	}
	return models.RuntimeEnvironment{RuntimeID: "codex", Enabled: true, Binary: binary, DefaultModel: "fixture", Detection: runtimes.DetectionResult{Runtime: runtimes.RuntimeSummary{ID: "codex", BaseRuntimeID: "codex"}, State: runtimes.DetectionStateReady, ResolvedBinary: binary, Capabilities: runtimes.CapabilitySet{runtimes.CapabilityMCPStreamableHTTP: true}, Models: []runtimes.ModelOption{{ID: "fixture", Name: "fixture", PermissionLevels: []runtimes.PermissionLevel{runtimes.PermissionLevelDefault, runtimes.PermissionLevelFullAccess}}}}}, nil
}

func openCoreWorkflowFixture(t *testing.T, directory string, runtime *fixtureRuntime) (*workflowFixture, func()) {
	s, close := openCoreWorkflowPorts(t, directory, runtime, fixtureEnvironment{})
	runtime.service = s
	return s, close
}

// coreTestRuntime 仅抽象模型运行边界，真实 smoke 使用生产 SDK。
type coreTestRuntime interface {
	// ForkSession 复用实际会话分叉协议。
	ForkSession(context.Context, runtimes.ForkSessionRequest) (runtimes.ForkSessionResult, error)
	// Runtimes 提供真实工作区集成目录约定。
	Runtimes() []runtimes.RuntimeSummary
	// Run 通过协议交付模型执行事件。
	Run(context.Context, runtimes.RunRequest, runtimes.EventSink) runtimes.Result
}

// coreTestEnvironment 是运行配置的唯一探测查询接缝。
type coreTestEnvironment interface {
	// Get 返回已探测环境，而不绕过 Core 校验。
	Get(context.Context, string) (models.RuntimeEnvironment, error)
}

func openCoreWorkflowPorts(t *testing.T, directory string, runtime coreTestRuntime, environment coreTestEnvironment) (*workflowFixture, func()) {
	t.Helper()
	ctx := context.Background()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(directory, "fixture.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	wakers := controllers.NewWakerController(database.DB)
	profiles := controllers.NewProfileDocumentController(database.DB)
	projects := controllers.NewProjectController(database.DB)
	if _, err := wakers.Get(ctx, "fixture-waker"); err != nil {
		if err := wakers.Create(ctx, models.Waker{ID: "fixture-waker", Name: "验收伙伴"}); err != nil {
			t.Fatal(err)
		}
		documents := []models.WakerDocument{}
		for _, kind := range []constants.ProfileDocumentKind{constants.ProfileDocumentKindIdentity, constants.ProfileDocumentKindPersona, constants.ProfileDocumentKindPlaybook, constants.ProfileDocumentKindMemory} {
			documents = append(documents, models.WakerDocument{ID: util.New(), WakerID: "fixture-waker", Kind: kind, Content: "验收配置"})
		}
		if err := profiles.CreateWakerDocuments(ctx, documents); err != nil {
			t.Fatal(err)
		}
	}
	transactions := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	hub := events.New()
	registry := agentcapability.New(builtinskills.Guide)
	endpoint := httptest.NewServer(registry)
	skills := controllers.NewSkillController(database.DB)
	installer := builtinskills.Installer{DataDirectory: directory, Skills: skills, Transactions: transactions}
	if err := installer.EnsureCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	preset, _ := builtinrolepreset.DefinitionByID(builtinrolepreset.PersonalAssistantRolePresetID)
	if err := builtinskills.EnableForWaker(
		ctx,
		skills,
		preset.DefaultWaker.ID,
		preset.DefaultSkills,
	); err != nil {
		t.Fatal(err)
	}
	chats, err := chatservice.New(chatservice.Config{StorageDirectory: directory, Projects: projects, QueryIndexes: chatservice.QueryIndexDependencies{Chats: controllers.NewChatController(database.DB), RuntimeSessions: controllers.NewRuntimeSessionController(database.DB), DeletionIntents: controllers.NewChatDeletionIntentController(database.DB), Transactions: transactions}})
	if err != nil {
		t.Fatal(err)
	}
	if err := chats.Start(ctx); err != nil {
		t.Fatal(err)
	}
	core, err := execution.New(execution.Config{
		Runtime: runtime, Journal: chats, DormantChats: chats,
		Agent: registry, AgentEndpoint: endpoint.URL + constants.AgentCLIPath, AgentBinary: fixtureCLIBinary, Events: hub,
		Wakers: wakers, Profiles: profiles, Projects: projects,
		Skills: skills, SkillDataDirectory: directory,
		RuntimeEnvironments: environment, StorageDirectory: directory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Open(ctx); err != nil {
		t.Fatal(err)
	}
	s := workflowTestNew(workflow.Config{Commands: controllers.NewAgentCommandController(database.DB), Events: hub, Store: controllers.NewWorkflowController(database.DB), Runs: controllers.NewWorkflowRunController(database.DB), CreationSessions: controllers.NewWorkflowCreationController(database.DB), Transactions: transactions, Executions: core, Chats: chats, Worker: workflow.CoreWorkerAdapter{Executions: core, Chats: chats}, Wakers: wakers, Projects: projects, DataDirectory: directory})
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	close := func() {
		once.Do(func() {
			s.Close()
			core.Close()
			registry.Close()
			endpoint.Close()
			hub.Close()
			chats.Close()
			if err := connection.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(close)
	return s, close
}

func awaitFixtureState(t *testing.T, s *workflowFixture, id string, ready func(controllers.WorkflowRunState) bool) controllers.WorkflowRunState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		state, err := s.Runs.LoadRunState(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if ready(state) {
			return state
		}
		if workflowstate.TerminalRun(state.Run.Status) {
			failures := []any{}
			for _, effect := range state.Effects {
				if effect.Failure != nil {
					failures = append(failures, map[string]any{"node": effect.Boundary.NodeID, "failure": effect.Failure.Data})
				}
			}
			t.Fatalf("unexpected final state: status=%s failure=%+v effects=%+v", state.Run.Status, state.Run.Failure, failures)
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, _ := s.Runs.LoadRunState(context.Background(), id)
	frames := []map[string]any{}
	for _, fiber := range state.Fibers {
		failure := map[string]any{}
		if fiber.State.Failure != nil {
			if data, ok := fiber.State.Failure.Data.(map[string]any); ok {
				for _, field := range []string{"code", "kind", "reason", "operation"} {
					if value, found := data[field]; found {
						failure[field] = value
					}
				}
			}
		}
		frames = append(frames, map[string]any{"path": fiber.Path, "stage": fiber.StageIndex, "status": fiber.Status, "program": fiber.State.ProgramID, "pc": fiber.State.PC, "failure": failure})
	}
	t.Fatalf("fixture timed out: runStatus=%s frames=%v", state.Run.Status, frames)
	return state
}

func TestF1WholeCoreWeeklyRestartFailedRevisionThenActualWriteRead(t *testing.T) {
	directory := t.TempDir()
	runtime := &fixtureRuntime{calls: map[string]int{}}
	s, close := openCoreWorkflowFixture(t, directory, runtime)
	ctx := context.Background()
	source, err := os.ReadFile("../../internal/workflow/dsl/testdata/weekly-report.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "weekly", Title: "周报", CreationWakerID: "fixture-waker", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	if _, diagnostics, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save", Source: string(source), SDKVersion: constants.WorkflowSDKVersion}); err != nil {
		t.Fatal(err, diagnostics)
	}
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "run", Args: map[string]any{"weekNotes": "完成导出；依赖待确认；下周灰度"}, Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal(err)
	}
	waiting := func(state controllers.WorkflowRunState) bool {
		for _, human := range state.Humans {
			if human.Status == constants.WorkflowStepStatusWaiting {
				return true
			}
		}
		return false
	}
	state := awaitFixtureState(t, s, run.ID, waiting)
	pendingHuman := func(state controllers.WorkflowRunState) models.WorkflowHumanRequest {
		for _, human := range state.Humans {
			if human.Status == constants.WorkflowStepStatusWaiting {
				return human
			}
		}
		return models.WorkflowHumanRequest{}
	}
	originalHuman := pendingHuman(state)
	close()
	s, _ = openCoreWorkflowFixture(t, directory, runtime)
	state = awaitFixtureState(t, s, run.ID, waiting)
	if pendingHuman(state).ID != originalHuman.ID {
		t.Fatal("restart replaced pending approval")
	}
	answer := func(value string) {
		t.Helper()
		for _, human := range state.Humans {
			if human.Status == constants.WorkflowStepStatusWaiting {
				if err := answerHuman(ctx, workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: human.ID, RequestID: util.New(), Value: value}); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatal("no pending human")
	}
	answer("补充灰度指标")
	state = awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return len(state.Humans) >= 2 && waiting(state) })
	if !strings.Contains(pendingHuman(state).Prompt, "## 本周进展") {
		t.Fatal("failed revision lost previous draft")
	}
	answer("完善计划")
	state = awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return len(state.Humans) >= 3 && waiting(state) })
	answer("approve")
	state = awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	result, _ := state.Run.Result.(map[string]any)
	if state.Run.Status != constants.WorkflowRunStatusSucceeded || result["verified"] != true || result["reviewRounds"] != float64(3) {
		t.Fatalf("actual final receipt: %+v", state.Run)
	}
	path, _ := result["reportPath"].(string)
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != result["report"] {
		t.Fatal("file result not verified", err)
	}
	runtime.mutex.Lock()
	defer runtime.mutex.Unlock()
	if runtime.calls["revise-report"] != 2 || runtime.calls["write-report"] != 1 || runtime.calls["verify-report"] != 1 {
		t.Fatal("unexpected actual calls", runtime.calls)
	}
	workspaces := map[string]bool{}
	for _, workspace := range runtime.workspaces {
		assertWorkflowChatWorkspace(t, workspace, state.Run.ScopeBinding.WorkingDirectory)
		workspaces[workspace.Path] = true
	}
	if len(workspaces) != len(runtime.workspaces) {
		t.Fatal("workers reused runtime cwd")
	}
	for _, effect := range state.Effects {
		if effect.Boundary.NodeID == "revise-report" && effect.Status == constants.WorkflowStepStatusFailed && effect.Failure == nil {
			t.Fatal("failed revision evidence missing")
		}
	}
}

func assertWorkflowChatWorkspace(t *testing.T, workspace runtimes.Workspace, businessDirectory string) {
	t.Helper()
	if !workspace.Managed || workspace.Path == businessDirectory || len(workspace.AdditionalWritableDirectories) != 1 || workspace.AdditionalWritableDirectories[0] != businessDirectory {
		t.Fatalf("workflow must use the common managed chat cwd and authorize its business directory: %+v", workspace)
	}
	for _, project := range workspace.ProjectDirectories {
		if !project.Default {
			continue
		}
		actual, err := filepath.EvalSymlinks(filepath.Join(workspace.Path, project.Path))
		expected, expectedErr := filepath.EvalSymlinks(businessDirectory)
		if err != nil || expectedErr != nil || actual != expected {
			t.Fatalf("workflow business directory is not reachable from chat cwd: %q %v", actual, err)
		}
		return
	}
	t.Fatal("workflow chat has no default project source")
}

func TestWorkerSystemControlReferenceRunsThroughCoreWithoutMissingConnector(t *testing.T) {
	directory := t.TempDir()
	runtime := &fixtureRuntime{calls: map[string]int{}}
	s, _ := openCoreWorkflowFixture(t, directory, runtime)
	ctx := context.Background()
	source := `export const meta={sdk:'ally.workflow/1',title:'内部能力',description:'系统结果提交不属于业务工具白名单',nodeResultPageOmissions:{facts:'只返回 MCP 能力白名单测试的提交载荷'},phases:[{id:'work',title:'执行',detail:'提交结果',canExit:true}]};return await worker('整理',{id:'facts',label:'整理',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker'},schema:{type:'object'},toolPolicy:{connectors:{mode:'allowlist',connectorIds:[]}}});`
	definition := savedTestWorkflow(t, s, source)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "system-reference", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal("system capability blocked run admission", err)
	}
	state := awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusSucceeded
	})
	if len(state.Effects) != 1 || state.Effects[0].ChatID == "" || state.Effects[0].Candidate == nil {
		t.Fatal("worker did not create a chat and submit its real result", state)
	}
	revision, err := s.Store.GetRevision(ctx, definition.ID, run.RevisionID)
	if err != nil || revision.Source != source {
		t.Fatal("immutable source changed", err)
	}
	runtime.mutex.Lock()
	calls := runtime.calls["facts"]
	runtime.mutex.Unlock()
	if calls != 1 {
		t.Fatal("worker was not dispatched exactly once", calls)
	}
}
