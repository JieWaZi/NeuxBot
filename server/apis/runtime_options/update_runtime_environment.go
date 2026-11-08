package runtime_options

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 默认执行设置请求
type UpdateRuntimeEnvironmentRequest struct {
	// 默认账号
	DefaultAccountID string `json:"defaultAccountId" validate:"max=32"`
	// 启用状态
	Enabled bool `json:"enabled"`
	// 是否默认
	IsDefault bool `json:"isDefault"`
	// 默认模型
	DefaultModel string `json:"defaultModel" validate:"max=200"`
	// 默认推理
	DefaultReasoning string `json:"defaultReasoning" validate:"max=80"`
	// 默认 Fast
	DefaultFast *bool `json:"defaultFast,omitempty"`
	// 默认权限
	DefaultPermissionLevel constants.PermissionLevel `json:"defaultPermissionLevel" validate:"required"`
}

// 更新默认执行设置
type UpdateRuntimeEnvironment struct {
	ginx.MethodPut
	// 运行时标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
	// 请求体
	Body UpdateRuntimeEnvironmentRequest `in:"body"`
}

// Path 返回 Runtime 环境更新接口路径。
func (u *UpdateRuntimeEnvironment) Path() string { return "/:runtimeId" }

// Validate 规范化 Runtime 环境配置中的文本字段。
func (u *UpdateRuntimeEnvironment) Validate(*gin.Context) error {
	u.RuntimeID = strings.TrimSpace(u.RuntimeID)
	u.Body.DefaultModel = strings.TrimSpace(u.Body.DefaultModel)
	u.Body.DefaultReasoning = strings.TrimSpace(u.Body.DefaultReasoning)
	u.Body.DefaultPermissionLevel = constants.PermissionLevel(
		strings.TrimSpace(string(u.Body.DefaultPermissionLevel)),
	)
	return nil
}

// Output 保存配置并只重新探测当前 Runtime。
func (u *UpdateRuntimeEnvironment) Output(ctx *gin.Context) (interface{}, error) {
	current, err := global.RuntimeEnvironmentControllerManager.Get(ctx, u.RuntimeID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	accountID := strings.TrimSpace(u.Body.DefaultAccountID)
	if accountID == constants.AiAccountSystemID {
		accountID = ""
	}
	if accountconfig.ACPSystemConfigVariable(current.BaseRuntimeID) != "" {
		if accountID == constants.AiAccountSystemID {
			accountID = ""
		}
		if accountID != "" {
			return nil, ginx.WithStack(status_error.AIAccountConfigurationInvalid)
		}
	}
	releaseAccount := func() {}
	if accountID != "" {
		release, err := global.AIAccountService.Exclusive(accountID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		releaseAccount = release
		defer release()
	}
	if accountID != "" {
		account, err := global.AIAccountService.Get(ctx, accountID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if err := aiaccount.ValidateRuntimeAccount(account, current); err != nil {
			return nil, ginx.WithStack(err)
		}
		if account.AuthMode == constants.AccountAuthModeAPIKey && u.Body.DefaultModel != "" {
			if _, err := global.AIAccountService.ValidateSelection(ctx, accountID, current.BaseRuntimeID, u.Body.DefaultModel, u.Body.DefaultReasoning); err != nil {
				return nil, ginx.WithStack(err)
			}
		}
	}
	environment := models.RuntimeEnvironment{
		DefaultAccountID:       accountID,
		BaseRuntimeID:          current.BaseRuntimeID,
		RuntimeID:              u.RuntimeID,
		Enabled:                u.Body.Enabled,
		DefaultModel:           u.Body.DefaultModel,
		DefaultReasoning:       u.Body.DefaultReasoning,
		DefaultFast:            u.Body.DefaultFast,
		DefaultPermissionLevel: u.Body.DefaultPermissionLevel,
	}
	if err := updateRuntimeEnvironment(ctx, environment, u.Body.IsDefault); err != nil {
		return nil, ginx.WithStack(err)
	}
	releaseAccount()
	if current.DefaultAccountID != environment.DefaultAccountID {
		if err := global.RuntimeEnvironmentService.RefreshOne(ctx, u.RuntimeID); err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	if u.Body.IsDefault && u.Body.Enabled {
		confirmed, err := global.RuntimeEnvironmentControllerManager.Get(ctx, u.RuntimeID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if canCompleteStartupSetup(confirmed) {
			global.RuntimeEnvironmentService.CompleteStartupSetup()
		}
	}
	return ginx.CommonSuccessResponse(), nil
}

// canCompleteStartupSetup 只在所选默认智能体完成模型与权限探测后解除首次引导。
func canCompleteStartupSetup(environment models.RuntimeEnvironment) bool {
	configured := environment.Enabled && environment.IsDefault && environment.DefaultModel != ""
	installed := environment.Detection.ResolvedBinary != "" && environment.Detection.State == agentapi.DetectionStateReady
	if !configured || !installed {
		return false
	}
	for _, diagnostic := range environment.Detection.Diagnostics {
		if diagnostic.Code == agentapi.DiagnosticCodeACPOptionsUnavailable {
			return false
		}
	}
	permissions := agentapi.ModelPermissionLevels(
		environment.Detection,
		nil,
		environment.DefaultModel,
	)
	return agentapi.PermissionLevelSupported(permissions, environment.DefaultPermissionLevel)
}

func updateRuntimeEnvironment(
	ctx context.Context,
	environment models.RuntimeEnvironment,
	isDefault bool,
) error {
	if !isDefault {
		return global.RuntimeEnvironmentControllerManager.UpdateConfiguration(ctx, environment)
	}
	return global.TransactionControllerManager.Transaction(ctx, func(transactionContext context.Context) error {
		if err := global.RuntimeEnvironmentControllerManager.ClearDefault(transactionContext); err != nil {
			return err
		}
		if err := global.RuntimeEnvironmentControllerManager.UpdateConfiguration(transactionContext, environment); err != nil {
			return err
		}
		return global.RuntimeEnvironmentControllerManager.UpdateFields(transactionContext, environment.RuntimeID, map[string]any{"is_default": true})
	})
}
