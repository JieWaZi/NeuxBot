package automatic_tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// testExecutionBoundary 替代昂贵的模型调用，但保留持久身份和首次执行边界。
type testExecutionBoundary struct {
	// mutex 保护并发接纳的权威状态和收据。
	mutex sync.Mutex
	// states 保存稳定 Chat 身份对应的首次执行事实。
	states map[string]chat.InitialExecutionState
	// reads 记录每个 Chat 权威快照的读取次数，验证查询内去重。
	reads map[string]int
	// intents 保存进入执行核心的完整中立意图。
	intents []execution.WorkIntent
	// transactions 让测试聚合删除回调使用真实 SQLite 事务。
	transactions controllers.TransactionController
	// invalid 是保存或触发时模拟的依赖错误。
	invalid error
}

func (b *testExecutionBoundary) ValidateConfiguration(context.Context, execution.ExecutionConfiguration) error {
	return b.invalid
}
func (b *testExecutionBoundary) Start(_ context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	identity := requestidempotency.Identity{ID: intent.ClientRequestID}
	id := identity.ResourceID("chat", intent.Chat.Create.WakerID)
	if state, exists := b.states[id]; exists {
		return execution.AcceptedExecution{Chat: state.Chat, Execution: *state.InitialExecution, Replayed: true}, nil
	}
	b.intents = append(b.intents, intent)
	state := chat.InitialExecutionState{Chat: models.Chat{ID: id, WakerID: intent.Chat.Create.WakerID, HiddenFromWakerHistory: intent.Chat.Create.HiddenFromWakerHistory, ActiveExecutionID: intent.ExecutionID},
		CreateRequestID: intent.ClientRequestID, InitialExecution: &models.Execution{ID: intent.ExecutionID, ChatID: id, Status: constants.ExecutionStatusRunning}}
	b.states[id] = state
	return execution.AcceptedExecution{Chat: state.Chat, Execution: *state.InitialExecution}, nil
}
func (b *testExecutionBoundary) ReadInitialExecution(_ context.Context, id string) (chat.InitialExecutionState, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.reads == nil {
		b.reads = map[string]int{}
	}
	b.reads[id]++
	state, exists := b.states[id]
	if !exists {
		return chat.InitialExecutionState{}, status_error.ChatNotFound.WithParams(map[string]interface{}{"ChatID": id})
	}
	copy := *state.InitialExecution
	state.InitialExecution = &copy
	return chat.InitialExecutionState{Chat: state.Chat, CreateRequestID: state.CreateRequestID, InitialExecution: state.InitialExecution}, nil
}
func (b *testExecutionBoundary) DeleteChatsAtomically(ctx context.Context, ids []string, finalize func(context.Context) error) error {
	if err := b.transactions.Transaction(ctx, finalize); err != nil {
		return err
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	for _, id := range ids {
		delete(b.states, id)
	}
	return nil
}
func (b *testExecutionBoundary) finish(id string, status constants.ExecutionStatus, continuation bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	state := b.states[id]
	state.InitialExecution.Status = status
	state.Chat.ActiveExecutionID = ""
	state.Chat.WaitingApproval = false
	state.Chat.WaitingElicitation = false
	if continuation {
		state.Chat.ActiveExecutionID = "manual-continuation"
	}
	b.states[id] = state
}

// testHarness 组合真实任务 Repository 与受控执行和时钟边界。
type testHarness struct {
	// workflows 保留真实源码版本和运行状态，验证调度不会创建第二种流程运行器。
	workflows *workflow.Service
	// service 是待验收的自动任务服务。
	service *schedule.Service
	// api 验证真实API拥有的入口业务与查询。
	api taskAPI
	// workflowStore 直接建立跨任务验收所需定义，不复制工作流创建用例。
	workflowStore controllers.WorkflowController
	// workflowState 与删除API、Runner共用同一命令保护。
	workflowState *workflowstate.Coordinator
	// boundary 是可产生等待、终态和后续手动执行的模型边界。
	boundary *testExecutionBoundary
	// now 是并发安全的注入时钟。
	now atomic.Int64
	// wakerID 是真实数据库中已建立的伙伴。
	wakerID string
	// directory 是用户授权的真实临时目录。
	directory string
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "schedule.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := database.DB.DB()
	t.Cleanup(func() { _ = sql.Close() })
	tx := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	boundary := &testExecutionBoundary{states: map[string]chat.InitialExecutionState{}, transactions: tx}
	wakers := controllers.NewWakerController(database.DB)
	h := &testHarness{boundary: boundary, wakerID: util.New(), directory: t.TempDir()}
	workflowStore := controllers.NewWorkflowController(database.DB)
	workflowRuns := controllers.NewWorkflowRunController(database.DB)
	workflowState := workflowstate.New(workflowStore, workflowRuns, tx)
	dataDirectory := t.TempDir()
	managedDirectory := t.TempDir()
	h.workflows = workflow.New(workflow.Config{Store: workflowStore, Runs: workflowRuns, State: workflowState, Transactions: tx, DataDirectory: managedDirectory})
	h.workflowStore, h.workflowState = workflowStore, workflowState
	h.now.Store(time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC).UnixMilli())
	if err := wakers.Create(context.Background(), models.Waker{ID: h.wakerID, Name: "自动任务伙伴"}); err != nil {
		t.Fatal(err)
	}
	h.api = taskAPI{config: schedule.Config{APIHandler: NewLocalHandler, Workflows: h.workflows, WorkflowRuns: workflowRuns, Tasks: controllers.NewAutomaticTaskController(database.DB), Transactions: tx, Wakers: wakers,
		Projects: controllers.NewProjectController(database.DB), Executions: boundary, Chats: boundary,
		Channels: controllers.NewChannelController(database.DB), Threads: controllers.NewChannelThreadController(database.DB), TeamTasks: controllers.NewChannelTaskController(database.DB), DataDirectory: dataDirectory, ManagedFilesDirectory: managedDirectory, Now: func() time.Time { return time.UnixMilli(h.now.Load()).UTC() }}}
	portData, _ := json.Marshal(map[string]any{"ip": "127.0.0.1", "port": availableAPIPort(t), "enabled": true})
	if err := os.WriteFile(filepath.Join(dataDirectory, "integration-api.json"), portData, 0600); err != nil {
		t.Fatal(err)
	}
	h.service = schedule.New(h.api.config)
	h.api.service = h.service
	if err := h.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.service.Close() })
	return h
}

