package global

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	knowledgemodel "github.com/JieWaZi/NeuxBot/server/internal/knowledge/model"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
)

// resolveKnowledgeModel 只验证模型服务选择；伙伴、聊天和执行权限不参与知识整理准入。
func resolveKnowledgeModel(ctx context.Context, selection models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
	return resolveKnowledgeSelection(ctx, selection, true)
}

// resolveKnowledgeMaintenanceModel 验证可执行工具的 ACP 模型，不套用纯文本隔离能力门槛。
func resolveKnowledgeMaintenanceModel(ctx context.Context, selection models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
	return resolveKnowledgeSelection(ctx, selection, false)
}

func resolveKnowledgeSelection(ctx context.Context, selection models.LibraryExecutionDefaults, pureText bool) (models.LibraryExecutionDefaults, error) {
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
	if !environment.Enabled || environment.Detection.State != runtimes.DetectionStateReady {
		return selection, status_error.RuntimeUnavailable
	}
	if supported, known := environment.Detection.Capabilities[runtimes.CapabilityIsolatedTextGeneration]; pureText && known && !supported {
		return selection, status_error.RuntimePolicyUnsupported
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
		available := runtimes.AvailableModels(environment.Detection, environment.CustomModels)
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
	if RuntimeSDK == nil || RuntimeEnvironmentControllerManager == nil {
		return nil, status_error.RuntimeUnavailable
	}
	return knowledgemodel.New(knowledgemodel.Config{Runtime: RuntimeSDK, Model: selection.Model, Reasoning: selection.Reasoning, Prepare: func(ctx context.Context) (runtimes.RuntimeTarget, runtimes.DetectionResult, func(), error) {
		return prepareKnowledgeRuntime(ctx, selection)
	}}), nil
}

// runKnowledgeMaintenance 复用 ACP SDK、账号租约及同包 CLI 执行独立 Wiki 维护。
func runKnowledgeMaintenance(ctx context.Context, selection models.LibraryExecutionDefaults, input knowledgeservice.MaintenanceInvocation) (string, error) {
	selected, err := resolveKnowledgeMaintenanceModel(ctx, selection)
	if err != nil {
		return "", err
	}
	runner := knowledgemodel.NewMaintenanceRunner(knowledgemodel.MaintenanceConfig{
		Runtime: RuntimeSDK, Registry: AgentCapabilities,
		Binary:   agentBinaryPath(),
		Endpoint: "http://127.0.0.1:" + strconv.Itoa(Config.Server.Port) + constants.AgentCLIPath,
		Model:    selected.Model, Reasoning: selected.Reasoning,
		Prepare: func(ctx context.Context) (runtimes.RuntimeTarget, runtimes.DetectionResult, func(), error) {
			return prepareKnowledgeRuntime(ctx, selected)
		},
	})
	return runner.Run(ctx, knowledgemodel.MaintenanceRequest{JobID: input.JobID, Bindings: input.Bindings, Instructions: input.Instructions, Message: input.Message, Validate: input.Validate})
}

// prepareKnowledgeRuntime 对独立文本生成及工具维护使用相同的可取消账号租约。
func prepareKnowledgeRuntime(ctx context.Context, selection models.LibraryExecutionDefaults) (runtimes.RuntimeTarget, runtimes.DetectionResult, func(), error) {
	if RuntimeSDK == nil || RuntimeEnvironmentControllerManager == nil {
		return runtimes.RuntimeTarget{}, runtimes.DetectionResult{}, nil, status_error.RuntimeUnavailable
	}
	environment, err := RuntimeEnvironmentControllerManager.Get(ctx, selection.RuntimeID)
	if err != nil {
		return runtimes.RuntimeTarget{}, runtimes.DetectionResult{}, nil, err
	}
	if !environment.Enabled || environment.Detection.State != runtimes.DetectionStateReady {
		return runtimes.RuntimeTarget{}, runtimes.DetectionResult{}, nil, status_error.RuntimeUnavailable
	}
	target := runtimes.RuntimeTarget{RuntimeID: selection.RuntimeID, Binary: environment.Binary, PrefixArgs: append([]string(nil), environment.PrefixArgs...), ConfigHome: environment.ConfigHome, CloseAfterRun: true}
	cleanup := func() {}
	if selection.AccountID != "" && selection.AccountID != constants.AiAccountSystemID {
		directory, err := os.MkdirTemp("", "neux-knowledge-account-")
		if err != nil {
			logx.Errorf("prepare knowledge model account workspace failed: error=%v", err)
			return target, environment.Detection, nil, status_error.KnowledgeStorageError
		}
		prepared, release, err := AIAccountService.PrepareQueued(ctx, selection.AccountID, environment.BaseRuntimeID, directory, selection.Model)
		if err != nil {
			_ = os.RemoveAll(directory)
			return target, environment.Detection, nil, err
		}
		target.AccountID, target.Environment, target.Isolated = prepared.AccountID, prepared.Environment, prepared.Isolated
		target.RefreshCredentials = prepared.RefreshCredentials
		if prepared.ConfigHome != "" {
			target.ConfigHome = prepared.ConfigHome
		}
		cleanup = func() {
			release()
			if err := os.RemoveAll(directory); err != nil {
				logx.Errorf("remove knowledge model account workspace failed: error=%v", err)
			}
		}
	}
	return target, environment.Detection, cleanup, nil
}
