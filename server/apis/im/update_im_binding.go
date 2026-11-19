package im

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// BindingConfiguration 聊天授权配置。
type BindingConfiguration struct {
	// 聊天名称
	Name string `json:"name" validate:"required,max=120"`
	// 伙伴标识
	WakerID string `json:"wakerId"`
	// 团队标识
	ChannelID string `json:"channelId"`
	// 项目标识
	ProjectID string `json:"projectId"`
	// 工作目录
	WorkingDirectory string `json:"workingDirectory"`
	// 执行配置
	Options models.IMExecutionOptions `json:"options"`
	// 管理成员
	ControllerIDs []string `json:"controllerIds" validate:"max=100,dive,max=200"`
	// 工作流投递
	AllowWorkflowDelivery bool `json:"allowWorkflowDelivery"`
}

// UpdateIMBindingRequest 聊天授权更新。
type UpdateIMBindingRequest struct {
	// 授权状态
	State constants.IMBindingState `json:"state"`
	// 聊天配置
	Configuration *BindingConfiguration `json:"configuration,omitempty"`
}

// UpdateIMBinding 更新聊天授权。
type UpdateIMBinding struct {
	ginx.MethodPut
	// 聊天标识
	BindingID string `in:"path" name:"bindingId" validate:"required"`
	// 请求体
	Body UpdateIMBindingRequest `in:"body"`
}

// Path 返回聊天授权的管理路径。
func (*UpdateIMBinding) Path() string { return "/bindings/:bindingId" }

// Output 保存聊天配置或单独更新已保存配置的授权状态。
func (o *UpdateIMBinding) Output(ctx *gin.Context) (interface{}, error) {
	state, input := o.Body.State, o.Body.Configuration
	validState := state == constants.IMBindingStateApproved ||
		input == nil && state == constants.IMBindingStateRejected ||
		input != nil && state == constants.IMBindingStateUnspecified
	if !validState {
		return nil, ginx.WithStack(status_error.IMConfigurationInvalid)
	}
	configuration := input
	if input == nil && state == constants.IMBindingStateApproved {
		saved, err := global.IMControllerManager.Binding(ctx, o.BindingID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		configuration = &BindingConfiguration{
			Name: saved.Name, WakerID: saved.WakerID, ChannelID: saved.ChannelID, ProjectID: saved.ProjectID,
			WorkingDirectory: saved.WorkingDirectory, Options: saved.Options,
		}
	}
	// 运行环境和本地目录校验在事务外完成。
	if configuration != nil {
		if err := validateBindingConfiguration(ctx, configuration); err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	err := global.IMService.WithBinding(ctx, o.BindingID, func(tx context.Context, binding models.IMBinding) error {
		savedState := binding.State
		if state != constants.IMBindingStateUnspecified {
			savedState = state
		}
		controllerIDs := binding.ControllerIDs
		if input != nil {
			controllerIDs = input.ControllerIDs
		}
		if savedState == constants.IMBindingStateApproved && binding.Kind == constants.IMChatKindGroup && len(controllerIDs) == 0 {
			return status_error.IMConfigurationInvalid
		}
		if input == nil {
			return global.IMControllerManager.SetBindingState(tx, binding.ID, savedState)
		}
		changed := binding.WakerID != input.WakerID || binding.ChannelID != input.ChannelID || binding.ProjectID != input.ProjectID || binding.WorkingDirectory != input.WorkingDirectory || binding.Options != input.Options
		binding.Name = input.Name
		binding.State = savedState
		binding.WakerID = input.WakerID
		binding.ChannelID = input.ChannelID
		binding.ProjectID = input.ProjectID
		binding.WorkingDirectory = input.WorkingDirectory
		binding.Options = input.Options
		binding.ControllerIDs = input.ControllerIDs
		binding.AllowWorkflowDelivery = input.AllowWorkflowDelivery
		if changed {
			binding.ActiveLinkID = ""
		}
		return global.IMControllerManager.SaveBinding(tx, binding)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

func validateBindingConfiguration(ctx context.Context, config *BindingConfiguration) error {
	config.Name = strings.TrimSpace(config.Name)
	if config.Name == "" || config.ProjectID != "" && config.WorkingDirectory != "" {
		return status_error.IMConfigurationInvalid
	}
	if err := validateBindingTarget(ctx, config.WakerID, config.ChannelID, config.ProjectID); err != nil {
		return err
	}
	if config.WakerID != "" {
		options := config.Options
		resolved, err := global.ExecutionService.ResolveConfiguration(ctx, execution.ExecutionConfiguration{RuntimeID: options.RuntimeID, AccountID: options.AccountID, Model: options.Model, Reasoning: options.Reasoning, PermissionLevel: options.PermissionLevel})
		if err != nil {
			return err
		}
		config.Options = models.IMExecutionOptions{RuntimeID: resolved.RuntimeID, AccountID: resolved.AccountID, Model: resolved.Model, Reasoning: resolved.Reasoning, PermissionLevel: resolved.PermissionLevel}
	}
	if config.WorkingDirectory == "" {
		return nil
	}
	path, err := filepath.Abs(config.WorkingDirectory)
	if err != nil {
		return status_error.IMConfigurationInvalid
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return status_error.IMConfigurationInvalid
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return status_error.IMConfigurationInvalid
	}
	config.WorkingDirectory = path
	return nil
}

func validateBindingTarget(ctx context.Context, wakerID, channelID, projectID string) error {
	if (wakerID == "") == (channelID == "") {
		return status_error.IMConfigurationInvalid
	}
	owner := wakerID
	if channelID != "" {
		team, err := global.ChannelControllerManager.Get(ctx, channelID)
		if err != nil {
			return err
		}
		owner = team.LeadWakerID
	} else if _, err := global.WakerControllerManager.Get(ctx, wakerID); err != nil {
		return err
	}
	if projectID != "" {
		project, err := global.ProjectControllerManager.Get(ctx, projectID)
		if err != nil {
			return err
		}
		if project.WakerID != owner {
			return status_error.IMConfigurationInvalid
		}
	}
	return nil
}