func (h *testHarness) config(trigger constants.AutomaticTaskTrigger) models.AutomaticTaskConfiguration {
	return models.AutomaticTaskConfiguration{Name: "资料归档", Instruction: "整理项目资料", WorkingDirectory: h.directory, RuntimeID: "codex", AccountID: "system", Model: "model", PermissionLevel: constants.PermissionLevelDefault,
		Enabled: true, Trigger: trigger, Schedule: models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindCron, Timezone: "UTC", Cron: "* * * * *"}}
}
func (h *testHarness) save(t *testing.T, trigger constants.AutomaticTaskTrigger) schedule.SavedTask {
	t.Helper()
	saved, err := h.service.SaveConfiguration(context.Background(), h.wakerID, "", 0, h.config(trigger), true)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestAutomaticCRUDTokensAndFrozenExecution(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	saved := h.save(t, constants.AutomaticTaskTriggerAPI)
	view, err := h.service.Get(ctx, h.wakerID, saved.TaskID)
	if err != nil || len(view.TokenHash) != 32 || saved.Token == "" {
		t.Fatalf("create/get: %+v %v", view, err)
	}
	if _, err := h.service.Get(ctx, "other-waker", saved.TaskID); !errors.Is(err, status_error.AutomaticTaskNotFound) {
		t.Fatal("cross-waker access accepted")
	}
	run, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "api-key", " 附加要求 ")
	if err != nil || run.ChatID == "" || run.ExecutionID == "" {
		t.Fatalf("trigger: %+v %v", run, err)
	}
	intent := h.boundary.intents[0]
	if intent.Admission != constants.ExecutionAdmissionCommitBeforeDispatch || !intent.Chat.Create.HiddenFromWakerHistory || intent.Input.Actor != constants.AutomaticTaskActor {
		t.Fatalf("wrong execution contract: %+v", intent)
	}
	replayed, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "api-key", "附加要求")
	if err != nil || replayed.ID != run.ID || len(h.boundary.intents) != 1 {
		t.Fatal("same key dispatched twice")
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "api-key", "different"); !errors.Is(err, status_error.AutomaticTaskRequestConflict) {
		t.Fatal("different input accepted")
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "new-key", ""); !errors.Is(err, status_error.AutomaticTaskBusy) {
		t.Fatal("busy task accepted another key")
	}
	view, _ = h.service.Get(ctx, h.wakerID, saved.TaskID)
	view.Instruction = "修改后的指令"
	if _, err := h.service.SaveConfiguration(ctx, h.wakerID, saved.TaskID, view.ConfigurationVersion, view.AutomaticTaskConfiguration, true); err != nil {
		t.Fatal(err)
	}
	history, _, _ := h.api.Runs(ctx, h.wakerID, saved.TaskID, 20, 0)
	if history[0].Configuration.Instruction != "整理项目资料" {
		t.Fatal("active snapshot was overwritten")
	}
	view, _ = h.service.Get(ctx, h.wakerID, saved.TaskID)
	if err := h.api.SetEnabled(ctx, h.wakerID, saved.TaskID, view.ConfigurationVersion, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.api.ReadAPIState(ctx, saved.TaskID, saved.Token, run.ID); err != nil {
		t.Fatal("disabled task cannot read state")
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "api-key", "附加要求"); err != nil {
		t.Fatal("disabled accepted receipt cannot replay")
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "new-key", ""); !errors.Is(err, status_error.AutomaticTaskDisabled) {
		t.Fatal("disabled task accepts new work")
	}
	view, _ = h.service.Get(ctx, h.wakerID, saved.TaskID)
	rotated, err := h.api.RotateToken(ctx, h.wakerID, saved.TaskID, view.ConfigurationVersion)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.api.ReadAPIState(ctx, saved.TaskID, saved.Token, run.ID); !errors.Is(err, status_error.AutomaticTaskTokenInvalid) {
		t.Fatal("old token survived rotation")
	}
	if _, err := h.api.ReadAPIState(ctx, saved.TaskID, rotated.Token, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.api.Delete(ctx, h.wakerID, saved.TaskID); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.AutomaticTaskBusy.Key() {
		t.Fatal("active chat deletion accepted")
	}
	h.boundary.finish(run.ChatID, constants.ExecutionStatusFailed, true)
	view, _ = h.service.Get(ctx, h.wakerID, saved.TaskID)
	if view.ActiveRunID != "" || view.LastRun.State != constants.AutomaticRunStateFailed {
		t.Fatal("manual continuation changed automatic result or slot")
	}
	if err := h.api.Delete(ctx, h.wakerID, saved.TaskID); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.AutomaticTaskBusy.Key() {
		t.Fatal("manual continuation deletion accepted")
	}
	h.boundary.finish(run.ChatID, constants.ExecutionStatusFailed, false)
	if err := h.api.Delete(ctx, h.wakerID, saved.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Get(ctx, h.wakerID, saved.TaskID); !errors.Is(err, status_error.AutomaticTaskNotFound) {
		t.Fatal("task survived delete")
	}
	if len(h.boundary.states) != 0 {
		t.Fatal("owned chat survived aggregate delete")
	}
}

func TestAutomaticTimerGraceBusyAndOfflineMisses(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	saved := h.save(t, constants.AutomaticTaskTriggerSchedule)
	h.advanceContinuous(t, 59)
	h.now.Add(6000)
	if err := h.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	view, _ := h.service.Get(ctx, h.wakerID, saved.TaskID)
	if view.RunCount != 1 || view.ActiveRun == nil {
		t.Fatal("continuous grace did not execute")
	}
	h.advanceContinuous(t, 60)
	if err := h.service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	runs, total, _ := h.api.Runs(ctx, h.wakerID, saved.TaskID, 20, 0)
	if total != 2 || runs[0].State != constants.AutomaticRunStateSkipped || runs[0].ChatID != "" {
		t.Fatal("busy timer created chat or lost skip")
	}
	h.now.Add(3 * 60_000)
	if err := h.service.SetSuspended(ctx, false); err != nil {
		t.Fatal(err)
	}
	view, _ = h.service.Get(ctx, h.wakerID, saved.TaskID)
	if view.MissedCount != 3 || view.RunCount != 1 || view.LastMissedAt == nil {
		t.Fatalf("offline aggregation: %+v", view)
	}
	_, total, _ = h.api.Runs(ctx, h.wakerID, saved.TaskID, 20, 0)
	if total != 2 {
		t.Fatal("offline periods created individual records")
	}
}

func TestAutomaticConcurrentKeysAndWaitingSlot(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	saved := h.save(t, constants.AutomaticTaskTriggerAPI)
	var group sync.WaitGroup
	ids := make(chan string, 8)
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			run, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "same-key", "")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- run.ID
		}()
	}
	group.Wait()
	close(ids)
	var id string
	for current := range ids {
		if id != "" && current != id {
			t.Fatal("one request produced multiple runs")
		}
		id = current
	}
	if len(h.boundary.intents) != 1 {
		t.Fatal("concurrent request produced multiple Chat starts")
	}
	run, _ := h.api.config.Tasks.GetRun(ctx, id)
	h.boundary.mutex.Lock()
	state := h.boundary.states[run.ChatID]
	state.Chat.WaitingApproval = true
	h.boundary.states[run.ChatID] = state
	h.boundary.mutex.Unlock()
	view, _ := h.service.Get(ctx, h.wakerID, saved.TaskID)
	if view.ActiveRun.State != constants.AutomaticRunStateWaitingApproval {
		t.Fatal("approval wait was not projected")
	}
	if _, err := h.api.TriggerManual(ctx, h.wakerID, saved.TaskID, "manual-key"); !errors.Is(err, status_error.AutomaticTaskBusy) {
		t.Fatal("waiting approval did not hold slot")
	}
}

