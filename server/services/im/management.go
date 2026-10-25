package im

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ConnectionConfiguration 只接受管理员可编辑的字段，平台凭据仅写入。
type ConnectionConfiguration struct {
	// 连接名称
	Name string `json:"name" validate:"required,max=120"`
	// 连接平台
	Platform constants.IMPlatform `json:"platform"`
	// 是否启用
	Enabled bool `json:"enabled"`
	// 维持运行
	KeepAwake bool `json:"keepAwake"`
	// 应用标识
	AppID string `json:"appId" validate:"max=200"`
	// 应用密钥
	AppSecret string `json:"appSecret" validate:"max=4096"`
	// 配置版本
	Revision int64 `json:"revision"`
}

// SaveConfiguration 创建或按版本更新连接，不允许手工注入微信令牌和服务域名。
func (s *Service) SaveConfiguration(ctx context.Context, id string, input ConnectionConfiguration) (models.IMConnection, error) {
	value := models.IMConnection{ID: id, Name: strings.TrimSpace(input.Name), Platform: input.Platform, Enabled: input.Enabled, KeepAwake: input.KeepAwake, State: constants.IMConnectionStateDisconnected}
	if value.Name == "" || (value.Platform != constants.IMPlatformWeixin && value.Platform != constants.IMPlatformFeishu && value.Platform != constants.IMPlatformWecom) {
		return value, status_error.IMConfigurationInvalid
	}
	if id == "" {
		value.ID = util.New()
		input.Revision = 0
	} else {
		previous, err := s.config.Store.Connection(ctx, id)
		if err != nil {
			return value, err
		}
		if previous.Platform != input.Platform {
			return value, status_error.IMConfigurationInvalid
		}
		value = previous
		value.Name = strings.TrimSpace(input.Name)
		value.Enabled = input.Enabled
		value.KeepAwake = input.KeepAwake
	}
	input.AppID = strings.TrimSpace(input.AppID)
	input.AppSecret = strings.TrimSpace(input.AppSecret)
	if input.AppSecret != "" || input.AppID != "" {
		if (value.Platform != constants.IMPlatformFeishu && value.Platform != constants.IMPlatformWecom) || strings.TrimSpace(input.AppID) == "" || (input.AppSecret == "" && input.AppID != value.AppID) {
			return value, status_error.IMConfigurationInvalid
		}
		if value.AppID != "" && value.AppID != strings.TrimSpace(input.AppID) {
			return value, status_error.IMConfigurationConflict
		}
		value.AppID = strings.TrimSpace(input.AppID)
		if input.AppSecret != "" {
			value.Credential = strings.TrimSpace(input.AppSecret)
		}
	}
	if value.Enabled && value.Credential == "" {
		return value, status_error.IMLoginRequired
	}
	if err := s.config.Store.SaveConnection(ctx, value, input.Revision); err != nil {
		return value, err
	}
	if err := s.config.Store.ConnectionState(ctx, value.ID, constants.IMConnectionStateDisconnected, ""); err != nil {
		return value, err
	}
	return s.config.Store.Connection(ctx, value.ID)
}

// Target 是可供模型选择的真实目标摘要，不包含平台身份和凭据。
type Target struct {
	// ID 是发送时使用的批准目标身份。
	ID string `json:"id"`
	// Name 是管理员设置的聊天名称。
	Name string `json:"name"`
	// Platform 是聊天所在平台。
	Platform constants.IMPlatform `json:"platform"`
	// Kind 区分个人聊天和群聊。
	Kind constants.IMChatKind `json:"kind"`
}

// ValidateWorkflowTarget 核验声明的发送目标，在运行接纳和实际发送前共用同一规则。
func (s *Service) ValidateWorkflowTarget(ctx context.Context, id string) error {
	binding, err := s.config.Store.Binding(ctx, id)
	if err != nil {
		return err
	}
	if binding.State != constants.IMBindingStateApproved {
		return status_error.IMTargetNotAuthorized.WithField("targetId", id).WithField("hint", constants.IMRecoveryAuthorizeChat)
	}
	if !binding.AllowWorkflowDelivery {
		return status_error.IMWorkflowDeliveryNotAllowed.WithField("targetId", id).WithField("hint", constants.IMRecoveryEnableWorkflowDelivery)
	}
	connection, err := s.config.Store.Connection(ctx, binding.ConnectionID)
	if err != nil {
		return err
	}
	if !connection.Enabled {
		return status_error.IMConnectionDisabled.WithField("targetId", id).WithField("hint", constants.IMRecoveryEnableConnection)
	}
	if connection.State == constants.IMConnectionStateLoginRequired || connection.Credential == "" {
		return status_error.IMLoginRequired.WithField("targetId", id).WithField("hint", constants.IMRecoverySignIn)
	}
	return nil
}

// Targets 只返回已批准且在线配置允许的目标，普通伙伴限于自身或所属团队。
func (s *Service) Targets(ctx context.Context, wakerID string, workflow bool) ([]Target, error) {
	bindings, _, err := s.config.Store.Bindings(ctx, "", 0, 0)
	if err != nil {
		return nil, err
	}
	connections, err := s.config.Store.Connections(ctx)
	if err != nil {
		return nil, err
	}
	active := map[string]models.IMConnection{}
	for _, c := range connections {
		if c.Enabled {
			active[c.ID] = c
		}
	}
	result := []Target{}
	for _, b := range bindings {
		connection, ok := active[b.ConnectionID]
		if !ok || b.State != constants.IMBindingStateApproved {
			continue
		}
		allowed := workflow && b.AllowWorkflowDelivery || !workflow && b.WakerID == wakerID && wakerID != ""
		if !workflow && !allowed && b.ChannelID != "" {
			members, err := s.config.Channels.ListMembers(ctx, b.ChannelID)
			if err != nil {
				return nil, err
			}
			for _, member := range members {
				if member.WakerID == wakerID {
					allowed = true
					break
				}
			}
		}
		if allowed {
			result = append(result, Target{ID: b.ID, Name: b.Name, Platform: connection.Platform, Kind: b.Kind})
		}
	}
	return result, nil
}

// SelectConversation 由桌面管理员切换同一聊天内实际对话，空身份准备下一条任务新建。
func (s *Service) SelectConversation(ctx context.Context, bindingID, linkID string) error {
	return s.WithBinding(ctx, bindingID, func(ctx context.Context, binding models.IMBinding) error {
		return s.selectConversation(ctx, binding, linkID)
	})
}

func (s *Service) selectConversation(ctx context.Context, binding models.IMBinding, linkID string) error {
	if binding.State != constants.IMBindingStateApproved {
		return status_error.IMChatNotAuthorized
	}
	if linkID != "" {
		link, err := s.config.Store.Link(ctx, linkID)
		if err != nil {
			return err
		}
		if link.BindingID != binding.ID || link.ChatID == "" {
			return status_error.IMResourceNotFound
		}
	}
	binding.ActiveLinkID = linkID
	return s.config.Store.SaveBinding(ctx, binding, binding.Revision)
}
