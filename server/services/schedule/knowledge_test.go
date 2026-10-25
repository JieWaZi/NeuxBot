package schedule

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	knowledgeService "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomaticKnowledgeDoesNotSilentlyIgnoreTeamOrWorkflowAuthorization(t *testing.T) {
	for name, config := range map[string]models.AutomaticTaskConfiguration{"team": {ChannelID: "team"}, "workflow": {WorkflowID: "workflow"}} {
		t.Run(name, func(t *testing.T) {
			config.Knowledge = &models.KnowledgeMaintenanceAuthorization{Backend: "local", ConnectionID: "local", KnowledgeBaseID: "library", Operation: "organize"}
			service := New(Config{})
			_, err := service.saveConfiguration(context.Background(), "waker", "", 0, config, false)
			if !errors.Is(err, status_error.KnowledgeUnsupported) {
				t.Fatalf("unsupported target=%v", err)
			}
		})
	}
}

// knowledgeScheduleExecutions 只替代真实模型边界，调用已经签发的实际知识能力。
type knowledgeScheduleExecutions struct {
	// Service 保持知识服务配置端口形状，未装配方法不得调用。
	execution.Service
	// calls 记录标准调度执行被接纳次数。
	calls int
	// intent 保存本次冻结的实际工作输入。
	intent execution.WorkIntent
	// receiptID 固定能力调用的幂等请求身份。
	receiptID string
	// states 作为既有Chat首次执行端口的测试事实源。
	states map[string]chat.InitialExecutionState
}

func (p *knowledgeScheduleExecutions) ValidateConfiguration(context.Context, execution.ExecutionConfiguration) error {
	return nil
}
func (p *knowledgeScheduleExecutions) ResolveConfiguration(_ context.Context, value execution.ExecutionConfiguration) (execution.ExecutionConfiguration, error) {
	value.Resolved = true
	return value, nil
}
func (p *knowledgeScheduleExecutions) Start(ctx context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	p.calls++
	p.intent = intent
	p.receiptID = intent.ClientRequestID + ".organize"
	found := false
	for _, capability := range intent.Options.ToolCapabilities {
		for _, binding := range capability.Bindings {
			if binding.Operation != "knowledge.jobs.start" {
				continue
			}
			found = true
			result, err := binding.Call(ctx, agentcapability.Request{Operation: binding.Operation, RequestID: p.receiptID, Params: map[string]any{}})
			if err != nil {
				return execution.AcceptedExecution{}, err
			}
			p.receiptID = result.Data.(map[string]any)["receipt"].(*core.Receipt).RequestID
		}
	}
	if !found {
		return execution.AcceptedExecution{}, errors.New("automatic knowledge binding was not dispatched")
	}
	id := (requestidempotency.Identity{ID: intent.ClientRequestID}).ResourceID("chat", intent.Chat.Create.WakerID)
	accepted := execution.AcceptedExecution{Chat: models.Chat{ID: id, WakerID: intent.Chat.Create.WakerID, ActiveExecutionID: intent.ExecutionID}, Execution: models.Execution{ID: intent.ExecutionID, Status: constants.ExecutionStatusRunning}}
	p.states[id] = chat.InitialExecutionState{Chat: accepted.Chat, CreateRequestID: intent.ClientRequestID, InitialExecution: &accepted.Execution}
	return accepted, nil
}

// knowledgeScheduleChats 仅复用调度已有首次执行快照，不制造知识任务状态。
type knowledgeScheduleChats struct {
	// Chats 保持未使用的Chat聚合删除接口。
	Chats
	// executions 指向测试模型边界保存的首次执行事实。
	executions *knowledgeScheduleExecutions
}

func (p knowledgeScheduleChats) ReadInitialExecution(_ context.Context, id string) (chat.InitialExecutionState, error) {
	value, ok := p.executions.states[id]
	if !ok {
		return chat.InitialExecutionState{}, status_error.ChatNotFound
	}
	return value, nil
}
func (p knowledgeScheduleChats) GetChatSummary(ctx context.Context, id string) (chat.ChatSummary, error) {
	value, err := p.ReadInitialExecution(ctx, id)
	return chat.ChatSummary{Chat: value.Chat}, err
}

