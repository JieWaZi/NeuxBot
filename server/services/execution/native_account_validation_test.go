package execution

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// nativeValidationRuntime 在真实账号准备之后提供外部 ACP 目录边界。
type nativeValidationRuntime struct {
	// accountRuntime 提供无关执行接口的既有测试实现。
	accountRuntime
	// testing 检查交给外部运行时的账号和模型事实。
	testing *testing.T
	// probes 记录实际执行的账号能力探测次数。
	probes int
}

// Runtimes 声明验证所需的 Gemini 指令布局。
func (r *nativeValidationRuntime) Runtimes() []runtimes.RuntimeSummary {
	return append(r.accountRuntime.Runtimes(), runtimes.RuntimeSummary{ID: "gemini", BaseRuntimeID: "gemini", InstructionFileName: "GEMINI.md", SkillDirectoryName: ".gemini"})
}

// Detect 验证实际所选模型与受管配置，而不是系统 auto 模型的能力。
func (r *nativeValidationRuntime) Detect(ctx context.Context, request runtimes.DetectRequest) runtimes.DetectionResult {
	r.probes++
	if request.Model != "gemini-3.8-flash" || request.WorkingDirectory == "" || request.LaunchOverride.Environment["GEMINI_API_KEY"] != "selected-account-key" {
		r.testing.Errorf("wrong probe target: model=%q cwd=%q", request.Model, request.WorkingDirectory)
	}
	home := request.LaunchOverride.Environment["GEMINI_CLI_HOME"]
	if _, err := os.Stat(filepath.Join(home, ".gemini", "trustedFolders.json")); err != nil {
		r.testing.Error(err)
	}
	return runtimes.DetectionResult{State: runtimes.DetectionStateReady, DetectedDefaultModel: request.Model, Models: []runtimes.ModelOption{{ID: request.Model, ReasoningOptions: []runtimes.ReasoningOption{{ID: "LOW", Name: "Low"}}, PermissionLevels: []runtimes.PermissionLevel{runtimes.PermissionLevelDefault}}}}
}

// TestNativeAccountValidationUsesSelectedModel 回归账号选择不能被系统默认模型或旧的自定义等级否决。
func TestNativeAccountValidationUsesSelectedModel(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	accounts := controllers.NewAIAccountController(database.DB)
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", Name: "Google", AuthMode: constants.AccountAuthModeAPIKey, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: constants.AIAccountGoogleURL}}, SelectedModels: []string{"gemini-3.8-flash"}, ModelReasoningLevels: map[string][]string{"gemini-3.8-flash": {"high"}}}
	if err := accounts.Create(context.Background(), account, "selected-account-key"); err != nil {
		t.Fatal(err)
	}
	runtime := &nativeValidationRuntime{testing: t}
	manager := &service{runtime: runtime, accounts: aiaccount.New(accounts), runtimeEnvironments: accountRuntimeEnvironmentReader{}}
	config := ExecutionConfiguration{RuntimeID: "gemini", AccountID: account.ID, Model: "gemini-3.8-flash", Reasoning: "LOW", PermissionLevel: runtimes.PermissionLevelDefault}
	resolved, err := manager.ResolveConfiguration(context.Background(), config)
	if err != nil {
		t.Fatalf("actual native LOW rejected: %v", err)
	}
	if resolved.AccountID != account.ID || resolved.Model != config.Model || resolved.Reasoning != config.Reasoning {
		t.Fatalf("resolved selection lost actual account configuration: %+v", resolved)
	}
	config.Reasoning = "high"
	if err := manager.ValidateConfiguration(context.Background(), config); err == nil {
		t.Fatal("stored unsupported choice overrode native capability")
	}
	if runtime.probes != 2 {
		t.Fatalf("targeted probes=%d", runtime.probes)
	}
}
