package workflows

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// workflowFixture 持有显式装配的真实共享服务与 API 依赖，不承载业务实现。
type workflowFixture struct {
	// Service 是真实源码、运行及 Runner 共享组件。
	*workflow.Service
	// Config 是同一组件使用的仓储、状态与外部执行边界。
	workflow.Config
}

func workflowTestService(t *testing.T) (*workflowFixture, controllers.WorkflowController) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "workflow.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	store := controllers.NewWorkflowController(database.DB)
	return workflowTestNew(workflow.Config{Commands: controllers.NewAgentCommandController(database.DB), Store: store, CreationSessions: controllers.NewWorkflowCreationController(database.DB), Consumers: controllers.NewWorkflowConsumerController(database.DB), Runs: controllers.NewWorkflowRunController(database.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))}), store
}

func workflowTestSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../internal/workflow/dsl/testdata/f0.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// workflowAPIDependencies 为测试显式装配 HTTP 入口使用的依赖，不编排业务规则。
func workflowAPIDependencies(s *workflowFixture) *workflowDependencies {
	deps := &workflowDependencies{
		Store: s.Store, Transactions: s.Transactions, Runs: s.Runs, CreationSessions: s.CreationSessions, State: s.State,
		Executions: s.Executions, Chats: s.Chats, Wakers: s.Wakers, Projects: s.Projects, DataDirectory: s.DataDirectory,
		SaveSource: func(ctx context.Context, definitionID, requestID, expectedID, source, restoredFrom string) (workflowdsl.SaveReceipt, []workflowdsl.Diagnostic, error) {
			return s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definitionID, RequestID: requestID, ExpectedRevisionID: expectedID, Source: source, SDKVersion: constants.WorkflowSDKVersion, RestoredFromRevisionID: restoredFrom})
		},
		RetryStart: func(ctx context.Context, previous models.WorkflowRun, requestID string) (models.WorkflowRun, error) {
			return s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: previous.DefinitionID, RevisionID: previous.RevisionID, Args: previous.Args, Defaults: previous.Defaults, WorkerConfigurations: previous.WorkerConfigurations, ApprovedScope: &previous.ScopeBinding, RequestID: requestID, RetrySourceRunID: previous.ID})
		},
	}
	if s.Worker != nil {
		deps.WorkerCancel = s.Worker.Cancel
	}
	return deps
}

// workflowTestNew 为真实仓储测试显式装配 API 与 Runner 共用的状态协调者。
func workflowTestNew(config workflow.Config) *workflowFixture {
	config.State = workflowstate.New(config.Store, config.Runs, config.Transactions)
	return &workflowFixture{Service: workflow.New(config), Config: config}
}
func savedTestWorkflow(t *testing.T, s *workflowFixture, source string) models.WorkflowDefinition {
	t.Helper()
	ctx := context.Background()
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "create-" + strings.ReplaceAll(t.Name(), "/", "-"), Title: "测试流程"})
	if err != nil {
		t.Fatal(err)
	}
	receipt, diagnostics, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save", Source: source, SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatalf("compile: %v %+v", err, diagnostics)
	}
	definition.CurrentRevisionID = receipt.RevisionID
	return definition
}

func runTicks(t *testing.T, s *workflowFixture, id string, count int) models.WorkflowRun {
	t.Helper()
	for index := 0; index < count; index++ {
		if err := s.Tick(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	value, err := s.Runs.GetRun(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// controlledWorker 用真实运行仓储控制智能体接纳与终态，避免依赖外部模型延迟。
type controlledWorker struct {
	// mutex 保护异步派发记录和可控的 B 执行终态。
	mutex sync.Mutex
	// prompts 保存真实派发顺序，检验管道没有全阶段栅栏。
	prompts []string
	// bCompleted 表示外部 B 执行已真实结束，可以由 Inspect 返回终态。
	bCompleted bool
}

func (*controlledWorker) ResolveAdmission(_ context.Context, options map[string]any, defaults models.WorkflowExecutionDefaults, _ bool) (models.WorkflowExecutionDefaults, error) {
	return defaults, nil
}

func (w *controlledWorker) Dispatch(_ context.Context, input workflow.WorkerDispatch) (workflow.WorkerReceipt, error) {
	prompt, _ := input.Effect.Boundary.Arguments[0].Data.(string)
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.prompts = append(w.prompts, prompt)
	return w.receipt(input.Effect, prompt), nil
}

func (w *controlledWorker) Inspect(_ context.Context, effect models.WorkflowEffect) (workflow.WorkerReceipt, error) {
	prompt, _ := effect.Boundary.Arguments[0].Data.(string)
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.receipt(effect, prompt), nil
}

// receipt 在持锁时读取可控终态，不把观察结果直接写入工作流状态。
func (w *controlledWorker) receipt(effect models.WorkflowEffect, prompt string) workflow.WorkerReceipt {
	receipt := workflow.WorkerReceipt{ChatID: "chat-" + effect.ID, ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusCompleted, FinalPresent: true, Text: prompt + "-result"}
	if prompt == "B" && !w.bCompleted {
		receipt.Status = constants.ExecutionStatusRunning
	}
	if prompt == "C" {
		receipt.Status, receipt.ErrorCode = constants.ExecutionStatusFailed, "ENV_FAILED"
	}
	return receipt
}

func (w *controlledWorker) completeB() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.bCompleted = true
}

func (w *controlledWorker) recordedPrompts() []string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return append([]string(nil), w.prompts...)
}

func (w *controlledWorker) Cancel(context.Context, models.WorkflowEffect) error { return nil }