func TestTeamAttentionIncludesAllActiveMemberChats(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	channelID, threadID, taskID, runID := util.New(), util.New(), util.New(), util.New()
	leadChatID, memberChatID, executionID := util.New(), util.New(), util.New()
	readyAt := int64(1)
	if err := h.api.config.Threads.Create(ctx, models.ChannelThread{ID: threadID, ChannelID: channelID, LeadWakerID: h.wakerID, LeadChatID: leadChatID, ReadyAt: &readyAt}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: h.wakerID, ChatID: leadChatID, IsLead: true},
		{ThreadID: threadID, WakerID: "member", ChatID: memberChatID},
	}); err != nil {
		t.Fatal(err)
	}
	configuration := h.config(constants.AutomaticTaskTriggerAPI)
	configuration.ChannelID = channelID
	task := models.AutomaticTask{ID: taskID, WakerID: h.wakerID, AutomaticTaskConfiguration: configuration, ActiveRunID: runID}
	run := models.AutomaticRun{ID: runID, TaskID: taskID, WakerID: h.wakerID, ThreadID: threadID, ChatID: leadChatID, ExecutionID: executionID, Configuration: configuration, Admission: constants.AutomaticRunAdmissionAccepted}
	if err := h.api.config.Tasks.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := h.api.config.Tasks.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	h.boundary.states[leadChatID] = chat.InitialExecutionState{Chat: models.Chat{ID: leadChatID, WakerID: h.wakerID, ActiveExecutionID: executionID}, InitialExecution: &models.Execution{ID: executionID, Status: constants.ExecutionStatusRunning}}
	h.boundary.states[memberChatID] = chat.InitialExecutionState{Chat: models.Chat{ID: memberChatID, WakerID: "member", ActiveExecutionID: "member-execution", WaitingApproval: true, WaitingElicitation: true}, InitialExecution: &models.Execution{ID: "member-execution", Status: constants.ExecutionStatusRunning}}
	attention, err := h.api.GetAttention(ctx, h.wakerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attention.Runs) != 1 || attention.Runs[0].State != constants.AutomaticRunStateWaitingApproval || len(attention.ActiveChats) != 2 {
		t.Fatalf("team attention omitted a member: %+v", attention)
	}
	seen := map[string]bool{}
	for _, member := range attention.ActiveChats {
		if seen[member.ID] {
			t.Fatal("duplicate active member Chat")
		}
		seen[member.ID] = true
	}
	if h.boundary.reads[leadChatID] != 1 || h.boundary.reads[memberChatID] != 1 {
		t.Fatalf("replayed member logs more than once: %+v", h.boundary.reads)
	}
	if !seen[leadChatID] || !seen[memberChatID] {
		t.Fatalf("active member IDs: %+v", seen)
	}
}

