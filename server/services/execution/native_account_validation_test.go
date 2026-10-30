package execution

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/host"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// nativeValidationRuntime 模拟仍提供显式设置探测端口的宿主。
type nativeValidationRuntime struct {
	// ChatHost 保留真实聊天宿主与工作区目录查询行为。
	*runtimehost.ChatHost
	// probes 记录配置准入不应调用的额外能力探测。
	probes int
}

// Detect 使额外探测返回不可用，证明执行配置不依赖设置查询的成功。
func (r *nativeValidationRuntime) Detect(context.Context, runtimeconfig.DetectRequest) runtimeconfig.DetectionResult {
	r.probes++
	return runtimeconfig.DetectionResult{State: runtimeconfig.DetectionStateUnavailable}
}

// TestNativeAccountValidationDoesNotProbe 防止配置准入要求真实 ChatHost 提供探测接口或启动额外会话。
func TestNativeAccountValidationDoesNotProbe(t *testing.T) {
	for _, sample := range []struct {
		// runtimeID 是账号支持的原生智能体类型。
		runtimeID string
		// protocol 是该智能体可以直接使用的账号协议。
		protocol constants.AccountProtocol
		// model 是账号允许但系统默认目录未列出的原始模型标识。
		model string
	}{
		{constants.RuntimeIDGemini, constants.AccountProtocolGoogleGenerativeAI, "gemini-3.8-flash"},
		{constants.RuntimeIDOpenCode, constants.AccountProtocolOpenAICompletions, "account-model"},
		{constants.RuntimeIDGrok, constants.AccountProtocolOpenAICompletions, "grok-account-model"},
	} {
		t.Run(sample.runtimeID, func(t *testing.T) {
			account := models.AIAccount{
				ID: "0123456789abcdef0123456789abcdef", Name: "selected account", AuthMode: constants.AccountAuthModeAPIKey,
				Endpoints:      []models.AIAccountEndpoint{{Protocol: sample.protocol, BaseURL: "https://provider.example/v1"}},
				SelectedModels: []string{sample.model}, ModelReasoningLevels: map[string][]string{sample.model: {"high"}},
			}
			accounts := nativeValidationAccounts(t, account, "selected-account-key")
			for _, withDetector := range []bool{false, true} {
				t.Run(map[bool]string{false: "real ChatHost without Detect", true: "unused explicit detector"}[withDetector], func(t *testing.T) {
					host := nativeValidationHost(t, sample.runtimeID)
					defer host.Close(context.Background())
					manager := &service{runtime: host, accounts: accounts, runtimeEnvironments: configurationEnvironmentReader{environment: nativeValidationEnvironment(sample.runtimeID)}}
					detector := &nativeValidationRuntime{ChatHost: host}
					if withDetector {
						manager.runtime = detector
					}
					for _, selectedModel := range []string{sample.model, ""} {
						config := ExecutionConfiguration{RuntimeID: sample.runtimeID, AccountID: account.ID, Model: selectedModel, Reasoning: "LOW", PermissionLevel: constants.PermissionLevelFullAccess}
						for attempt := 0; attempt < 2; attempt++ {
							resolved, err := manager.ResolveConfiguration(context.Background(), config)
							if err != nil {
								t.Fatalf("account model rejected before native initialization: model=%q error=%v", selectedModel, err)
							}
							if !resolved.Resolved || resolved.AccountID != account.ID || resolved.Model != sample.model || resolved.Reasoning != "LOW" || resolved.PermissionLevel != constants.PermissionLevelFullAccess {
								t.Fatalf("resolved configuration lost account selection: %+v", resolved)
							}
							config = resolved
							if err := manager.ValidateConfiguration(context.Background(), config); err != nil {
								t.Fatalf("frozen account configuration rejected: %v", err)
							}
						}
						if detector.probes != 0 {
							t.Fatalf("configuration admission started %d extra probes", detector.probes)
						}
					}
				})
			}
		})
	}
}

