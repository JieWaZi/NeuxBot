package workflows

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	automatic_tasks "github.com/JieWaZi/NeuxBot/server/apis/automatic_tasks"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowinput "github.com/JieWaZi/NeuxBot/server/internal/workflow/input"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestWorkflowTriggersAtomicConfigurationReplayAndRemoval(t *testing.T) {
	h := newTriggerHarness(t)
	ctx := context.Background()
	source, err := os.ReadFile("../../internal/workflow/dsl/testdata/f0.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	definition, config := triggerWorkflowFixture(t, h, string(source))
	config.WorkflowInputBindings = map[string]workflowinput.Binding{"text": {Source: constants.WorkflowInputSourceFixed, Value: "NeuxBot"}}
	original, err := readTriggersThroughOperator(ctx, definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	command := SaveWorkflowTriggersCommand{RequestID: "shared", ExpectedFingerprint: original.Fingerprint, Configuration: config, Triggers: []WorkflowTrigger{{Trigger: constants.AutomaticTaskTriggerAPI, InputBindings: config.WorkflowInputBindings}, {Trigger: constants.AutomaticTaskTriggerSchedule, InputBindings: config.WorkflowInputBindings, Schedule: models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindDaily, Timezone: "UTC", Hour: 9}}}}
	saved, err := saveTriggersThroughOperator(ctx, definition.ID, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Tasks) != 2 || saved.Tasks[0].Token == "" {
		t.Fatalf("missing actual receipt: %+v", saved)
	}
	stored, err := readTriggersThroughOperator(ctx, definition.ID)
	if err != nil || len(stored.Tasks) != 2 || stored.Fingerprint != saved.Fingerprint {
		t.Fatalf("incomplete aggregate: %+v %v", stored, err)
	}
	for _, task := range stored.Tasks {
		if task.WakerID != h.wakerID || task.WorkflowID != definition.ID || task.WorkflowInputBindings["text"].Value != "NeuxBot" {
			t.Fatalf("lost shared input or binding: %+v", task)
		}
	}
	replay, err := saveTriggersThroughOperator(ctx, definition.ID, command)
	if err != nil || !replay.Replayed || len(replay.Tasks) != 2 || replay.Tasks[0].TaskID != saved.Tasks[0].TaskID || replay.Tasks[0].Token != "" {
		t.Fatalf("non-idempotent replay or leaked secret: %+v %v", replay, err)
	}
	command.Triggers[0].InputBindings = map[string]workflowinput.Binding{"text": {Source: constants.WorkflowInputSourceFixed, Value: "changed"}}
	if _, err = saveTriggersThroughOperator(ctx, definition.ID, command); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.AutomaticTaskRequestConflict.Key() {
		t.Fatalf("accepted changed replay: %v", err)
	}
	command.RequestID = "rollback"
	command.ExpectedFingerprint = stored.Fingerprint
	command.Triggers = []WorkflowTrigger{{TaskID: saved.Tasks[0].TaskID, Trigger: constants.AutomaticTaskTriggerAPI, InputBindings: config.WorkflowInputBindings}, {TaskID: saved.Tasks[1].TaskID, Trigger: constants.AutomaticTaskTriggerSchedule, InputBindings: config.WorkflowInputBindings, Schedule: models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindDaily, Timezone: "invalid/timezone", Hour: 9}}}
	if _, err = saveTriggersThroughOperator(ctx, definition.ID, command); err == nil {
		t.Fatal("accepted invalid second trigger")
	}
	unchanged, err := readTriggersThroughOperator(ctx, definition.ID)
	if err != nil || unchanged.Fingerprint != stored.Fingerprint || unchanged.Tasks[0].WorkflowInputBindings["text"].Value != "NeuxBot" {
		t.Fatalf("partially committed shared config: %+v %v", unchanged, err)
	}
	response := triggerTaskThroughHTTP(h, ctx, saved.Tasks[0].TaskID, saved.Tasks[0].Token, "still-valid")
	if response.Code != http.StatusAccepted {
		t.Fatalf("rollback revoked existing API token: %d %s", response.Code, response.Body.String())
	}
	command.RequestID = "stale"
	command.ExpectedFingerprint = original.Fingerprint
	command.Triggers = command.Triggers[:1]
	if _, err = saveTriggersThroughOperator(ctx, definition.ID, command); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.AutomaticTaskRevisionConflict.Key() {
		t.Fatalf("accepted stale whole config: %v", err)
	}
	command.RequestID = "remove"
	command.ExpectedFingerprint = stored.Fingerprint
	command.Triggers = nil
	if _, err = saveTriggersThroughOperator(ctx, definition.ID, command); err != nil {
		t.Fatal(err)
	}
	empty, err := readTriggersThroughOperator(ctx, definition.ID)
	if err != nil || len(empty.Tasks) != 0 {
		t.Fatalf("removed trigger remains configured: %+v %v", empty, err)
	}
	old, err := h.tasks.Get(ctx, saved.Tasks[0].TaskID)
	if err != nil || old.Enabled || !old.WorkflowTriggerDetached {
		t.Fatalf("removal destroyed history or remained enabled: %+v %v", old, err)
	}
	if _, total, err := h.tasks.ListRuns(ctx, old.ID, 0, 0); err != nil || total != 1 {
		t.Fatalf("removal lost actual occurrence: %d %v", total, err)
	}
	if response := triggerTaskThroughHTTP(h, ctx, old.ID, saved.Tasks[0].Token, "removed"); response.Code == http.StatusAccepted {
		t.Fatal("removed trigger admitted another run")
	}
	command.RequestID = "too-many"
	command.ExpectedFingerprint = empty.Fingerprint
	command.Triggers = make([]WorkflowTrigger, 6)
	if _, err = saveTriggersThroughOperator(ctx, definition.ID, command); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.BadRequest.Key() {
		t.Fatalf("accepted sixth trigger: %v", err)
	}
}

