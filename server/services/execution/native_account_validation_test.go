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
				ID: "0123456789abcdef0123456789abcdef", RuntimeID: sample.runtimeID, Name: "selected account", AuthMode: constants.AccountAuthModeAPIKey,
				Endpoint:       models.AIAccountEndpoint{Protocol: sample.protocol, BaseURL: "https://provider.example/v1"},
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
		{"missing transport", func(_ *models.AIAccount, _ *models.RuntimeEnvironment, config *ExecutionConfiguration) {
			config.RequiredConnectorTransport = constants.RuntimeConnectorTransport("unsupported")
		}, status_error.RuntimePolicyUnsupported},
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
			account.Endpoint.Protocol = constants.AccountProtocolAnthropicMessages
		}, status_error.AIAccountConfigurationInvalid},
		{"another runtime profile", func(account *models.AIAccount, _ *models.RuntimeEnvironment, _ *ExecutionConfiguration) {
			account.RuntimeID = "gemini-another-profile"
		}, status_error.AIAccountConfigurationInvalid},
		{"missing credential", func(account *models.AIAccount, _ *models.RuntimeEnvironment, _ *ExecutionConfiguration) {
			account.HasCredential = false
		}, status_error.RuntimeAuthRequired},
	} {
		t.Run(sample.name, func(t *testing.T) {
			account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", RuntimeID: constants.RuntimeIDGemini, Name: "Google", AuthMode: constants.AccountAuthModeAPIKey,
				Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: constants.AIAccountGoogleURL}, SelectedModels: []string{"gemini-3.8-flash"}, HasCredential: true}
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

// TestManagedAccountDefaultsStayBoundToTheirAccount 验证默认模型、强度与实际账号共同解析。
func TestManagedAccountDefaultsStayBoundToTheirAccount(t *testing.T) {
	account := models.AIAccount{
		ID: "0123456789abcdef0123456789abcdef", RuntimeID: constants.RuntimeIDGemini,
		Name: "Google", AuthMode: constants.AccountAuthModeAPIKey,
		Endpoint:       models.AIAccountEndpoint{Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: constants.AIAccountGoogleURL},
		SelectedModels: []string{"first", "preferred", "runtime-default"}, DefaultModel: "preferred", DefaultReasoning: "LOW",
	}
	accounts := nativeValidationAccounts(t, account, "fixture-key")
	for _, usesDefaultAccount := range []bool{false, true} {
		environment := nativeValidationEnvironment(account.RuntimeID)
		environment.DefaultModel, environment.DefaultReasoning = "runtime-default", "HIGH"
		wantModel, wantReasoning := account.DefaultModel, account.DefaultReasoning
		if usesDefaultAccount {
			environment.DefaultAccountID = account.ID
			wantModel, wantReasoning = environment.DefaultModel, environment.DefaultReasoning
		}
		manager := &service{runtimeSummaries: validationRuntimeSummaries(account.RuntimeID), accounts: accounts, runtimeEnvironments: configurationEnvironmentReader{environment: environment}}
		resolved, err := manager.ResolveConfiguration(t.Context(), ExecutionConfiguration{RuntimeID: account.RuntimeID, AccountID: account.ID, PermissionLevel: constants.PermissionLevelFullAccess})
		if err != nil || resolved.Model != wantModel || resolved.Reasoning != wantReasoning {
			t.Fatalf("default account=%v: got %+v error=%v", usesDefaultAccount, resolved, err)
		}
		resolved, err = manager.ResolveConfiguration(t.Context(), ExecutionConfiguration{RuntimeID: account.RuntimeID, AccountID: account.ID, Model: "first", PermissionLevel: constants.PermissionLevelFullAccess})
		if err != nil || resolved.Model != "first" || resolved.Reasoning != "" {
			t.Fatalf("explicit model inherited a different model's default: %+v %v", resolved, err)
		}
	}
}

// TestNativeAccountChatAdmissionRejectsAnotherProfile 验证真实聊天入口在原生启动及耐久接纳前拒绝跨配置账号。
func TestNativeAccountChatAdmissionRejectsAnotherProfile(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", RuntimeID: "codex-profile", Name: "profile account", AuthMode: constants.AccountAuthModeAPIKey,
		Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}, SelectedModels: []string{"gpt-5"}}
	manager.execution.(*service).accounts = nativeValidationAccounts(t, account, "fixture-key")
	request := testMessageInput{NewChat: &testChatInput{WakerID: "waker-1", WorkingDirectory: t.TempDir()}, Message: userMessage{Text: "reject cross-profile account"},
		Runtime: runtimeSelection{RuntimeID: constants.RuntimeIDCodex, AccountID: account.ID, Model: "gpt-5"}}
	if _, err := manager.sendTestMessage(t.Context(), request); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("chat accepted another profile's account: %v", err)
	}
	ids, err := manager.store.ListChatIDs()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		version, err := manager.store.ReadVersion(t.Context(), id)
		if err != nil || version != 0 {
			t.Fatalf("rejected account committed chat events: %d %v", version, err)
		}
	}
}

