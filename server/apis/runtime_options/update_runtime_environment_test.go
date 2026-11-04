package runtime_options

import (
	"context"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"errors"
	"os"
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

func TestConfigurationRequiresDetectRefreshOnlyForLaunchChanges(t *testing.T) {
	current := models.RuntimeEnvironment{
		Binary:       "codex",
		PrefixArgs:   []string{"--profile", "ally"},
		ConfigHome:   "/tmp/codex-home",
		DefaultModel: "gpt-5",
	}
	permissionOnly := current
	permissionOnly.DefaultPermissionLevel = "full-access"
	if configurationRequiresDetectRefresh(current, permissionOnly) {
		t.Fatal("permission-only update should not require detect refresh")
	}
	binaryChanged := current
	binaryChanged.Binary = "/usr/local/bin/codex"
	if !configurationRequiresDetectRefresh(current, binaryChanged) {
		t.Fatal("binary change should require detect refresh")
	}
}

func TestCanCompleteStartupSetupRequiresDetectedModelsAndPermissions(t *testing.T) {
	ready := models.RuntimeEnvironment{
		Enabled:                true,
		IsDefault:              true,
		DefaultModel:           "free-model",
		DefaultPermissionLevel: agentapi.PermissionLevelFullAccess,
		CustomModels:           []string{},
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
			Binary: provider, DefaultAccountID: "fixture-managed-account",
		}}
		if _, err := request.Output(ctx); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
			t.Fatalf("%s accepted a managed account or checked an unrelated account record: %v", provider, err)
		}
	}
}

func TestUpdateRuntimeEnvironmentRejectsAliasedQoderRoots(t *testing.T) {
	runtimeUpdateFixture(t)
	root := t.TempDir()
	old, cnHome := filepath.Join(root, "global"), filepath.Join(root, "cn")
	for _, directory := range []string{old, cnHome} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias-to-cn")
	if err := os.Symlink(cnHome, alias); err != nil {
		t.Logf("symlink fixture unavailable, checking the direct path: %v", err)
		alias = cnHome
	}
	env := models.RuntimeEnvironment{
		ID: "global-qoder", RuntimeID: constants.RuntimeIDQoder, BaseRuntimeID: constants.RuntimeIDQoder, Binary: "qoder", ConfigHome: old,
	}
	for _, row := range []models.RuntimeEnvironment{
		env,
		{ID: "regional-qoder", RuntimeID: "qoder-cn-profile", BaseRuntimeID: constants.RuntimeIDQoderCN, Binary: "qodercn", ConfigHome: cnHome},
	} {
		if err := global.RuntimeEnvironmentControllerManager.Ensure(t.Context(), row); err != nil {
			t.Fatal(err)
		}
	}
	ctx, _ := gin.CreateTestContext(nil)
	request := UpdateRuntimeEnvironment{RuntimeID: env.RuntimeID, Body: UpdateRuntimeEnvironmentRequest{
		Binary: env.Binary, ConfigHome: alias, IsDefault: true,
	}}
	if _, err := request.Output(ctx); !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
		t.Fatalf("the regional runtime's aliased config root was accepted: %v", err)
	}
	current, err := global.RuntimeEnvironmentControllerManager.Get(t.Context(), env.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ConfigHome != old || current.IsDefault {
		t.Fatal("an invalid update partially changed the saved runtime")
	}
	env.ConfigHome = filepath.Join(root, "new-uncreated-root")
	if err := updateRuntimeEnvironment(t.Context(), env, false); err != nil {
		t.Fatalf("an independent first-use root was rejected: %v", err)
	}
}

func TestUpdateRuntimeEnvironmentFencesQoderPeerRootChange(t *testing.T) {
	runtimeUpdateFixture(t)
	parent := t.TempDir()
	globalHome, cnHome, shared := filepath.Join(parent, "global"), filepath.Join(parent, "cn"), filepath.Join(parent, "shared")
	environment := models.RuntimeEnvironment{
		ID: "global-qoder", RuntimeID: constants.RuntimeIDQoder, BaseRuntimeID: constants.RuntimeIDQoder, Binary: "qoder", ConfigHome: globalHome,
	}
	peer := models.RuntimeEnvironment{
		ID: "regional-qoder", RuntimeID: constants.RuntimeIDQoderCN, BaseRuntimeID: constants.RuntimeIDQoderCN, Binary: "qodercn", ConfigHome: cnHome,
	}
	for _, row := range []models.RuntimeEnvironment{environment, peer} {
		if err := global.RuntimeEnvironmentControllerManager.Ensure(t.Context(), row); err != nil {
			t.Fatal(err)
		}
	}
	global.TransactionControllerManager = beforeRuntimeUpdateTransaction{
		TransactionController: global.TransactionControllerManager,
		before: func(ctx context.Context) error {
			return global.RuntimeEnvironmentControllerManager.UpdateFields(ctx, peer.RuntimeID, map[string]any{"config_home": shared})
		},
	}
	environment.ConfigHome = shared
	if err := updateRuntimeEnvironment(t.Context(), environment, false); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("a peer update after filesystem preflight was accepted: %v", err)
	}
	saved, err := global.RuntimeEnvironmentControllerManager.Get(t.Context(), environment.RuntimeID)
	if err != nil || saved.ConfigHome != globalHome {
		t.Fatalf("the rejected configuration changed the original runtime: %v", err)
	}
}

type beforeRuntimeUpdateTransaction struct {
	// TransactionController owns the actual SQLite transaction.
	controllers.TransactionController
	// before commits the competing change after the request's preflight.
	before func(context.Context) error
}

func (t beforeRuntimeUpdateTransaction) Transaction(ctx context.Context, run func(context.Context) error) error {
	if err := t.before(ctx); err != nil {
		return err
	}
	return t.TransactionController.Transaction(ctx, run)
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
