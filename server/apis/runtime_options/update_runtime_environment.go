package runtime_options

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 默认执行设置请求
type UpdateRuntimeEnvironmentRequest struct {
	// 安装实例
	InstallationPath string `json:"installationPath,omitempty" validate:"max=4096"`
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
// [RuntimeInstallationBusy][40900007301][智能体程序正在维护]
// [RuntimeConfigurationInvalid][50000000040][所选智能体版本不可用]
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
	// Runtime 设置只影响后续准备，不改写账号认证材料或回收现有消费者。
	if accountID != "" {
		account, err := global.AIAccountService.Get(ctx, accountID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if err := aiaccount.ValidateRuntimeAccount(account, current); err != nil {
			return nil, ginx.WithStack(err)
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
	refresh := false
	commit := func(observed models.RuntimeEnvironment, selected agent.CLIInstallation) error {
		refresh = selected.Path != "" || observed.DefaultAccountID != environment.DefaultAccountID
		return global.TransactionControllerManager.Transaction(ctx, func(tx context.Context) error {
			if err := updateRuntimeEnvironment(tx, environment, u.Body.IsDefault); err != nil {
				return err
			}
			if selected.Path != "" {
				summary := observed.Detection.Runtime
				if global.RuntimeCatalog != nil {
					for _, candidate := range global.RuntimeCatalog.Runtimes() {
						if candidate.ID == u.RuntimeID {
							summary = candidate
							break
						}
					}
				}
				detection := agent.DetectionResult{Runtime: summary, Installations: observed.Detection.Installations, ResolvedBinary: selected.Path, Version: selected.Version, State: agent.DetectionStateReady, Auth: agent.AuthStatusUnknown}
				return global.RuntimeEnvironmentControllerManager.UpdateDetection(tx, u.RuntimeID, detection, observed.DetectedAt)
			}
			return nil
		})
	}
	if u.Body.InstallationPath != "" {
		err = global.RuntimeEnvironmentService.CommitInstallationSelection(ctx, u.RuntimeID, u.Body.InstallationPath, commit)
	} else {
		err = commit(current, agent.CLIInstallation{})
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if refresh {
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
