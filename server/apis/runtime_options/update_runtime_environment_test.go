package runtime_options

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestCanCompleteStartupSetupRequiresDetectedModelsAndPermissions(t *testing.T) {
	ready := models.RuntimeEnvironment{
		Enabled:                true,
		IsDefault:              true,
		DefaultModel:           "free-model",
		DefaultPermissionLevel: agentapi.PermissionLevelFullAccess,
		Detection: agentapi.DetectionResult{
			ResolvedBinary: "/installed/opencode",
			State:          agentapi.DetectionStateReady,
			Models: []agentapi.ModelOption{{
				ID:               "free-model",
				PermissionLevels: []agentapi.PermissionLevel{agentapi.PermissionLevelFullAccess},
			}},
			Diagnostics: []agentapi.Diagnostic{},
		},
	}
	tests := []struct {
		// name 说明需要保留引导或允许完成的场景。
		name string
		// change 修改所选智能体的真实探测与默认配置。
		change func(*models.RuntimeEnvironment)
		// allowed 表示是否允许解除首次引导。
		allowed bool
	}{
		{name: "模型与权限就绪", allowed: true},
		{name: "所选智能体探测未就绪", change: func(environment *models.RuntimeEnvironment) {
			environment.Detection.State = agentapi.DetectionStateUnavailable
		}},
		{name: "尚未设为默认", change: func(environment *models.RuntimeEnvironment) {
			environment.IsDefault = false
		}},
		{name: "只有安装证据", change: func(environment *models.RuntimeEnvironment) {
			environment.Detection.Models = []agentapi.ModelOption{}
		}},
		{name: "没有默认模型", change: func(environment *models.RuntimeEnvironment) {
			environment.DefaultModel = ""
		}},
		{name: "默认模型已失效", change: func(environment *models.RuntimeEnvironment) {
			environment.DefaultModel = "removed-model"
		}},
		{name: "默认权限不支持", change: func(environment *models.RuntimeEnvironment) {
			environment.DefaultPermissionLevel = agentapi.PermissionLevelAuto
		}},
		{name: "旧目录仍在但刷新失败", change: func(environment *models.RuntimeEnvironment) {
			environment.Detection.Diagnostics = []agentapi.Diagnostic{{Code: agentapi.DiagnosticCodeACPOptionsUnavailable}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := ready
			if test.change != nil {
				test.change(&environment)
			}
			if got := canCompleteStartupSetup(environment); got != test.allowed {
				t.Fatalf("canCompleteStartupSetup() = %v, want %v", got, test.allowed)
			}
		})
	}
}

func TestUpdateRuntimeEnvironmentRejectsManagedACPAccount(t *testing.T) {
	runtimeUpdateFixture(t)
	ctx, _ := gin.CreateTestContext(nil)
	for _, provider := range []string{constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder, constants.RuntimeIDQoderCN} {
		if err := global.RuntimeEnvironmentControllerManager.Ensure(t.Context(), models.RuntimeEnvironment{
			ID: provider, RuntimeID: provider, BaseRuntimeID: provider, Binary: provider,
		}); err != nil {
			t.Fatal(err)
		}
		request := UpdateRuntimeEnvironment{RuntimeID: provider, Body: UpdateRuntimeEnvironmentRequest{
			DefaultAccountID: "fixture-managed-account",
		}}
		if _, err := request.Output(ctx); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
			t.Fatalf("%s accepted a managed account or checked an unrelated account record: %v", provider, err)
		}
	}
}

// TestUpdateRuntimeDefaultRejectsAccountFromAnotherProfile 验证同协议智能体配置不能互用默认账号。
func TestUpdateRuntimeDefaultRejectsAccountFromAnotherProfile(t *testing.T) {
	runtimeUpdateFixture(t)
	for _, runtimeID := range []string{"codex", "codex-profile"} {
		if err := global.RuntimeEnvironmentControllerManager.Ensure(t.Context(), models.RuntimeEnvironment{ID: runtimeID, RuntimeID: runtimeID, BaseRuntimeID: constants.RuntimeIDCodex, Binary: "codex"}); err != nil {
			t.Fatal(err)
		}
	}
	accountID := "0123456789abcdef0123456789abcdef"
	account := models.AIAccount{ID: accountID, RuntimeID: "codex-profile", Name: "other profile", AuthMode: constants.AccountAuthModeAPIKey,
		Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}}, SelectedModels: []string{"model"}}
	// 此测试通过真实账号服务校验，避免只检查请求字段。
	accounts := global.AIAccountService
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.sqlite"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB.DB()
	t.Cleanup(func() { _ = pool.Close() })
	repository := controllers.NewAIAccountController(database.DB)
	if err := repository.Create(t.Context(), account, "fixture-key"); err != nil {
		t.Fatal(err)
	}
	global.AIAccountService = aiaccount.New(repository)
	t.Cleanup(func() { global.AIAccountService = accounts })
	ctx, _ := gin.CreateTestContext(nil)
	request := UpdateRuntimeEnvironment{RuntimeID: "codex", Body: UpdateRuntimeEnvironmentRequest{DefaultAccountID: accountID, DefaultModel: "model"}}
	if _, err := request.Output(ctx); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("runtime accepted another profile's default account: %v", err)
	}
	current, err := global.RuntimeEnvironmentControllerManager.Get(t.Context(), "codex")
	if err != nil || current.DefaultAccountID != "" || current.DefaultModel != "" {
		t.Fatalf("rejected binding partially changed defaults: %+v %v", current, err)
	}
}

func runtimeUpdateFixture(t *testing.T) {
	t.Helper()
	homeVariable := "HOME"
	if runtime.GOOS == "windows" {
		homeVariable = "USERPROFILE"
	}
	t.Setenv(homeVariable, t.TempDir())
	t.Setenv("QODER_CONFIG_DIR", "")
	t.Setenv("QODERCN_CONFIG_DIR", "")
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "runtime.sqlite"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	previousRuntimes, previousTransactions, previousAccounts := global.RuntimeEnvironmentControllerManager, global.TransactionControllerManager, global.AIAccountService
	t.Cleanup(func() {
		global.RuntimeEnvironmentControllerManager, global.TransactionControllerManager, global.AIAccountService = previousRuntimes, previousTransactions, previousAccounts
		_ = sqlDB.Close()
	})
	global.RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	global.AIAccountService = aiaccount.New(controllers.NewAIAccountController(database.DB))
}
