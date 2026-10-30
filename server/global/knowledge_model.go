package global

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/host"
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
	selection.RuntimeID = strings.TrimSpace(selection.RuntimeID)
	selection.AccountID = strings.TrimSpace(selection.AccountID)
	selection.Model = strings.TrimSpace(selection.Model)
	selection.Reasoning = strings.TrimSpace(selection.Reasoning)
	if RuntimeEnvironmentControllerManager == nil || selection.RuntimeID == "" {
		return selection, status_error.KnowledgeInvalid
	}
	environment, err := RuntimeEnvironmentControllerManager.Get(ctx, selection.RuntimeID)
	if err != nil {
		return selection, err
	}
	if !environment.Enabled || environment.Detection.State != runtimeconfig.DetectionStateReady {
		return selection, status_error.RuntimeUnavailable
	}
	if selection.AccountID == "" {
		selection.AccountID = environment.DefaultAccountID
	}
	if selection.AccountID == "" {
		selection.AccountID = constants.AiAccountSystemID
	}
	if selection.AccountID != constants.AiAccountSystemID {
		if AIAccountService == nil {
			return selection, status_error.AIAccountConfigurationInvalid
		}
		account, err := AIAccountService.ValidateSelection(ctx, selection.AccountID, environment.BaseRuntimeID, selection.Model, selection.Reasoning)
		if err != nil {
			return selection, err
		}
		if selection.Model == "" && account.AuthMode == constants.AccountAuthModeAPIKey {
			if len(account.SelectedModels) == 0 {
				return selection, status_error.AIAccountConfigurationInvalid
			}
			selection.Model = account.SelectedModels[0]
		}
	} else {
		if selection.Model == "" {
			selection.Model = environment.DefaultModel
		}
		available := runtimeconfig.AvailableModels(environment.Detection, environment.CustomModels)
		found := len(available) == 0
		for _, model := range available {
			found = found || model.ID == selection.Model
		}
		if !found {
			return selection, status_error.KnowledgeInvalid
		}
	}
	return selection, nil
}

// newKnowledgeModelGenerator 每个模型调用单独准备账号租约，调用结束后回收配置目录。
func newKnowledgeModelGenerator(_ context.Context, selection models.LibraryExecutionDefaults) (core.Generator, error) {
	if RuntimeCatalog == nil || RuntimeEnvironmentControllerManager == nil || KnowledgeRuntimeHost == nil {
		return nil, status_error.RuntimeUnavailable
	}
	return knowledgemodel.New(knowledgemodel.Config{Runtime: KnowledgeRuntimeHost, RuntimeID: selection.RuntimeID, AccountID: selection.AccountID, Model: selection.Model, Reasoning: selection.Reasoning}), nil
}

// runKnowledgeMaintenance 通过共享 Runtime 宿主和单任务 CLI 授权执行独立 Wiki 维护。
func runKnowledgeMaintenance(ctx context.Context, selection models.LibraryExecutionDefaults, input knowledgeservice.MaintenanceInvocation) (string, error) {
	selected, err := resolveKnowledgeMaintenanceModel(ctx, selection)
	if err != nil {
		return "", err
	}
	runner := knowledgemodel.NewMaintenanceRunner(knowledgemodel.MaintenanceConfig{
		Runtime: KnowledgeRuntimeHost, Registry: AgentCapabilities,
		Binary:    agentBinaryPath(),
		Endpoint:  "http://127.0.0.1:" + strconv.Itoa(Config.Server.Port) + constants.AgentCLIPath,
		RuntimeID: selected.RuntimeID, AccountID: selected.AccountID,
		Model: selected.Model, Reasoning: selected.Reasoning,
	})
	return runner.Run(ctx, knowledgemodel.MaintenanceRequest{JobID: input.JobID, Bindings: input.Bindings, Instructions: input.Instructions, Message: input.Message, Validate: input.Validate})
}

// newKnowledgeRuntimeHost 只在产品边界读取环境和账号，独立 Runtime 不导入业务对象。
func newKnowledgeRuntimeHost() *runtimehost.Host {
	return runtimehost.New(runtimehost.Config{Launch: RuntimeCatalog.PrepareLaunch, Resolve: func(ctx context.Context, input runtimehost.TextRequest) (runtimehost.Environment, error) {
		environment, err := RuntimeEnvironmentControllerManager.Get(ctx, input.RuntimeID)
		if err != nil {
			return runtimehost.Environment{}, err
		}
		if !environment.Enabled || environment.Detection.State != runtimeconfig.DetectionStateReady {
			return runtimehost.Environment{}, status_error.RuntimeUnavailable
		}
		binary := environment.Binary
		if binary == "" {
			binary = environment.Detection.ResolvedBinary
		}
		return runtimehost.Environment{BaseRuntimeID: environment.BaseRuntimeID, Detection: environment.Detection, Target: runtimeconfig.RuntimeTarget{RuntimeID: input.RuntimeID, Binary: binary, PrefixArgs: append([]string(nil), environment.PrefixArgs...), ConfigHome: environment.ConfigHome, CloseAfterRun: true}}, nil
	}, Prepare: func(ctx context.Context, input runtimehost.TextRequest, environment runtimehost.Environment, workspace string) (runtimeconfig.RuntimeTarget, func(context.Context) error, error) {
		target := environment.Target
		if input.AccountID == "" || input.AccountID == constants.AiAccountSystemID {
			return target, func(context.Context) error { return nil }, nil
		}
		if AIAccountService == nil {
			return target, nil, status_error.AIAccountConfigurationInvalid
		}
		prepared, release, err := AIAccountService.PrepareQueuedLease(ctx, input.AccountID, environment.BaseRuntimeID, workspace, input.Model)
		if err != nil {
			return target, nil, err
		}
		target.AccountID, target.Environment, target.Isolated = prepared.AccountID, prepared.Environment, prepared.Isolated
		target.RefreshCredentials = prepared.RefreshCredentials
		if prepared.ConfigHome != "" {
			target.ConfigHome = prepared.ConfigHome
		}
		return target, release, nil
	}})
}
