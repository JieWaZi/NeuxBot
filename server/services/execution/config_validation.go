package execution

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ValidateConfiguration 校验下一次执行的程序与账号准入，原生组合由会话初始化确认。
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
	return m.resolveConfiguration(ctx, config, environment)
}

// resolveConfiguration 使预检和发送消费同一环境快照与默认选择规则。
func (m *service) resolveConfiguration(ctx context.Context, config ExecutionConfiguration, environment models.RuntimeEnvironment) (ExecutionConfiguration, error) {
	if config.RuntimeID == "" || !constants.SelectablePermissionLevel(config.PermissionLevel) {
		return config, status_error.BadRequest
	}
	if err := validateRuntimeAvailability(environment, config); err != nil {
		return config, err
	}
	if config.AccountID == "" && !config.Resolved {
		config.AccountID = environment.DefaultAccountID
	}
	if config.AccountID == "" {
		config.AccountID = constants.AiAccountSystemID
	}
	defaultAccountID := environment.DefaultAccountID
	if defaultAccountID == "" {
		defaultAccountID = constants.AiAccountSystemID
	}
	provider := nativeProvider(environment)
	if config.AccountID != constants.AiAccountSystemID {
		if m.accounts == nil {
			return config, status_error.AIAccountConfigurationInvalid
		}
		account, err := m.accounts.ValidateSelection(ctx, config.AccountID, provider, config.Model, config.Reasoning)
		if err != nil {
			return config, err
		}
		if config.Model == "" && !config.Resolved && account.AuthMode == constants.AccountAuthModeAPIKey {
			candidates := []string{account.DefaultModel}
			if config.AccountID == defaultAccountID {
				candidates = append([]string{environment.DefaultModel}, candidates...)
			}
			candidates = append(candidates, account.SelectedModels...)
			for _, candidate := range candidates {
				allowed := slices.Contains(account.SelectedModels, candidate)
				if provider == constants.RuntimeIDClaude {
					_, allowed = aiaccount.ManagedClaudeModelTarget(account, candidate)
				}
				if candidate != "" && allowed {
					config.Model = candidate
					break
				}
			}
		}
		if config.Model == "" && !config.Resolved && account.AuthMode == constants.AccountAuthModeSubscription && config.AccountID == defaultAccountID {
			config.Model = environment.DefaultModel
		}
		if config.Reasoning == "" && !config.Resolved && config.AccountID == defaultAccountID && config.Model == environment.DefaultModel {
			config.Reasoning = environment.DefaultReasoning
		}
		// 原生组合仍由会话 setter 回执确认；受管账号必须保留凭据、协议与选中模型准入。
		if _, err := m.accounts.ValidateSelection(ctx, config.AccountID, provider, config.Model, config.Reasoning); err != nil {
			return config, err
		}
	} else if !config.Resolved {
		if config.Model == "" && config.AccountID == defaultAccountID {
			config.Model = environment.DefaultModel
		}
		if config.Reasoning == "" && config.AccountID == defaultAccountID && config.Model == environment.DefaultModel {
			config.Reasoning = environment.DefaultReasoning
		}
	}
	fast := config.Fast
	if fast == nil && !config.Resolved {
		fast = environment.DefaultFast
	}
	if fast != nil {
		value := *fast
		config.Fast = &value
	}
	// 持久化的模型目录不是实际会话能力；明确请求原样交给唯一原生设置端口。
	_, err := m.resolveRuntimeWorkspaceFiles(config.RuntimeID)
	config.Resolved = true
	return config, err
}

// validateRuntimeAvailability 校验用户启用及适配器支持范围；探测结果不决定本次调用资格。
func validateRuntimeAvailability(
	environment models.RuntimeEnvironment,
	config ExecutionConfiguration,
) error {
	if !environment.Enabled {
		return status_error.RuntimeUnavailable
	}
	if config.RequiredConnectorTransport != constants.RuntimeConnectorTransportUnspecified {
		if supported, _ := agentapi.ConnectorCompatibility(nativeProvider(environment), config.RequiredConnectorTransport); !supported {
			return status_error.RuntimePolicyUnsupported
		}
	}
	return nil
}
