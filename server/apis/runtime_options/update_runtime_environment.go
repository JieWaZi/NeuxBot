package runtime_options

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"slices"
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
	// 默认 Fast
	DefaultFast *bool `json:"defaultFast,omitempty"`
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
	if !agentapi.LaunchOverrideArgumentsAllowed(current.BaseRuntimeID, u.Body.PrefixArgs) {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	accountID := u.Body.DefaultAccountID
	if accountconfig.ACPSystemConfigVariable(current.BaseRuntimeID) != "" {
		if accountID == constants.AiAccountSystemID {
			accountID = ""
		}
		if accountID != "" {
			return nil, ginx.WithStack(status_error.AIAccountConfigurationInvalid)
		}
	}
	// Runtime 设置只影响后续准备，不改写账号认证材料或回收现有消费者。
	if err := global.AIAccountService.Validate(ctx, accountID); err != nil {
		return nil, ginx.WithStack(err)
	}
	environment := models.RuntimeEnvironment{
		DefaultAccountID:       accountID,
		BaseRuntimeID:          current.BaseRuntimeID,
		RuntimeID:              u.RuntimeID,
		Enabled:                u.Body.Enabled,
		Binary:                 u.Body.Binary,
		PrefixArgs:             append([]string(nil), u.Body.PrefixArgs...),
		ConfigHome:             u.Body.ConfigHome,
		CustomModels:           append([]string(nil), u.Body.CustomModels...),
		DefaultModel:           u.Body.DefaultModel,
		DefaultReasoning:       u.Body.DefaultReasoning,
		DefaultFast:            u.Body.DefaultFast,
		DefaultPermissionLevel: u.Body.DefaultPermissionLevel,
	}
	if err := updateRuntimeEnvironment(ctx, environment, u.Body.IsDefault); err != nil {
		return nil, ginx.WithStack(err)
	}
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

// canCompleteStartupSetup 接受用户明确保存的合法默认配置，原生默认模型可留空，探测结果仅提供诊断。
func canCompleteStartupSetup(environment models.RuntimeEnvironment) bool {
	return environment.Enabled && environment.IsDefault && strings.TrimSpace(environment.Binary) != "" &&
		constants.SelectablePermissionLevel(environment.DefaultPermissionLevel)
}

func configurationRequiresDetectRefresh(
	current models.RuntimeEnvironment,
	next models.RuntimeEnvironment,
) bool {
	return !agentapi.LaunchDetectInputsEqual(
		agentapi.LaunchDetectInput{
			Binary:     current.Binary,
			PrefixArgs: current.PrefixArgs,
			ConfigHome: current.ConfigHome,
		},
		agentapi.LaunchDetectInput{
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
	peer := accountconfig.QoderPeerRuntimeID(environment.BaseRuntimeID)
	if !isDefault && peer == "" {
		return global.RuntimeEnvironmentControllerManager.UpdateConfiguration(ctx, environment)
	}
	var peerHomes []string
	if peer != "" {
		var err error
		peerHomes, err = qoderPeerConfigHomes(ctx, peer)
		if err != nil {
			return err
		}
		if err := aiaccount.ValidateQoderConfigHomes(environment.BaseRuntimeID, environment.ConfigHome, peerHomes); err != nil {
			return err
		}
	}
	return global.TransactionControllerManager.Transaction(ctx, func(transactionContext context.Context) error {
		if peer != "" {
			// Keep filesystem work outside the database transaction. Fence the
			// exact peer configuration used by that preflight before writing.
			currentPeerHomes, err := qoderPeerConfigHomes(transactionContext, peer)
			if err != nil {
				return err
			}
			if !slices.Equal(currentPeerHomes, peerHomes) {
				return status_error.Conflict
			}
		}
		if isDefault {
			if err := global.RuntimeEnvironmentControllerManager.ClearDefault(transactionContext); err != nil {
				return err
			}
		}
		if err := global.RuntimeEnvironmentControllerManager.UpdateConfiguration(transactionContext, environment); err != nil {
			return err
		}
		if !isDefault {
			return nil
		}
		return global.RuntimeEnvironmentControllerManager.UpdateFields(
			transactionContext,
			environment.RuntimeID,
			map[string]any{"is_default": true},
		)
	})
}

func qoderPeerConfigHomes(ctx context.Context, peer string) ([]string, error) {
	environments, err := global.RuntimeEnvironmentControllerManager.List(ctx)
	if err != nil {
		return nil, err
	}
	var homes []string
	for _, environment := range environments {
		if environment.BaseRuntimeID == peer && environment.ConfigHome != "" {
			homes = append(homes, environment.ConfigHome)
		}
	}
	slices.Sort(homes)
	return slices.Compact(homes), nil
}
