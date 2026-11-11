package workflows

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/agentfixture"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// fixtureRuntime 生成领域 worker 结果并执行测试中的实际文件操作。
type fixtureRuntime struct {
	// mutex 保护并行 Runtime 收到的身份与顺序。
	mutex sync.Mutex
	// calls 保存节点实际调用次数。
	calls map[string]int
}

func (r *fixtureRuntime) Run(ctx context.Context, request fixtureInvocation) (*string, error) {
	contract, err := request.callResult(ctx, "workflow.results.schema", nil)
	if err != nil {
		return nil, err
	}
	nodeID, _ := contract["nodeId"].(string)
	r.mutex.Lock()
	r.calls[nodeID]++
	count := r.calls[nodeID]
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
			return nil, status_error.WorkflowEffectFailed
		}
		data = draft
	case "write-report":
		path := filepath.Join(request.businessDirectory, "reports", "周报-个人-2026-09-28~2026-10-04.md")
		if !strings.Contains(request.prompt, request.businessDirectory) {
			return nil, status_error.WorkflowStateInvalid.WithField("path", "write prompt omitted the approved business directory")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(markdown), 0600); err != nil {
			return nil, err
		}
		data = map[string]any{"reportPath": path, "written": true, "bytes": len([]byte(markdown))}
	case "verify-report":
		path := filepath.Join(request.businessDirectory, "reports", "周报-个人-2026-09-28~2026-10-04.md")
		if !strings.Contains(request.prompt, path) {
			return nil, status_error.WorkflowStateInvalid.WithField("path", "read prompt omitted the shared report path")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		data = map[string]any{"exists": true, "hasAllSections": strings.Contains(string(content), "## 下周计划"), "ok": string(content) == markdown, "issues": []string{}}
	default:
		return nil, status_error.WorkflowStateInvalid.WithField("node", nodeID)
	}
	if _, err := request.callResult(ctx, "workflow.results.submit", map[string]any{"data": data}); err != nil {
		return nil, err
	}

	return nil, nil
}

// fixtureEnvironment 返回真实 Core 会校验的固定能力目录，不替代接纳逻辑。
type fixtureEnvironment struct{}

func (fixtureEnvironment) Get(context.Context, string) (models.RuntimeEnvironment, error) {
	// 外部模型由测试替身接管，仅用测试进程满足真实 Core 的可执行路径校验。
	binary, err := os.Executable()
	if err != nil {
		return models.RuntimeEnvironment{}, err
	}
	sdk, err := agentapi.NewCatalog(agentapi.CatalogConfig{})
	if err != nil {
		return models.RuntimeEnvironment{}, err
	}
	var summary agentapi.RuntimeSummary
	for _, registered := range sdk.Runtimes() {
		if registered.ID == "codex" {
			summary = registered
			break
		}
	}
	return models.RuntimeEnvironment{RuntimeID: "codex", Enabled: true, Binary: binary, DefaultModel: "fixture", Detection: agentapi.DetectionResult{Runtime: summary, State: agentapi.DetectionStateReady, ResolvedBinary: binary, Capabilities: agentapi.CapabilitySet{agentapi.CapabilityMCPStreamableHTTP: true, agentapi.CapabilitySkillDirectory: true}, Models: []agentapi.ModelOption{{ID: "fixture", Name: "fixture", PermissionLevels: []agentapi.PermissionLevel{agentapi.PermissionLevelDefault, agentapi.PermissionLevelFullAccess}}}}}, nil
}

func openCoreWorkflowFixture(t *testing.T, directory string, runtime *fixtureRuntime) (*workflowFixture, func()) {
	return openCoreWorkflowPorts(t, directory, runtime, fixtureEnvironment{})
}

// coreTestEnvironment 是运行配置的唯一探测查询接缝。
type coreTestEnvironment interface {
	// Get 返回已探测环境，而不绕过 Core 校验。
	Get(context.Context, string) (models.RuntimeEnvironment, error)
}