// triggerHarness 只建立API测试所需持久化、工作流共享状态与真实任务监听器。
type triggerHarness struct {
	// wakerID 是真实任务归属伙伴。
	wakerID string
	// directory 是本次定义批准的工作目录。
	directory string
	// store 直接建立定义前置数据，不复制创建用例。
	store controllers.WorkflowController
	// workflows 提供所有入口复用的源码保存与运行接纳。
	workflows *workflow.Service
	// tasks 持有真实配置、运行与幂等收据。
	tasks controllers.AutomaticTaskController
	// scheduler 与HTTP用例共用同一协调锁和配置保存核心。
	scheduler *schedule.Service
	// taskConfig 提供真实本机任务HTTP适配器的装配依赖。
	taskConfig schedule.Config
}

func newTriggerHarness(t *testing.T) *triggerHarness {
	t.Helper()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "triggers.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB.DB()
	t.Cleanup(func() { _ = connection.Close() })
	transactions := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	store := controllers.NewWorkflowController(db.DB)
	runs := controllers.NewWorkflowRunController(db.DB)
	state := workflowstate.New(store, runs, transactions)
	wakers := controllers.NewWakerController(db.DB)
	dataDirectory := t.TempDir()
	h := &triggerHarness{wakerID: util.New(), directory: t.TempDir(), store: store, tasks: controllers.NewAutomaticTaskController(db.DB)}
	if err := wakers.Create(context.Background(), models.Waker{ID: h.wakerID, Name: "自动任务伙伴"}); err != nil {
		t.Fatal(err)
	}
	h.workflows = workflow.New(workflow.Config{Store: store, Runs: runs, State: state, Transactions: transactions, DataDirectory: dataDirectory})
	h.taskConfig = schedule.Config{APIHandler: automatic_tasks.NewLocalHandler, Workflows: h.workflows, WorkflowRuns: runs, Tasks: h.tasks, Transactions: transactions, Wakers: wakers, DataDirectory: dataDirectory, Now: func() time.Time { return time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC) }}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	settings, _ := json.Marshal(map[string]any{"ip": "127.0.0.1", "port": port, "enabled": true})
	if err := os.WriteFile(filepath.Join(dataDirectory, "integration-api.json"), settings, 0600); err != nil {
		t.Fatal(err)
	}
	h.scheduler = schedule.New(h.taskConfig)
	if err := h.scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.scheduler.Close)
	previousStore, previousTasks, previousTransactions, previousScheduler := global.WorkflowControllerManager, global.AutomaticTaskControllerManager, global.TransactionControllerManager, global.ScheduleService
	global.WorkflowControllerManager, global.AutomaticTaskControllerManager, global.TransactionControllerManager, global.ScheduleService = store, h.tasks, transactions, h.scheduler
	t.Cleanup(func() {
		global.WorkflowControllerManager, global.AutomaticTaskControllerManager, global.TransactionControllerManager, global.ScheduleService = previousStore, previousTasks, previousTransactions, previousScheduler
	})
	return h
}

func triggerWorkflowFixture(t *testing.T, h *triggerHarness, source string) (models.WorkflowDefinition, models.AutomaticTaskConfiguration) {
	t.Helper()
	ctx := context.Background()
	definition := models.WorkflowDefinition{ID: util.New(), Title: "自动流程", CreationWakerID: h.wakerID, ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: h.directory}}
	if err := h.store.CreateDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	saved, diagnostics, err := h.workflows.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save", Source: source, SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatalf("%v: %+v", err, diagnostics)
	}
	definition.CurrentRevisionID = saved.RevisionID
	configuration := models.AutomaticTaskConfiguration{Name: "自动流程", TargetKind: constants.AutomaticTaskTargetKindWorkflow, WorkflowID: definition.ID, WorkflowRevisionPolicy: constants.WorkflowRevisionPolicyCurrent, WorkflowInputBindings: map[string]workflowinput.Binding{"text": {Source: constants.WorkflowInputSourceAPI}}, Trigger: constants.AutomaticTaskTriggerAPI, Enabled: true, PermissionLevel: constants.PermissionLevelDefault}
	return definition, configuration
}

func readTriggersThroughOperator(ctx context.Context, definitionID string) (WorkflowTriggersView, error) {
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/workflows/"+definitionID+"/triggers", nil).WithContext(ctx)
	operator := GetWorkflowTriggers{ID: definitionID}
	result, err := operator.Output(ginContext)
	if err != nil {
		return WorkflowTriggersView{}, err
	}
	return result.(WorkflowTriggersView), nil
}

func saveTriggersThroughOperator(ctx context.Context, definitionID string, command SaveWorkflowTriggersCommand) (SavedWorkflowTriggers, error) {
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = httptest.NewRequest(http.MethodPut, "/workflows/"+definitionID+"/triggers", nil).WithContext(ctx)
	operator := SaveWorkflowTriggers{ID: definitionID, Body: command}
	result, err := operator.Output(ginContext)
	if err != nil {
		return SavedWorkflowTriggers{}, err
	}
	return result.(SavedWorkflowTriggers), nil
}

func triggerTaskThroughHTTP(h *triggerHarness, ctx context.Context, taskID, token, requestID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8431/v1/automatic-tasks/"+taskID+"/runs", strings.NewReader(`{}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", requestID)
	response := httptest.NewRecorder()
	automatic_tasks.NewLocalHandler(h.scheduler, h.taskConfig, "127.0.0.1").ServeHTTP(response, request)
	return response
}
