package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func workflowTestService(t *testing.T) (*Service, controllers.WorkflowController) {
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
	return workflowTestNew(Config{Commands: controllers.NewAgentCommandController(database.DB), Store: store, CreationSessions: controllers.NewWorkflowCreationController(database.DB), Consumers: controllers.NewWorkflowConsumerController(database.DB), Runs: controllers.NewWorkflowRunController(database.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))}), store
}

func workflowTestSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../internal/workflow/dsl/testdata/f0.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// receiptFailingStore 注入提交回执失败，用真实 SQLite 检查完整事务回滚。
type receiptFailingStore struct {
	// WorkflowController 继续使用真实数据库实现。
	controllers.WorkflowController
}

func (s receiptFailingStore) CreateReceipt(context.Context, models.WorkflowCommandReceipt) error {
	return status_error.WorkflowStateInvalid
}

func TestFailedSaveRollsBackRevisionPointerAndSuccessEvent(t *testing.T) {
	s, store := workflowTestService(t)
	ctx := context.Background()
	definition, err := fixtureDefinition(ctx, s.store, definitionFixture{RequestID: "create", Title: "空定义"})
	if err != nil {
		t.Fatal(err)
	}
	s.store = receiptFailingStore{WorkflowController: store}
	if _, _, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save", Source: workflowTestSource(t), SDKVersion: constants.WorkflowSDKVersion}); err == nil {
		t.Fatal("save should fail")
	}
	definition, err = store.GetDefinition(ctx, definition.ID)
	if err != nil || definition.CurrentRevisionID != "" || definition.Ordinal != 0 {
		t.Fatal("failed save changed current revision")
	}
	revisions, err := store.ListRevisions(ctx, definition.ID, 0, 50)
	if err != nil || len(revisions) != 0 {
		t.Fatal("failed save left half a revision")
	}
	events, err := store.ListEvents(ctx, definition.ID, 0, 100)
	if err != nil || len(events) != 1 {
		t.Fatal("failed save published success")
	}
}

func TestUnchangedSourceCreatesReceiptWithoutVersionNoise(t *testing.T) {
	s, store := workflowTestService(t)
	ctx := context.Background()
	definition, err := fixtureDefinition(ctx, s.store, definitionFixture{RequestID: "create", Title: "空定义"})
	if err != nil {
		t.Fatal(err)
	}
	save := SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save", Source: workflowTestSource(t), SDKVersion: constants.WorkflowSDKVersion}
	first, _, err := s.SaveSource(ctx, save)
	if err != nil {
		t.Fatal(err)
	}
	save.RequestID = "save-again"
	save.ExpectedRevisionID = first.RevisionID
	unchanged, _, err := s.SaveSource(ctx, save)
	if err != nil || !unchanged.Unchanged || unchanged.RevisionID != first.RevisionID {
		t.Fatalf("unchanged: %+v %v", unchanged, err)
	}
	history, err := store.ListRevisions(ctx, definition.ID, 0, 50)
	if err != nil || len(history) != 1 {
		t.Fatal("unchanged source created extra history")
	}
}

// workflowTestNew 为真实仓储测试显式装配 API 与 Runner 共用的状态协调者。
func workflowTestNew(config Config) *Service {
	config.State = workflowstate.New(config.Store, config.Runs, config.Transactions)
	return New(config)
}

// definitionFixture 只描述仓储测试所需的初始定义数据。
type definitionFixture struct {
	// RequestID 记录夹具来源标签，仓储初始化不执行 API 请求幂等。
	RequestID string
	// Title 是测试定义的展示标题。
	Title string
	// CreationWakerID 是测试定义绑定的创作伙伴。
	CreationWakerID string
	// ScopeBinding 是测试批准的工作范围。
	ScopeBinding models.WorkflowScopeBinding
}

// fixtureDefinition 直接建立仓储夹具，API 创建语义由 API 包独立验证。
func fixtureDefinition(ctx context.Context, store controllers.WorkflowController, input definitionFixture) (models.WorkflowDefinition, error) {
	definition := models.WorkflowDefinition{ID: util.New(), Title: input.Title, CreationWakerID: input.CreationWakerID, ScopeBinding: input.ScopeBinding}
	if err := store.CreateDefinition(ctx, definition); err != nil {
		return models.WorkflowDefinition{}, err
	}
	_, err := store.AppendEvent(ctx, models.WorkflowEvent{StreamID: definition.ID, EventID: util.New(), Type: constants.WorkflowDefinitionCreatedEvent, EntityID: definition.ID})
	return definition, err
}
