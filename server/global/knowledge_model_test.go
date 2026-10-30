package global

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	catalog "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/catalog"

	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestKnowledgeModelSelectionDoesNotRequireExecutionServiceOrPartner(t *testing.T) {
	ctx := context.Background()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "model.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	defer sqlDB.Close()
	previousRuntime, previousExecution := RuntimeEnvironmentControllerManager, ExecutionService
	RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(database.DB)
	ExecutionService = nil
	defer func() { RuntimeEnvironmentControllerManager = previousRuntime; ExecutionService = previousExecution }()
	environment := models.RuntimeEnvironment{ID: "runtime", RuntimeID: constants.RuntimeIDCodex, BaseRuntimeID: constants.RuntimeIDCodex, Enabled: true, DefaultModel: "known-model", Detection: runtimeconfig.DetectionResult{State: runtimeconfig.DetectionStateReady, Models: []runtimeconfig.ModelOption{{ID: "known-model"}}}}
	if err := RuntimeEnvironmentControllerManager.Ensure(ctx, environment); err != nil {
		t.Fatal(err)
	}
	selected, err := resolveKnowledgeModel(ctx, models.LibraryExecutionDefaults{RuntimeID: constants.RuntimeIDCodex})
	if err != nil || selected.Model != "known-model" || selected.AccountID != constants.AiAccountSystemID {
		t.Fatalf("独立模型选择失败: %+v %v", selected, err)
	}
	if _, err := resolveKnowledgeModel(ctx, models.LibraryExecutionDefaults{RuntimeID: constants.RuntimeIDCodex, Model: "unknown-model"}); !errors.Is(err, status_error.KnowledgeInvalid) {
		t.Fatalf("不存在的模型被接纳: %v", err)
	}
}

func TestKnowledgeModelAcceptsRunnableCLIWithoutIsolationCapability(t *testing.T) {
	ctx := context.Background()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "unsupported.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	defer sqlDB.Close()
	previous := RuntimeEnvironmentControllerManager
	RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(database.DB)
	defer func() { RuntimeEnvironmentControllerManager = previous }()
	environment := models.RuntimeEnvironment{ID: "unverified", RuntimeID: constants.RuntimeIDPi, BaseRuntimeID: constants.RuntimeIDPi, Enabled: true, Detection: runtimeconfig.DetectionResult{State: runtimeconfig.DetectionStateReady}}
	if err := RuntimeEnvironmentControllerManager.Ensure(ctx, environment); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveKnowledgeModel(ctx, models.LibraryExecutionDefaults{RuntimeID: constants.RuntimeIDPi}); err != nil {
		t.Fatalf("runnable model rejected: %v", err)
	}
	if _, err := resolveKnowledgeMaintenanceModel(ctx, models.LibraryExecutionDefaults{RuntimeID: constants.RuntimeIDPi}); err != nil {
		t.Fatalf("tool-capable maintenance inherited the pure-text restriction: %v", err)
	}
}

func TestKnowledgeUnknownCapabilityAcceptsValidManagedAccount(t *testing.T) {
	ctx := context.Background()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "managed.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	defer sqlDB.Close()
	previousRuntime, previousAccounts := RuntimeEnvironmentControllerManager, AIAccountService
	RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(database.DB)
	AIAccountService = aiaccount.New(controllers.NewAIAccountController(database.DB))
	defer func() { RuntimeEnvironmentControllerManager = previousRuntime; AIAccountService = previousAccounts }()
	account := models.AIAccount{ID: "managed-account", Name: "test", AuthMode: constants.AccountAuthModeAPIKey, HasCredential: true, SelectedModels: []string{"model-a"}, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: "https://model.example/v1"}}}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	environment := models.RuntimeEnvironment{ID: "no-system-login", RuntimeID: constants.RuntimeIDCodex, BaseRuntimeID: constants.RuntimeIDCodex, Enabled: true, Detection: runtimeconfig.DetectionResult{State: runtimeconfig.DetectionStateReady, Auth: runtimeconfig.AuthStatusMissing}}
	if err := RuntimeEnvironmentControllerManager.Ensure(ctx, environment); err != nil {
		t.Fatal(err)
	}
	selected, err := resolveKnowledgeModel(ctx, models.LibraryExecutionDefaults{RuntimeID: constants.RuntimeIDCodex, AccountID: account.ID, Model: "model-a"})
	if err != nil || selected.AccountID != account.ID || selected.Model != "model-a" {
		t.Fatalf("system login blocked managed model: %+v %v", selected, err)
	}
}

func TestKnowledgeGenerationWaitsForSubscriptionLeaseAndCanBeCancelled(t *testing.T) {
	ctx := context.Background()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "queued-model.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	defer sqlDB.Close()
	previousRuntime, previousAccounts, previousSDK, previousHost := RuntimeEnvironmentControllerManager, AIAccountService, RuntimeCatalog, KnowledgeRuntimeHost
	defer func() {
		RuntimeEnvironmentControllerManager, AIAccountService, RuntimeCatalog = previousRuntime, previousAccounts, previousSDK
		KnowledgeRuntimeHost = previousHost
	}()
	RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(database.DB)
	accounts := controllers.NewAIAccountController(database.DB)
	AIAccountService = aiaccount.New(accounts)
	AIAccountService.Start(ctx)
	defer AIAccountService.Close()
	RuntimeCatalog, err = catalog.New(catalog.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer RuntimeCatalog.Close(ctx)
	KnowledgeRuntimeHost = newKnowledgeRuntimeHost()
	defer KnowledgeRuntimeHost.Close(ctx)
	account := models.AIAccount{ID: "queued-subscription", Name: "fixture", AuthMode: constants.AccountAuthModeSubscription, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}}}
	if err := accounts.Create(ctx, account, "not-read-before-lease-is-acquired"); err != nil {
		t.Fatal(err)
	}
	binary, _ := os.Executable()
	environment := models.RuntimeEnvironment{ID: "queue-runtime", RuntimeID: constants.RuntimeIDCodex, BaseRuntimeID: constants.RuntimeIDCodex, Enabled: true, Binary: binary, Detection: runtimeconfig.DetectionResult{State: runtimeconfig.DetectionStateReady}}
	if err := RuntimeEnvironmentControllerManager.Ensure(ctx, environment); err != nil {
		t.Fatal(err)
	}
	release, err := AIAccountService.Exclusive(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	generator, err := newKnowledgeModelGenerator(ctx, models.LibraryExecutionDefaults{RuntimeID: constants.RuntimeIDCodex, AccountID: account.ID, Model: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	waitContext, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := generator.Generate(waitContext, core.GenerateRequest{Messages: []core.ModelMessage{{Role: "user", Content: "fixture"}}})
		finished <- err
	}()
	select {
	case err := <-finished:
		t.Fatalf("后台生成没有等待订阅租约: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, status_error.OperationCanceled) {
			t.Fatalf("等待取消没有保留真实原因: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("等待账号的知识生成不能取消")
	}
	if _, err := AIAccountService.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatal("等待方取消释放了其他调用的租约")
	}
}
