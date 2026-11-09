package global

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	knowledgemodel "github.com/JieWaZi/NeuxBot/server/internal/knowledge/model"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
)

// resolveKnowledgeModel 只验证模型服务选择；伙伴、聊天和执行权限不参与知识整理准入。
func resolveKnowledgeModel(ctx context.Context, selection models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
	return resolveKnowledgeSelection(ctx, selection)
}

// resolveKnowledgeMaintenanceModel 验证维护使用的模型选择与账号认证。
func resolveKnowledgeMaintenanceModel(ctx context.Context, selection models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
	return resolveKnowledgeSelection(ctx, selection)
}

func resolveKnowledgeSelection(ctx context.Context, selection models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
	selected, _, err := detachedModelSelection(ctx, selection)
	return selected, err
}

// detachedModelSelection 复用同一模型和账号接纳规则，冻结启动环境。
func detachedModelSelection(ctx context.Context, selection models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, models.RuntimeEnvironment, error) {
	selection.RuntimeID = strings.TrimSpace(selection.RuntimeID)
	selection.AccountID = strings.TrimSpace(selection.AccountID)
	selection.Model = strings.TrimSpace(selection.Model)
	selection.Reasoning = strings.TrimSpace(selection.Reasoning)
	if RuntimeEnvironmentControllerManager == nil || selection.RuntimeID == "" {
		return selection, models.RuntimeEnvironment{}, status_error.KnowledgeInvalid
	}
	environment, err := RuntimeEnvironmentControllerManager.Get(ctx, selection.RuntimeID)
	if err != nil {
		return selection, environment, err
	}
	if !environment.Enabled {
		return selection, environment, status_error.RuntimeUnavailable
	}
	provider := environment.BaseRuntimeID
	if provider == "" {
		provider = environment.Detection.Runtime.BaseRuntimeID
	}
	if provider == "" {
		provider = environment.RuntimeID
	}
	if !agent.SupportsDetachedPolicy(provider) {
		return selection, environment, status_error.RuntimePolicyUnsupported
	}
	environment.BaseRuntimeID = provider
	if selection.AccountID == "" {
		selection.AccountID = environment.DefaultAccountID
	}
	if selection.AccountID == "" {
		selection.AccountID = constants.AiAccountSystemID
	}
	if selection.AccountID != constants.AiAccountSystemID {
		if AIAccountService == nil {
			return selection, environment, status_error.AIAccountConfigurationInvalid
		}
		account, err := AIAccountService.ValidateSelection(ctx, selection.AccountID, environment.BaseRuntimeID, selection.Model, selection.Reasoning)
		if err != nil {
			return selection, environment, err
		}
		if selection.Model == "" && account.AuthMode == constants.AccountAuthModeAPIKey {
			if len(account.SelectedModels) == 0 {
				return selection, environment, status_error.AIAccountConfigurationInvalid
			}
			selection.Model = account.SelectedModels[0]
		}
	} else {
		if selection.Model == "" {
			selection.Model = environment.DefaultModel
		}
	}
	return selection, environment, nil
}

// detachedConnection 提供工作区创建前已知的程序、账号和模型选择。
func detachedConnection(selection models.LibraryExecutionDefaults, environment models.RuntimeEnvironment) agent.ProviderConnectionRequest {
	executable := environment.Detection.ResolvedBinary
	if executable == "" {
		executable = environment.Binary
	}
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		if name, value, ok := strings.Cut(entry, "="); ok {
			env[name] = value
		}
	}
	return agent.ProviderConnectionRequest{
		Account: agent.AccountRequest{Provider: environment.BaseRuntimeID, AccountID: selection.AccountID,
			Executable: executable, PrefixArgs: environment.PrefixArgs, Model: selection.Model, WaitForLease: true},
		Launch: agent.ProviderLaunch{Executable: executable, PrefixArgs: environment.PrefixArgs,
			ConfigHome: environment.ConfigHome, BaseEnvironment: env},
	}
}

// newKnowledgeModelGenerator 固定本次作业的模型选择；每个调用都使用正式运行时取得独立账号租约。
func newKnowledgeModelGenerator(ctx context.Context, selection models.LibraryExecutionDefaults) (core.Generator, error) {
	if AgentThreads == nil || AIAccountService == nil {
		return nil, status_error.RuntimeUnavailable
	}
	selected, environment, err := detachedModelSelection(ctx, selection)
	if err != nil {
		return nil, err
	}
	return knowledgemodel.New(knowledgemodel.Config{
		Runtime: AgentThreads, Connection: detachedConnection(selected, environment),
		ResolveAccount: AIAccountService.ResolveAccount, Reasoning: selected.Reasoning,
	}), nil
}

// runKnowledgeMaintenance 通过共享运行时和作业级授权执行独立 Wiki 维护。
func runKnowledgeMaintenance(ctx context.Context, selection models.LibraryExecutionDefaults, input knowledgeservice.MaintenanceInvocation) (string, error) {
	if AgentThreads == nil || AIAccountService == nil || Config == nil {
		return "", status_error.RuntimeUnavailable
	}
	selected, environment, err := detachedModelSelection(ctx, selection)
	if err != nil {
		return "", err
	}
	runner := knowledgemodel.NewMaintenanceRunner(knowledgemodel.MaintenanceConfig{
		Runtime: AgentThreads, Registry: AgentCapabilities,
		Binary: agentBinaryPath(), Endpoint: "http://127.0.0.1:" + strconv.Itoa(Config.Server.Port) + constants.AgentCLIPath,
		Connection: detachedConnection(selected, environment), ResolveAccount: AIAccountService.ResolveAccount, Reasoning: selected.Reasoning,
	})
	return runner.Run(ctx, knowledgemodel.MaintenanceRequest{JobID: input.JobID, Bindings: input.Bindings, Instructions: input.Instructions, Message: input.Message, Validate: input.Validate})
}