// TestNativeAccountValidationKeepsAdmissionGuards 覆盖不探测仍须保留的账号、程序、连接器与权限准入。
func TestNativeAccountValidationKeepsAdmissionGuards(t *testing.T) {
	for _, sample := range []struct {
		// name 描述被破坏的准入条件。
		name string
		// mutate 修改合法账号、环境或执行配置中的一个事实。
		mutate func(*models.AIAccount, *models.RuntimeEnvironment, *ExecutionConfiguration)
		// want 是必须保留的对外拒绝类别。
		want error
	}{
		{"disabled CLI", func(_ *models.AIAccount, environment *models.RuntimeEnvironment, _ *ExecutionConfiguration) {
			environment.Enabled = false
		}, status_error.RuntimeUnavailable},
		{"unavailable CLI", func(_ *models.AIAccount, environment *models.RuntimeEnvironment, _ *ExecutionConfiguration) {
			environment.Detection.State = runtimeconfig.DetectionStateUnavailable
		}, status_error.RuntimeUnavailable},
		{"missing transport", func(_ *models.AIAccount, _ *models.RuntimeEnvironment, config *ExecutionConfiguration) {
			config.RequiredConnectorTransport = constants.RuntimeConnectorTransportStreamableHTTP
		}, status_error.RuntimeUnavailable},
		{"invalid permission", func(_ *models.AIAccount, _ *models.RuntimeEnvironment, config *ExecutionConfiguration) {
			config.PermissionLevel = constants.PermissionLevel("invalid")
		}, status_error.BadRequest},
		{"unselected account model", func(_ *models.AIAccount, _ *models.RuntimeEnvironment, config *ExecutionConfiguration) {
			config.Model = "unselected"
		}, status_error.BadRequest},
		{"empty account models", func(account *models.AIAccount, _ *models.RuntimeEnvironment, _ *ExecutionConfiguration) {
			account.SelectedModels = nil
		}, status_error.AIAccountConfigurationInvalid},
		{"incompatible protocol", func(account *models.AIAccount, _ *models.RuntimeEnvironment, _ *ExecutionConfiguration) {
			account.Endpoints[0].Protocol = constants.AccountProtocolAnthropicMessages
		}, status_error.AIAccountConfigurationInvalid},
		{"missing credential", func(account *models.AIAccount, _ *models.RuntimeEnvironment, _ *ExecutionConfiguration) {
			account.HasCredential = false
		}, status_error.RuntimeAuthRequired},
		{"system model permission", func(_ *models.AIAccount, _ *models.RuntimeEnvironment, config *ExecutionConfiguration) {
			config.AccountID, config.Model, config.Reasoning = constants.AiAccountSystemID, "auto", ""
		}, status_error.BadRequest},
	} {
		t.Run(sample.name, func(t *testing.T) {
			account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", Name: "Google", AuthMode: constants.AccountAuthModeAPIKey,
				Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: constants.AIAccountGoogleURL}}, SelectedModels: []string{"gemini-3.8-flash"}, HasCredential: true}
			environment := nativeValidationEnvironment(constants.RuntimeIDGemini)
			config := ExecutionConfiguration{RuntimeID: constants.RuntimeIDGemini, AccountID: account.ID, Model: "gemini-3.8-flash", Reasoning: "LOW", PermissionLevel: constants.PermissionLevelFullAccess}
			sample.mutate(&account, &environment, &config)
			credential := "selected-account-key"
			if !account.HasCredential {
				credential = ""
			}
			host := nativeValidationHost(t, constants.RuntimeIDGemini)
			defer host.Close(context.Background())
			manager := &service{runtime: host, accounts: nativeValidationAccounts(t, account, credential), runtimeEnvironments: configurationEnvironmentReader{environment: environment}}
			if err := manager.ValidateConfiguration(context.Background(), config); !errors.Is(err, sample.want) {
				t.Fatalf("admission error=%v, want %v", err, sample.want)
			}
		})
	}
}

// nativeValidationAccounts 使用真实临时数据库保存账号及凭据准入事实。
func nativeValidationAccounts(t *testing.T, account models.AIAccount, credential string) *aiaccount.Service {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	accounts := controllers.NewAIAccountController(database.DB)
	if err := accounts.Create(context.Background(), account, credential); err != nil {
		t.Fatal(err)
	}
	return aiaccount.New(accounts)
}

// nativeValidationEnvironment 保留与账号模型无关的系统默认模型能力，暴露误用默认目录的拒绝。
func nativeValidationEnvironment(runtimeID string) models.RuntimeEnvironment {
	return models.RuntimeEnvironment{
		RuntimeID: runtimeID, BaseRuntimeID: runtimeID, Enabled: true, DefaultModel: "auto",
		Detection: runtimeconfig.DetectionResult{State: runtimeconfig.DetectionStateReady, Models: []runtimeconfig.ModelOption{{
			ID: "auto", ReasoningOptions: []runtimeconfig.ReasoningOption{{ID: "medium"}}, PermissionLevels: []runtimeconfig.PermissionLevel{runtimeconfig.PermissionLevelDefault},
		}}},
	}
}

// nativeValidationHost 提供真实聊天宿主，任何配置准入发起的原生启动都会立即失败。
func nativeValidationHost(t *testing.T, runtimeID string) *runtimehost.ChatHost {
	t.Helper()
	return runtimehost.NewChat(runtimehost.ChatConfig{
		Runtimes: func() []runtimeconfig.RuntimeSummary {
			return []runtimeconfig.RuntimeSummary{{ID: runtimeID, BaseRuntimeID: runtimeID, InstructionFileName: "AGENTS.md"}}
		},
		Launch: func(runtimeconfig.RuntimeTarget, runtimeconfig.DetectionResult) (runtimeconfig.LaunchFacts, error) {
			t.Error("configuration admission started a native process")
			return runtimeconfig.LaunchFacts{}, status_error.RuntimeUnavailable
		},
	})
}
