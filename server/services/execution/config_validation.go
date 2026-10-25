package execution

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
)

// ValidateConfiguration 校验 Runtime、模型、推理强度和权限组合可以用于下一次执行。
func (m *service) ValidateConfiguration(
	ctx context.Context,
	config ExecutionConfiguration,
) error {
	_, err := m.ResolveConfiguration(ctx, config)
	return err
}

// ResolveConfiguration 将默认选择解析为明确身份，并复用同一执行能力校验。
func (m *service) ResolveConfiguration(ctx context.Context, config ExecutionConfiguration) (ExecutionConfiguration, error) {
	config.RuntimeID = strings.TrimSpace(config.RuntimeID)
	config.AccountID = strings.TrimSpace(config.AccountID)
	config.Model = strings.TrimSpace(config.Model)
	config.Reasoning = strings.TrimSpace(config.Reasoning)
	if config.RuntimeID == "" || !constants.SelectablePermissionLevel(config.PermissionLevel) {
		return config, status_error.BadRequest
	}
	environment, err := m.runtimeEnvironments.Get(ctx, config.RuntimeID)
	if err != nil {
		return config, err
	}
	if config.AccountID == "" && !config.Resolved {
		config.AccountID = environment.DefaultAccountID
	}
	if config.AccountID == "" {
		// 显式原生登录身份阻止后续派发再次采用已变化的默认账号。
		config.AccountID = constants.AiAccountSystemID
	}
	if config.Reasoning == "" && !config.Resolved {
		config.Reasoning = environment.DefaultReasoning
		if config.AccountID == constants.AiAccountSystemID && config.Reasoning != "" {
			// 已配置的原生默认强度按目标模型解析；未配置或没有具体档位时保留空值。
			model := config.Model
			if model == "" {
				model = environment.DefaultModel
			}
			_, config.Reasoning = runtimes.NormalizeDetectedDefaults(
				environment.Detection, environment.CustomModels, model, config.Reasoning,
			)
		}
	}
	validation := config
	if config.AccountID != constants.AiAccountSystemID {
		if m.accounts == nil {
			return config, status_error.AIAccountConfigurationInvalid
		}
		account, err := m.accounts.ValidateSelection(ctx, config.AccountID, environment.BaseRuntimeID, config.Model, config.Reasoning)
		if err != nil {
			return config, err
		}
		if account.AuthMode == constants.AccountAuthModeAPIKey && config.Model == "" && !config.Resolved {
			if len(account.SelectedModels) == 0 {
				return config, status_error.AIAccountConfigurationInvalid
			}
			config.Model = account.SelectedModels[0]
			// 默认模型确定后仍按该模型校验账号声明，不能用空模型跳过推理约束。
			if _, err := m.accounts.ValidateSelection(ctx, config.AccountID, environment.BaseRuntimeID, config.Model, config.Reasoning); err != nil {
				return config, err
			}
		}
		validation = config
		if account.AuthMode == constants.AccountAuthModeAPIKey && accountconfig.IsNativeAccountRuntime(environment.BaseRuntimeID) {
			detector, ok := m.runtime.(aiaccount.ModelDetector)
			if !ok {
				return config, status_error.RuntimeUnavailable
			}
			result, err := m.accounts.DetectModel(ctx, detector, account.ID, environment, config.Model)
			if err != nil {
				return config, err
			}
			environment.Detection = result
			environment.DefaultModel = config.Model
			environment.CustomModels = nil
		} else if account.AuthMode == constants.AccountAuthModeAPIKey {
			// 账号模型不一定在原生目录中，权限仍以该环境的已探测能力验证。
			declaredReasoning := len(account.ModelReasoningLevels[config.Model]) > 0
			validation.Model = environment.DefaultModel
			if declaredReasoning {
				validation.Reasoning = ""
			}
		}
	}
	if config.Model == "" && !config.Resolved {
		config.Model = environment.DefaultModel
		validation.Model = config.Model
	}
	if err := validateExecutionConfiguration(environment, validation); err != nil {
		return config, err
	}
	_, err = m.resolveRuntimeWorkspaceFiles(config.RuntimeID)
	config.Resolved = true
	return config, err
}

// validateExecutionConfiguration 只使用一份冻结 Runtime 环境快照完成组合校验。
func validateExecutionConfiguration(
	environment models.RuntimeEnvironment,
	config ExecutionConfiguration,
) error {
	if !environment.Enabled || environment.Detection.State != constants.RuntimeDetectionStateReady {
		return status_error.RuntimeUnavailable
	}
	if config.RequiredConnectorTransport != constants.RuntimeConnectorTransportUnspecified &&
		!runtimes.DetectedConnectorTransportSupported(
			environment.Detection,
			config.RequiredConnectorTransport,
		) {
		return status_error.RuntimeUnavailable
	}
	selectedModel := strings.TrimSpace(config.Model)
	if selectedModel == "" {
		selectedModel = strings.TrimSpace(environment.DefaultModel)
	}
	modelsAvailable := runtimes.AvailableModels(environment.Detection, environment.CustomModels)
	modelIndex := -1
	for index, model := range modelsAvailable {
		if model.ID == selectedModel {
			modelIndex = index
			break
		}
	}
	if len(modelsAvailable) > 0 && modelIndex < 0 {
		return status_error.BadRequest
	}
	if !runtimePermissionSupported(environment, selectedModel, config.PermissionLevel) {
		return status_error.BadRequest
	}
	selectedReasoning := strings.TrimSpace(config.Reasoning)
	if selectedReasoning == "" {
		return nil
	}
	reasoningOptions := environment.Detection.ReasoningOptions
	if modelIndex >= 0 && len(modelsAvailable[modelIndex].ReasoningOptions) > 0 {
		reasoningOptions = modelsAvailable[modelIndex].ReasoningOptions
	}
	for _, option := range reasoningOptions {
		if option.ID == selectedReasoning {
			return nil
		}
	}
	return status_error.BadRequest
}
