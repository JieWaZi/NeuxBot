package execution

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

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
			manager := &service{runtimeSummaries: validationRuntimeSummaries(sample.runtimeID), accounts: accounts, runtimeEnvironments: configurationEnvironmentReader{environment: nativeValidationEnvironment(sample.runtimeID)}}
			for _, selectedModel := range []string{sample.model, ""} {
				config := ExecutionConfiguration{RuntimeID: sample.runtimeID, AccountID: account.ID, Model: selectedModel, Reasoning: "LOW", PermissionLevel: constants.PermissionLevelFullAccess}
				resolved, err := manager.ResolveConfiguration(context.Background(), config)
				if err != nil {
					t.Fatalf("account model rejected before native initialization: model=%q error=%v", selectedModel, err)
				}
				if !resolved.Resolved || resolved.AccountID != account.ID || resolved.Model != sample.model || resolved.Reasoning != "LOW" || resolved.PermissionLevel != constants.PermissionLevelFullAccess {
					t.Fatalf("resolved configuration lost account selection: %+v", resolved)
				}
				if err := manager.ValidateConfiguration(context.Background(), resolved); err != nil {
					t.Fatalf("frozen account configuration rejected: %v", err)
				}
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
			environment.Detection.State = agentapi.DetectionStateUnavailable
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
			manager := &service{runtimeSummaries: validationRuntimeSummaries(constants.RuntimeIDGemini), accounts: nativeValidationAccounts(t, account, credential), runtimeEnvironments: configurationEnvironmentReader{environment: environment}}
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
		Detection: agentapi.DetectionResult{State: agentapi.DetectionStateReady, Models: []agentapi.ModelOption{{
			ID: "auto", ReasoningOptions: []agentapi.ReasoningOption{{ID: "medium"}}, PermissionLevels: []agentapi.PermissionLevel{agentapi.PermissionLevelDefault},
		}}},
	}
}

func validationRuntimeSummaries(runtimeID string) []agentapi.RuntimeSummary {
	return []agentapi.RuntimeSummary{{ID: runtimeID, BaseRuntimeID: runtimeID, InstructionFileName: "AGENTS.md"}}
}