// restart 通过真实关闭与启动验证持久机会恢复，不直接调用私有恢复函数。
func (h *testHarness) restart(ctx context.Context) error {
	h.service.Close()
	h.service = schedule.New(h.api.config)
	h.api.service = h.service
	return h.service.Start(ctx)
}

// advanceContinuous 用真实连续扫描推进受控时钟，避免修改调度器私有扫描游标。
func (h *testHarness) advanceContinuous(t *testing.T, seconds int) {
	t.Helper()
	for range seconds {
		h.now.Add(1000)
		if err := h.service.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func (b *testExecutionBoundary) GetChatSummary(ctx context.Context, id string) (chat.ChatSummary, error) {
	value, err := b.ReadInitialExecution(ctx, id)
	return chat.ChatSummary{Chat: value.Chat}, err
}

// runWorkflowUntil 启动真实后台Runner并轮询权威记录，不公开或复制单入口推进逻辑。
func (h *testHarness) runWorkflowUntil(t *testing.T, ctx context.Context, runID string, expected constants.WorkflowRunStatus) {
	t.Helper()
	if err := h.workflows.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.workflows.Close)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		run, err := h.api.config.WorkflowRuns.GetRun(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == expected {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("workflow status=%s, want=%s", run.Status, expected)
		case <-poll.C:
		}
	}
}
