package runtime_options

import (
	"context"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"errors"
	"net/http/httptest"
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
	"github.com/JieWaZi/NeuxBot/server/services/runtimeenvironment"
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

func TestCanCompleteStartupSetupUsesExplicitConfiguration(t *testing.T) {
	ready := models.RuntimeEnvironment{
		Enabled:                true,
		Binary:                 "opencode",
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
		{name: "所选智能体探测未就绪", allowed: true, change: func(environment *models.RuntimeEnvironment) {
			environment.Detection.State = agentapi.DetectionStateUnavailable
		}},
		{name: "没有安装探测证据", allowed: true, change: func(environment *models.RuntimeEnvironment) {
			environment.Detection.ResolvedBinary = ""
		}},
		{name: "已禁用", change: func(environment *models.RuntimeEnvironment) { environment.Enabled = false }},
		{name: "未配置命令", change: func(environment *models.RuntimeEnvironment) { environment.Binary = "" }},
		{name: "无效权限", change: func(environment *models.RuntimeEnvironment) { environment.DefaultPermissionLevel = "invalid" }},
		{name: "尚未设为默认", change: func(environment *models.RuntimeEnvironment) {
			environment.IsDefault = false
		}},
		{name: "只有安装证据", allowed: true, change: func(environment *models.RuntimeEnvironment) {
			environment.Detection.Models = []agentapi.ModelOption{}
		}},
		{name: "使用原生默认模型", allowed: true, change: func(environment *models.RuntimeEnvironment) {
			environment.DefaultModel = ""
		}},
		{name: "默认模型未在目录中", allowed: true, change: func(environment *models.RuntimeEnvironment) {
			environment.DefaultModel = "removed-model"
		}},
		{name: "目录未声明所选权限", allowed: true, change: func(environment *models.RuntimeEnvironment) {
			environment.DefaultPermissionLevel = agentapi.PermissionLevelAuto
		}},
		{name: "旧目录仍在但刷新失败", allowed: true, change: func(environment *models.RuntimeEnvironment) {
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

func runtimeUpdateFixture(t *testing.T) controllers.AIAccountController {
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
	accounts := controllers.NewAIAccountController(database.DB)
	global.AIAccountService = aiaccount.New(accounts, t.TempDir())
	return accounts
}

// runtimeUpdateRefreshRecorder 只替代外部 CLI 探测，账号及配置保存使用真实实现。
type runtimeUpdateRefreshRecorder struct {
	// Service 提供本测试不使用的生命周期接口。
	runtimeenvironment.Service
	// refreshed 记录配置更新后请求探测的智能体。
	refreshed []string
}

// RefreshOne 记录外部探测请求，不启动本机智能体。
func (r *runtimeUpdateRefreshRecorder) RefreshOne(_ context.Context, runtimeID string) error {
	r.refreshed = append(r.refreshed, runtimeID)
	return nil
}

// TestUpdateRuntimeEnvironmentKeepsActiveAccountLease 验证普通默认配置和启动设置保存不争用账号租约。
func TestUpdateRuntimeEnvironmentKeepsActiveAccountLease(t *testing.T) {
	for _, launchChanged := range []bool{false, true} {
		t.Run(map[bool]string{false: "默认设置", true: "下一次启动设置"}[launchChanged], func(t *testing.T) {
			accounts := runtimeUpdateFixture(t)
			account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", Name: "managed", AuthMode: constants.AccountAuthModeAPIKey,
				Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: "https://example.invalid"}, SelectedModels: []string{"first", "second"}}
			if err := accounts.Create(t.Context(), account, "fixture-credential"); err != nil {
				t.Fatal(err)
			}
			oldFast, newFast := false, true
			environment := models.RuntimeEnvironment{ID: "runtime", RuntimeID: constants.RuntimeIDClaude, BaseRuntimeID: constants.RuntimeIDClaude, Enabled: true,
				Binary: "claude", DefaultAccountID: account.ID, DefaultModel: "first", DefaultReasoning: "low", DefaultFast: &oldFast, DefaultPermissionLevel: constants.PermissionLevelDefault}
			if err := global.RuntimeEnvironmentControllerManager.Ensure(t.Context(), environment); err != nil {
				t.Fatal(err)
			}
			cwd := t.TempDir()
			selected, err := global.AIAccountService.ResolveAccount(t.Context(), agentapi.AccountRequest{
				Provider: environment.BaseRuntimeID, AccountID: account.ID, Model: environment.DefaultModel, Executable: environment.Binary, CWD: cwd,
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			lease, err := global.AIAccountService.AcquireAccount(t.Context(), selected)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := lease.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			closes := 0
			global.AIAccountService.SetIdleSessionCloser(func(context.Context, string) error { closes++; return status_error.AIAccountBusy })
			refresh := &runtimeUpdateRefreshRecorder{}
			previousService := global.RuntimeEnvironmentService
			global.RuntimeEnvironmentService = refresh
			t.Cleanup(func() { global.RuntimeEnvironmentService = previousService })
			request := UpdateRuntimeEnvironment{RuntimeID: environment.RuntimeID, Body: UpdateRuntimeEnvironmentRequest{
				Enabled: true, Binary: environment.Binary, DefaultAccountID: account.ID, DefaultModel: "second", DefaultReasoning: "high", DefaultFast: &newFast, DefaultPermissionLevel: constants.PermissionLevelFullAccess,
			}}
			if launchChanged {
				request.Body.Binary = "/next/bin/claude"
				request.Body.ConfigHome = filepath.Join(t.TempDir(), "next-config")
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("PUT", "/", nil)
			if _, err := request.Output(ctx); err != nil {
				t.Fatalf("ordinary runtime save blocked by active account: %v", err)
			}
			saved, err := global.RuntimeEnvironmentControllerManager.Get(t.Context(), environment.RuntimeID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.DefaultModel != "second" || saved.DefaultReasoning != "high" || saved.DefaultFast == nil || !*saved.DefaultFast || saved.DefaultPermissionLevel != constants.PermissionLevelFullAccess || closes != 0 {
				t.Fatalf("ordinary save changed account ownership or lost defaults: %+v closes=%d", saved, closes)
			}
			if saved.Binary != request.Body.Binary || saved.ConfigHome != request.Body.ConfigHome || configurationRequiresDetectRefresh(environment, saved) != launchChanged || len(refresh.refreshed) != map[bool]int{false: 0, true: 1}[launchChanged] {
				t.Fatalf("launch configuration was not saved for the next preparation: %+v refresh=%v", saved, refresh.refreshed)
			}
			next, err := global.AIAccountService.ResolveAccount(t.Context(), agentapi.AccountRequest{
				Provider: saved.BaseRuntimeID, AccountID: saved.DefaultAccountID, Model: saved.DefaultModel, Executable: saved.Binary, PrefixArgs: saved.PrefixArgs, CWD: cwd,
			}, saved.ConfigHome)
			if err != nil {
				t.Fatal(err)
			}
			if selected.Executable != "claude" || selected.Model != "first" || next.Executable != request.Body.Binary || next.Model != "second" || next.Subject != selected.Subject || next.AuthGeneration != selected.AuthGeneration {
				t.Fatalf("saved launch changed an accepted identity or was not read next: previous=%+v next=%+v", selected, next)
			}
			nextLease, err := global.AIAccountService.AcquireAccount(t.Context(), next)
			if err != nil {
				t.Fatalf("next preparation could not share the unchanged account identity: %v", err)
			}
			if err := nextLease.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			persistedAccount, err := accounts.Get(t.Context(), account.ID)
			if err != nil || persistedAccount.AuthGeneration != selected.AuthGeneration || closes != 0 {
				t.Fatalf("ordinary runtime save changed account authentication: %+v closes=%d error=%v", persistedAccount, closes, err)
			}
		})
	}
}