// TestNativeThreadFastDefaultBelongsToDefaultAccount 验证环境 Fast 偏好不会被另一账号继承。
func TestNativeThreadFastDefaultBelongsToDefaultAccount(t *testing.T) {
	fast := true
	for _, defaultAccount := range []string{"", "managed-default"} {
		for _, selectedAccount := range []string{"", "managed-default", "other-managed"} {
			environment := models.RuntimeEnvironment{DefaultAccountID: defaultAccount, DefaultFast: &fast}
			request := pendingMessage{runtime: runtimeSelection{AccountID: selectedAccount}}
			thread := nativeThreadRequest("chat", agentapi.Workspace{}, nil, request, environment, agentapi.ResourceSelection{}, "key")
			if (thread.Selection.DefaultFast != nil) != (selectedAccount == defaultAccount) {
				t.Fatalf("default=%q selected=%q inherited=%v", defaultAccount, selectedAccount, thread.Selection.DefaultFast)
			}
			request.runtime.Fast = &fast
			thread = nativeThreadRequest("chat", agentapi.Workspace{}, nil, request, environment, agentapi.ResourceSelection{}, "key")
			if thread.Selection.Fast == nil || !*thread.Selection.Fast {
				t.Fatal("explicit Fast selection was lost")
			}
		}
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

func TestResolveConfigurationUsesPersistedManagedDefaultModel(t *testing.T) {
	for _, sample := range []struct {
		// name 描述默认模型来源。
		name string
		// runtimeDefault 是持久化的智能体默认模型。
		runtimeDefault string
		// accountDefault 是账号保存的默认模型。
		accountDefault string
		// configuredAccount 表示是否选择智能体默认账号。
		configuredAccount bool
		// explicit 是本轮明确请求的模型。
		explicit string
		// resolved 表示此前已接纳配置，空模型保留原生默认。
		resolved bool
		// want 是唯一有效模型选择。
		want string
	}{
		{name: "runtime selected second model", runtimeDefault: "second", accountDefault: "preferred", configuredAccount: true, want: "second"},
		{name: "invalid runtime default", runtimeDefault: "removed", accountDefault: "preferred", configuredAccount: true, want: "preferred"},
		{name: "explicit nondefault account", runtimeDefault: "second", accountDefault: "preferred", want: "preferred"},
		{name: "invalid account default", runtimeDefault: "removed", accountDefault: "removed", want: "first"},
		{name: "explicit model", runtimeDefault: "second", accountDefault: "preferred", configuredAccount: true, explicit: "first", want: "first"},
		{name: "frozen native model", runtimeDefault: "second", accountDefault: "preferred", configuredAccount: true, resolved: true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", RuntimeID: constants.RuntimeIDGemini, Name: "Google", AuthMode: constants.AccountAuthModeAPIKey, Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: constants.AIAccountGoogleURL}, SelectedModels: []string{"first", "second", "preferred"}, DefaultModel: sample.accountDefault}
			environment := nativeValidationEnvironment(constants.RuntimeIDGemini)
			environment.DefaultAccountID = "other"
			if sample.configuredAccount {
				environment.DefaultAccountID = account.ID
			}
			environment.DefaultModel = sample.runtimeDefault
			manager := &service{runtimeSummaries: validationRuntimeSummaries(constants.RuntimeIDGemini), accounts: nativeValidationAccounts(t, account, "selected-key"), runtimeEnvironments: configurationEnvironmentReader{environment: environment}}
			result, err := manager.ResolveConfiguration(t.Context(), ExecutionConfiguration{RuntimeID: constants.RuntimeIDGemini, AccountID: account.ID, Model: sample.explicit, Resolved: sample.resolved, PermissionLevel: constants.PermissionLevelDefault})
			if err != nil || result.Model != sample.want {
				t.Fatalf("effective model=%q error=%v, want %q", result.Model, err, sample.want)
			}
		})
	}
}

func TestResolveConfigurationKeepsManagedClaudeRuntimeAlias(t *testing.T) {
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", RuntimeID: constants.RuntimeIDClaude, Name: "Claude", AuthMode: constants.AccountAuthModeAPIKey, Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: constants.AIAccountAnthropicURL}, SelectedModels: []string{"first", "second"}, DefaultModel: "first", ClaudeModelRoles: map[string]string{"sonnet": "second"}}
	environment := nativeValidationEnvironment(constants.RuntimeIDClaude)
	environment.DefaultAccountID, environment.DefaultModel = account.ID, "sonnet"
	manager := &service{runtimeSummaries: validationRuntimeSummaries(constants.RuntimeIDClaude), accounts: nativeValidationAccounts(t, account, "selected-key"), runtimeEnvironments: configurationEnvironmentReader{environment: environment}}
	resolved, err := manager.ResolveConfiguration(t.Context(), ExecutionConfiguration{RuntimeID: constants.RuntimeIDClaude, AccountID: account.ID, PermissionLevel: constants.PermissionLevelDefault})
	if err != nil || resolved.Model != "sonnet" {
		t.Fatalf("managed native alias=%q error=%v, want sonnet", resolved.Model, err)
	}
}

// TestSubscriptionDefaultsSurviveFailedProbe 保留订阅默认账号的配置，且不改写已解析的空选择。
func TestSubscriptionDefaultsSurviveFailedProbe(t *testing.T) {
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", RuntimeID: constants.RuntimeIDCodex, Name: "Codex", AuthMode: constants.AccountAuthModeSubscription,
		Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}}
	environment := nativeValidationEnvironment(constants.RuntimeIDCodex)
	environment.DefaultAccountID, environment.DefaultModel, environment.DefaultReasoning = account.ID, "chosen-model", "high"
	environment.Detection = agentapi.DetectionResult{State: agentapi.DetectionStateUnavailable}
	manager := &service{runtimeSummaries: validationRuntimeSummaries(constants.RuntimeIDCodex), accounts: nativeValidationAccounts(t, account, "subscription-credential"), runtimeEnvironments: configurationEnvironmentReader{environment: environment}}
	for _, resolved := range []bool{false, true} {
		selection, err := manager.ResolveConfiguration(t.Context(), ExecutionConfiguration{RuntimeID: constants.RuntimeIDCodex, Resolved: resolved, AccountID: account.ID, PermissionLevel: constants.PermissionLevelDefault})
		if err != nil {
			t.Fatal(err)
		}
		if !resolved && (selection.Model != "chosen-model" || selection.Reasoning != "high") {
			t.Fatalf("subscription defaults were lost: %+v", selection)
		}
		if resolved && (selection.Model != "" || selection.Reasoning != "") {
			t.Fatalf("resolved native defaults were replaced: %+v", selection)
		}
	}
}
