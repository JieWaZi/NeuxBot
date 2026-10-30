package runtime_options

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	catalog "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/catalog"

	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 环境变量请求
type UpdateRuntimeEnvironmentRequest struct {
	// 默认账号
	DefaultAccountID string `json:"defaultAccountId" validate:"max=32"`
	// 启用状态
	Enabled bool `json:"enabled"`
	// 是否默认
	IsDefault bool `json:"isDefault"`
	// 可执行路径
	Binary string `json:"binary" validate:"required,max=4096"`
	// 前置参数
	PrefixArgs []string `json:"prefixArgs" validate:"max=100,dive,max=4096"`
	// 配置目录
	ConfigHome string `json:"configHome" validate:"max=4096"`
	// 自定义模型
	CustomModels []string `json:"customModels" validate:"max=50,dive,max=200"`
	// 默认模型
	DefaultModel string `json:"defaultModel" validate:"max=200"`
	// 默认推理
	DefaultReasoning string `json:"defaultReasoning" validate:"max=80"`
	// 默认权限
	DefaultPermissionLevel constants.PermissionLevel `json:"defaultPermissionLevel" validate:"required"`
}

// 更新环境变量
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
	u.Body.Binary = strings.TrimSpace(u.Body.Binary)
	u.Body.ConfigHome = strings.TrimSpace(u.Body.ConfigHome)
	u.Body.CustomModels = normalizeCustomModels(u.Body.CustomModels)
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
	if !catalog.LaunchOverrideArgumentsAllowed(current.BaseRuntimeID, u.Body.PrefixArgs) {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	releaseAccount := func() {}
	if u.Body.DefaultAccountID != "" {
		release, err := global.AIAccountService.Exclusive(u.Body.DefaultAccountID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		releaseAccount = release
		defer release()
	}
	if err := global.AIAccountService.Validate(ctx, u.Body.DefaultAccountID); err != nil {
		return nil, ginx.WithStack(err)
	}
	environment := models.RuntimeEnvironment{
		DefaultAccountID:       u.Body.DefaultAccountID,
		RuntimeID:              u.RuntimeID,
		Enabled:                u.Body.Enabled,
		Binary:                 u.Body.Binary,
		PrefixArgs:             append([]string(nil), u.Body.PrefixArgs...),
		ConfigHome:             u.Body.ConfigHome,
		CustomModels:           append([]string(nil), u.Body.CustomModels...),
		DefaultModel:           u.Body.DefaultModel,
		DefaultReasoning:       u.Body.DefaultReasoning,
		DefaultPermissionLevel: u.Body.DefaultPermissionLevel,
	}
	if err := updateRuntimeEnvironment(ctx, environment, u.Body.IsDefault); err != nil {
		return nil, ginx.WithStack(err)
	}
	releaseAccount()
	if configurationRequiresDetectRefresh(current, environment) || current.DefaultAccountID != environment.DefaultAccountID {
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
	installed := environment.Detection.ResolvedBinary != "" && environment.Detection.State == runtimeconfig.DetectionStateReady
	if !configured || !installed {
		return false
	}
	for _, diagnostic := range environment.Detection.Diagnostics {
		if diagnostic.Code == runtimeconfig.DiagnosticCodeACPOptionsUnavailable {
			return false
		}
	}
	permissions := runtimeconfig.ModelPermissionLevels(
		environment.Detection,
		environment.CustomModels,
		environment.DefaultModel,
	)
	return runtimeconfig.PermissionLevelSupported(permissions, environment.DefaultPermissionLevel)
}

func configurationRequiresDetectRefresh(
	current models.RuntimeEnvironment,
	next models.RuntimeEnvironment,
) bool {
	return !catalog.LaunchDetectInputsEqual(
		catalog.LaunchDetectInput{
			Binary:     current.Binary,
			PrefixArgs: current.PrefixArgs,
			ConfigHome: current.ConfigHome,
		},
		catalog.LaunchDetectInput{
			Binary:     next.Binary,
			PrefixArgs: next.PrefixArgs,
			ConfigHome: next.ConfigHome,
		},
	)
}

func normalizeCustomModels(models []string) []string {
	result := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	return result
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
		return global.RuntimeEnvironmentControllerManager.UpdateFields(
			transactionContext,
			environment.RuntimeID,
			map[string]any{"is_default": true},
		)
	})
}
