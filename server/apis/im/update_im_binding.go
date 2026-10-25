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
	// 授权状态
	State constants.IMBindingState `json:"state"`
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
	// 配置版本
	Revision int64 `json:"revision"`
}

// UpdateIMBinding 更新聊天授权。
type UpdateIMBinding struct {
	ginx.MethodPut
	// 聊天标识
	BindingID string `in:"path" name:"bindingId" validate:"required"`
	// 请求体
	Body BindingConfiguration `in:"body"`
}

func (*UpdateIMBinding) Path() string { return "/bindings/:bindingId" }
func (o *UpdateIMBinding) Output(ctx *gin.Context) (interface{}, error) {
	input := o.Body
	if input.State != constants.IMBindingStateApproved && input.State != constants.IMBindingStateRejected {
		return nil, ginx.WithStack(status_error.IMConfigurationInvalid)
	}
	if strings.TrimSpace(input.Name) == "" || input.ProjectID != "" && input.WorkingDirectory != "" {
		return nil, ginx.WithStack(status_error.IMConfigurationInvalid)
	}
	owner := input.WakerID
	if input.State == constants.IMBindingStateApproved {
		if (input.WakerID == "") == (input.ChannelID == "") {
			return nil, ginx.WithStack(status_error.IMConfigurationInvalid)
		}
		if input.ChannelID != "" {
			team, err := global.ChannelControllerManager.Get(ctx, input.ChannelID)
			if err != nil {
				return nil, ginx.WithStack(err)
			}
			owner = team.LeadWakerID
		} else {
			if _, err := global.WakerControllerManager.Get(ctx, input.WakerID); err != nil {
				return nil, ginx.WithStack(err)
			}
			resolved, err := global.ExecutionService.ResolveConfiguration(ctx, execution.ExecutionConfiguration{RuntimeID: input.Options.RuntimeID, AccountID: input.Options.AccountID, Model: input.Options.Model, Reasoning: input.Options.Reasoning, PermissionLevel: input.Options.PermissionLevel})
			if err != nil {
				return nil, ginx.WithStack(err)
			}
			input.Options = models.IMExecutionOptions{RuntimeID: resolved.RuntimeID, AccountID: resolved.AccountID, Model: resolved.Model, Reasoning: resolved.Reasoning, PermissionLevel: resolved.PermissionLevel}
		}
		if input.ProjectID != "" {
			project, err := global.ProjectControllerManager.Get(ctx, input.ProjectID)
			if err != nil {
				return nil, ginx.WithStack(err)
			}
			if project.WakerID != owner {
				return nil, ginx.WithStack(status_error.IMConfigurationInvalid)
			}
		}
		// 文件系统校验在事务外完成，事务只提交并发校验后的配置。
		if input.WorkingDirectory != "" {
			path, err := filepath.Abs(input.WorkingDirectory)
			if err != nil {
				return nil, ginx.WithStack(status_error.IMConfigurationInvalid)
			}
			path, err = filepath.EvalSymlinks(path)
			if err != nil {
				return nil, ginx.WithStack(status_error.IMConfigurationInvalid)
			}
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() {
				return nil, ginx.WithStack(status_error.IMConfigurationInvalid)
			}
			input.WorkingDirectory = path
		}
	}
	err := global.IMService.WithBinding(ctx, o.BindingID, func(tx context.Context, binding models.IMBinding) error {
		if input.State == constants.IMBindingStateApproved && binding.Kind == constants.IMChatKindGroup && len(input.ControllerIDs) == 0 {
			return status_error.IMConfigurationInvalid
		}
		changed := binding.WakerID != input.WakerID || binding.ChannelID != input.ChannelID || binding.ProjectID != input.ProjectID || binding.WorkingDirectory != input.WorkingDirectory || binding.Options != input.Options
		binding.Name = strings.TrimSpace(input.Name)
		binding.State = input.State
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
		return global.IMControllerManager.SaveBinding(tx, binding, input.Revision)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