func openCoreWorkflowPorts(t *testing.T, directory string, runtime workflowFixtureModel, environment coreTestEnvironment, domain ...bool) (*workflowFixture, func()) {
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
	registry := capability.New(builtinskills.Guide)
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
	conversations, err := conversation.NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	chats, err := chatservice.New(chatservice.Config{StorageDirectory: directory, Conversations: conversations, Projects: projects, QueryIndexes: chatservice.QueryIndexDependencies{Chats: controllers.NewChatController(database.DB), DeletionIntents: controllers.NewChatDeletionIntentController(database.DB), Transactions: transactions}})
	if err != nil {
		t.Fatal(err)
	}
	if err := chats.Start(ctx); err != nil {
		t.Fatal(err)
	}
	catalog, err := agentapi.NewCatalog(agentapi.CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	accounts := aiaccount.New(controllers.NewAIAccountController(database.DB), directory)
	resolve := accounts.ResolveAccount
	providerOptions := agentapi.ProviderRuntimeOptions{AcquireAccount: accounts.AcquireAccount}
	if runtime != nil {
		resolve = agentfixture.ResolveAccount
		providerOptions = rejectUnexpectedWorkflowNative(t)
	}
	providers := agentapi.NewProviderRuntime(providerOptions)
	threads := agentapi.NewThreadLifecycle(providers, conversations)
	core, err := execution.New(execution.Config{
		Threads: threads, Accounts: accounts, ResolveAccount: resolve, RuntimeSummaries: catalog.Runtimes(),
		Journal: chats, DormantChats: chats,
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
	var worker workflow.WorkerPort = workflow.CoreWorkerAdapter{Executions: core, Chats: chats}
	if len(domain) != 0 && domain[0] {
		worker = domainWorkflowWorker{CoreWorkerAdapter: workflow.CoreWorkerAdapter{Executions: core, Chats: chats}, model: runtime}
	}
	s := workflowTestNew(workflow.Config{Commands: controllers.NewAgentCommandController(database.DB), Events: hub, Store: controllers.NewWorkflowController(database.DB), Runs: controllers.NewWorkflowRunController(database.DB), CreationSessions: controllers.NewWorkflowCreationController(database.DB), Transactions: transactions, Executions: core, Chats: chats, Worker: worker, Wakers: wakers, Projects: projects, DataDirectory: directory})
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	close := func() {
		once.Do(func() {
			s.Close()
			core.Close()
			if err := threads.Close(context.Background()); err != nil {
				t.Error(err)
			}
			if err := providers.Close(context.Background()); err != nil {
				t.Error(err)
			}
			accounts.Close()
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

func TestF1DomainWeeklyRestartFailedRevisionThenActualWriteRead(t *testing.T) {
	directory := t.TempDir()
	runtime := &fixtureRuntime{calls: map[string]int{}}
	s, close := openCoreWorkflowPorts(t, directory, runtime, fixtureEnvironment{}, true)
	ctx := context.Background()
	source, err := os.ReadFile("../../internal/workflow/dsl/testdata/weekly-report.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "weekly", Title: "周报", CreationWakerID: "fixture-waker", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	if _, diagnostics, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save", Source: string(source), SDK: constants.WorkflowSDKIdentifier}); err != nil {
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
	s, _ = openCoreWorkflowPorts(t, directory, runtime, fixtureEnvironment{}, true)
	state = awaitFixtureState(t, s, run.ID, waiting)
	if pendingHuman(state).ID != originalHuman.ID {
		t.Fatal("restart replaced pending approval")
	}
	answer := func(value string) {
		t.Helper()
		for _, human := range state.Humans {
			if human.Status == constants.WorkflowStepStatusWaiting {
				if err := answerHumanFixture(ctx, workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: human.ID, RequestID: util.New(), Value: value}); err != nil {
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
	for _, effect := range state.Effects {
		if effect.Boundary.NodeID == "revise-report" && effect.Status == constants.WorkflowStepStatusFailed && effect.Failure == nil {
			t.Fatal("failed revision evidence missing")
		}
	}
}

func TestWorkerScopedConnectorPolicyRejectsBeforeNativeDispatch(t *testing.T) {
	directory := t.TempDir()
	runtime := &fixtureRuntime{calls: map[string]int{}}
	s, _ := openCoreWorkflowFixture(t, directory, runtime)
	ctx := context.Background()
	source := `export const meta={sdk:'ally.workflow',title:'内部能力',description:'系统结果提交不属于业务工具白名单',phases:[{id:'work',title:'执行',detail:'提交结果',canExit:true}]};return await worker('整理',{id:'facts',label:'整理',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker'},schema:{type:'object'},toolPolicy:{connectors:{mode:'allowlist',connectorIds:[]}}});`
	definition := savedTestWorkflow(t, s, source)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "system-reference", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal("system capability blocked run admission", err)
	}
	state := awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
		return workflowstate.TerminalRun(state.Run.Status)
	})
	if len(state.Effects) != 1 || state.Effects[0].Failure == nil || state.Run.Status != constants.WorkflowRunStatusFailed {
		t.Fatalf("scoped worker escaped resource admission: %+v", state.Effects)
	}
	failure, _ := state.Effects[0].Failure.Data.(map[string]any)
	if failure["code"] != status_error.RuntimePolicyUnsupported.Key() || state.Effects[0].Candidate != nil {
		t.Fatalf("unexpected scope failure: %+v", failure)
	}
	runtime.mutex.Lock()
	defer runtime.mutex.Unlock()
	if len(runtime.calls) != 0 {
		t.Fatal("rejected worker submitted a native prompt")
	}
}