func knowledgeScheduleFixture(t *testing.T) (*Service, *core.Engine, *knowledgeScheduleExecutions, models.AutomaticTask, *core.Library) {
	t.Helper()
	ctx := context.Background()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "ally.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	created, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "library", Name: "调度知识", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	library, err := engine.GetLibrary(ctx, created.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	wakers := controllers.NewWakerController(db.DB)
	wakerID := util.New()
	if err = wakers.Create(ctx, models.Waker{ID: wakerID, Name: "调度伙伴"}); err != nil {
		t.Fatal(err)
	}
	projects := controllers.NewProjectController(db.DB)
	executors := &knowledgeScheduleExecutions{states: map[string]chat.InitialExecutionState{}}
	tasks := controllers.NewAutomaticTaskController(db.DB)
	knowledge := knowledgeService.New(knowledgeService.Config{Engine: engine, References: controllers.NewKnowledgeController(db.DB), Projects: projects, AutomaticRun: tasks.GetRun, ResolveModel: func(_ context.Context, selection models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
		return selection, nil
	}})
	service := New(Config{Tasks: controllers.NewAutomaticTaskController(db.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)), Wakers: wakers, Projects: projects, Executions: executors, Chats: knowledgeScheduleChats{executions: executors}, BindKnowledge: knowledge.BindIntent})
	// 本回归只激活同步接纳生命周期，不启动后台扫描器或HTTP监听器。
	service.ctx = ctx
	saved, err := service.SaveConfiguration(ctx, wakerID, "", 0, models.AutomaticTaskConfiguration{Name: "整理知识", Instruction: "整理授权知识库", Trigger: constants.AutomaticTaskTriggerAPI, RuntimeID: constants.RuntimeIDCodex, AccountID: "system", Model: "configured-model", Reasoning: "low", PermissionLevel: constants.PermissionLevelDefault, Enabled: true, Knowledge: &models.KnowledgeMaintenanceAuthorization{Backend: "local", ConnectionID: "local", KnowledgeBaseID: library.ID, Operation: "organize"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.config.Tasks.Get(ctx, saved.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	return service, engine, executors, task, library
}

func TestAutomaticKnowledgeUsesExistingAdmissionAndStableOrganizeReceipt(t *testing.T) {
	service, engine, executors, task, library := knowledgeScheduleFixture(t)
	ctx := context.Background()
	command := AdmissionCommand{Task: task, Source: constants.AutomaticRunSourceManual, OccurrenceKey: "knowledge-occurrence", RequestID: "knowledge-request", Input: "本次保持人工主题"}
	run, err := service.Admit(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if run.Admission != constants.AutomaticRunAdmissionAccepted || run.State != constants.AutomaticRunStateRunning || executors.calls != 1 {
		t.Fatal(run, executors.calls)
	}
	intent := executors.intent
	if intent.ClientRequestID != run.ID || intent.ExecutionID != run.ExecutionID || intent.Admission != constants.ExecutionAdmissionCommitBeforeDispatch || intent.Chat.Create == nil || !intent.Chat.Create.HiddenFromWakerHistory || intent.Input.Actor != constants.AutomaticTaskActor || !strings.Contains(intent.Input.Content, command.Input) {
		t.Fatal("schedule identity or input was replaced", intent)
	}
	if intent.Options.RuntimeID != task.RuntimeID || intent.Options.AccountID != task.AccountID || intent.Options.Model != task.Model || intent.Options.Reasoning != task.Reasoning || intent.Options.WorkspaceReadOnly || intent.Options.PermissionLevel != task.PermissionLevel {
		t.Fatal("knowledge bypassed the frozen execution configuration", intent.Options)
	}
	receipt, err := engine.GetReceipt(ctx, executors.receiptID)
	if err != nil || receipt.State != "no-op" || receipt.Operation != "job.accept" || receipt.LibraryID != library.ID {
		t.Fatal(receipt, err)
	}
	if _, total, err := engine.ListJobs(ctx, library.ID, 0, 50); err != nil || total != 0 {
		t.Fatal("empty batch created a model job", total, err)
	}
	replay, err := service.Admit(ctx, command)
	if err != nil || replay.ID != run.ID || replay.ExecutionID != run.ExecutionID || executors.calls != 1 {
		t.Fatal("same occurrence dispatched twice", replay, executors.calls, err)
	}
	command.Input = "不同输入"
	if _, err = service.Admit(ctx, command); !errors.Is(err, status_error.AutomaticTaskRequestConflict) {
		t.Fatal("same occurrence accepted changed input", err)
	}
	if run.Configuration.Knowledge == nil || run.Configuration.Knowledge.KnowledgeBaseID != library.ID {
		t.Fatal("knowledge target was not frozen", run.Configuration)
	}
	// 核心no-op并不把调度运行伪造为完成；首次Chat Execution终态仍是唯一调度状态源。
	state := executors.states[run.ChatID]
	completedAt := service.config.Now().UnixMilli()
	state.Chat.ActiveExecutionID = ""
	state.InitialExecution.Status = constants.ExecutionStatusCompleted
	state.InitialExecution.CompletedAt = &completedAt
	executors.states[run.ChatID] = state
	view, err := service.ReadRun(ctx, run.AutomaticRun)
	if err != nil || view.State != constants.AutomaticRunStateCompleted || view.CompletedAt == nil {
		t.Fatal(view, err)
	}
	latest, err := service.config.Tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.refreshSlot(ctx, latest); err != nil {
		t.Fatal(err)
	}
	latest, err = service.config.Tasks.Get(ctx, task.ID)
	if err != nil || latest.ActiveRunID != "" {
		t.Fatal("completed run retained schedule slot", latest.ActiveRunID, err)
	}
}

func TestAutomaticKnowledgeRevocationUsesExistingAdmissionFailure(t *testing.T) {
	service, engine, executors, task, library := knowledgeScheduleFixture(t)
	ctx := context.Background()
	if _, err := engine.UpdateLibrary(ctx, core.UpdateLibraryInput{RequestID: "revoke", LibraryID: library.ID, ExpectedVersion: library.Version, Name: library.Name, Archived: true, PolicyVersion: library.PolicyVersion + 1}); err != nil {
		t.Fatal(err)
	}
	command := AdmissionCommand{Task: task, Source: constants.AutomaticRunSourceManual, OccurrenceKey: "revoked-opportunity", RequestID: "revoked-request"}
	if _, err := service.Admit(ctx, command); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatal("revoked knowledge was admitted", err)
	}
	run, exists, err := service.config.Tasks.FindRun(ctx, task.ID, command.OccurrenceKey)
	if err != nil || !exists || run.Admission != constants.AutomaticRunAdmissionFailed || run.ErrorCode != status_error.KnowledgeForbidden.Key() || run.ChatID != "" || run.ExecutionID != "" {
		t.Fatal(run, exists, err)
	}
	current, err := service.config.Tasks.Get(ctx, task.ID)
	if err != nil || current.ActiveRunID != "" || executors.calls != 0 {
		t.Fatal("failure bypassed normal slot release", current.ActiveRunID, executors.calls, err)
	}
	if _, err = service.Admit(ctx, command); !errors.Is(err, status_error.AutomaticTaskAdmissionRevoked) {
		t.Fatal("failed opportunity was blindly redispatched", err)
	}
	if executors.calls != 0 {
		t.Fatal("revoked opportunity reached execution")
	}
}

func TestAutomaticKnowledgeMissingBinderReleasesAdmissionSlot(t *testing.T) {
	service, _, executors, task, _ := knowledgeScheduleFixture(t)
	service.config.BindKnowledge = nil
	ctx := context.Background()
	command := AdmissionCommand{Task: task, Source: constants.AutomaticRunSourceManual, OccurrenceKey: "unsupported-opportunity", RequestID: "unsupported-request"}
	if _, err := service.Admit(ctx, command); !errors.Is(err, status_error.KnowledgeUnsupported) {
		t.Fatalf("missing binder error=%v", err)
	}
	run, exists, err := service.config.Tasks.FindRun(ctx, task.ID, command.OccurrenceKey)
	if err != nil || !exists || run.Admission != constants.AutomaticRunAdmissionFailed || run.ErrorCode != status_error.KnowledgeUnsupported.Key() {
		t.Fatalf("unsupported admission was left pending: %+v exists=%v err=%v", run, exists, err)
	}
	current, err := service.config.Tasks.Get(ctx, task.ID)
	if err != nil || current.ActiveRunID != "" || executors.calls != 0 {
		t.Fatalf("unsupported knowledge retained slot: active=%s calls=%d err=%v", current.ActiveRunID, executors.calls, err)
	}
}
