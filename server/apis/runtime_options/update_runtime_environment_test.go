package runtime_options

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"errors"
	"net/http/httptest"
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

func TestCanCompleteStartupSetupUsesExplicitConfiguration(t *testing.T) {
	ready := models.RuntimeEnvironment{
		Enabled:                true,
		Binary:                 "opencode",
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
	for _, provider := range []string{constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder} {
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
		Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}, SelectedModels: []string{"model"}}
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

func runtimeUpdateFixture(t *testing.T) controllers.AIAccountController {
	t.Helper()
	homeVariable := "HOME"
	if runtime.GOOS == "windows" {
		homeVariable = "USERPROFILE"
	}
	t.Setenv(homeVariable, t.TempDir())
	t.Setenv("QODER_CONFIG_DIR", "")
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
	for _, launchChanged := range []bool{false} {
		t.Run(map[bool]string{false: "默认设置", true: "下一次启动设置"}[launchChanged], func(t *testing.T) {
			accounts := runtimeUpdateFixture(t)
			account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", RuntimeID: constants.RuntimeIDClaude, Name: "managed", AuthMode: constants.AccountAuthModeAPIKey,
				Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: "https://example.invalid"}, SelectedModels: []string{"first", "second"}}
			if err := accounts.Create(t.Context(), account, "fixture-credential"); err != nil {
				t.Fatal(err)
			}
			oldFast, newFast := false, true
			environment := models.RuntimeEnvironment{ID: "runtime", RuntimeID: constants.RuntimeIDClaude, BaseRuntimeID: constants.RuntimeIDClaude, Enabled: true,
				Binary: "claude", Detection: agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BinaryCandidates: []string{"claude"}}}, DefaultAccountID: account.ID, DefaultModel: "first", DefaultReasoning: "low", DefaultFast: &oldFast, DefaultPermissionLevel: constants.PermissionLevelDefault}
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
				Enabled: true, DefaultAccountID: account.ID, DefaultModel: "second", DefaultReasoning: "high", DefaultFast: &newFast, DefaultPermissionLevel: constants.PermissionLevelFullAccess,
			}}
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
			if saved.Binary != environment.Binary || len(refresh.refreshed) != 0 {
				t.Fatalf("ordinary settings changed launch: %+v", saved)
			}
			next, err := global.AIAccountService.ResolveAccount(t.Context(), agentapi.AccountRequest{
				Provider: saved.BaseRuntimeID, AccountID: saved.DefaultAccountID, Model: saved.DefaultModel, Executable: saved.Binary, PrefixArgs: saved.PrefixArgs, CWD: cwd,
			}, saved.ConfigHome)
			if err != nil {
				t.Fatal(err)
			}
			if selected.Executable != "claude" || selected.Model != "first" || next.Executable != environment.Binary || next.Model != "second" || next.Subject != selected.Subject || next.AuthGeneration != selected.AuthGeneration {
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

// TestRuntimeInstallationSelectionUsesDiscoveredInstances 验证版本切换只接受已发现且可用的实例。
func TestRuntimeInstallationSelectionUsesDiscoveredInstances(t *testing.T) {
	runtimeUpdateFixture(t)
	refresh := &runtimeUpdateRefreshRecorder{}
	previous := global.RuntimeEnvironmentService
	global.RuntimeEnvironmentService = refresh
	t.Cleanup(func() { global.RuntimeEnvironmentService = previous })
	environment := models.RuntimeEnvironment{ID: "qoder", RuntimeID: "qoder", BaseRuntimeID: constants.RuntimeIDQoder, Detection: agentapi.DetectionResult{ResolvedBinary: "/bin/qoder", Runtime: agentapi.RuntimeSummary{ID: "qoder", BaseRuntimeID: "qoder"}, Installations: []agentapi.CLIInstallation{{Path: "/bin/qoder", Version: "1", Available: true}, {Path: "/bin/qoderclicn", Version: "2", Available: true}, {Path: "/missing/qoder", Available: false}}}}
	if err := global.RuntimeEnvironmentControllerManager.Ensure(t.Context(), environment); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/arbitrary/cli", "/missing/qoder", "/bin/qoderclicn"} {
		request := UpdateRuntimeEnvironment{RuntimeID: "qoder", Body: UpdateRuntimeEnvironmentRequest{InstallationPath: path, Enabled: true, DefaultPermissionLevel: constants.PermissionLevelFullAccess}}
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("PUT", "/", nil)
		_, err := request.Output(ctx)
		if path != "/bin/qoderclicn" {
			if !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
				t.Fatalf("unverified selection accepted: %s %v", path, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		saved, err := global.RuntimeEnvironmentControllerManager.Get(t.Context(), "qoder")
		if err != nil {
			t.Fatal(err)
		}
		if saved.Binary != path || saved.Detection.Version != "2" || len(refresh.refreshed) != 1 {
			t.Fatalf("selection not applied: %+v refresh=%v", saved, refresh.refreshed)
		}
	}
}
