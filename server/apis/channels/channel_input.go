package channels

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// 团队成员输入
type ChannelMemberInput struct {
	// 伙伴标识
	WakerID string `json:"wakerId" validate:"required,max=32"`
	// 运行时标识
	RuntimeID string `json:"runtimeId" validate:"required,max=120"`
	// 账号标识
	AccountID string `json:"accountId" validate:"max=32"`
	// 模型
	Model string `json:"model" validate:"max=200"`
	// 推理强度
	Reasoning string `json:"reasoning" validate:"max=80"`
	// 权限等级
	PermissionLevel constants.PermissionLevel `json:"permissionLevel" validate:"required"`
}

// 团队输入
type ChannelInput struct {
	// 团队名称
	Name string `json:"name" validate:"required,max=80"`
	// 团队说明
	Description string `json:"description" validate:"max=1000"`
	// 负责人标识
	LeadWakerID string `json:"leadWakerId" validate:"required,max=32"`
	// 团队成员
	Members []ChannelMemberInput `json:"members" validate:"required,min=2,max=5,dive"`
}

func normalizeChannelInput(input ChannelInput) (ChannelInput, error) {
	input.Name = util.NormalizeName(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.LeadWakerID = strings.TrimSpace(input.LeadWakerID)
	if input.Name == "" || len(input.Members) < constants.ChannelMinimumMemberCount ||
		len(input.Members) > constants.ChannelMaximumMemberCount {
		return ChannelInput{}, status_error.ChannelMemberCountInvalid
	}
	members, err := normalizeChannelMemberInputs(input.Members)
	if err != nil {
		return ChannelInput{}, err
	}
	input.Members = members
	seen := make(map[string]struct{}, len(input.Members))
	leadCount := 0
	for _, member := range input.Members {
		seen[member.WakerID] = struct{}{}
		if member.WakerID == input.LeadWakerID {
			leadCount++
		}
	}
	if leadCount != 1 {
		return ChannelInput{}, status_error.ChannelLeadInvalid
	}
	return input, nil
}

func normalizeChannelMemberInputs(
	members []ChannelMemberInput,
) ([]ChannelMemberInput, error) {
	normalized := append([]ChannelMemberInput(nil), members...)
	seen := make(map[string]struct{}, len(normalized))
	for index := range normalized {
		member := &normalized[index]
		member.WakerID = strings.TrimSpace(member.WakerID)
		member.RuntimeID = strings.TrimSpace(member.RuntimeID)
		member.AccountID = strings.TrimSpace(member.AccountID)
		member.Model = strings.TrimSpace(member.Model)
		member.Reasoning = strings.TrimSpace(member.Reasoning)
		member.PermissionLevel = constants.PermissionLevel(
			strings.TrimSpace(string(member.PermissionLevel)),
		)
		if member.WakerID == "" || member.RuntimeID == "" || member.PermissionLevel == "" {
			return nil, status_error.BadRequest
		}
		if _, duplicated := seen[member.WakerID]; duplicated {
			return nil, status_error.ChannelMemberCountInvalid
		}
		seen[member.WakerID] = struct{}{}
	}
	return normalized, nil
}

func validateChannelMembersExist(ctx context.Context, input ChannelInput) error {
	for _, member := range input.Members {
		if _, err := global.WakerControllerManager.Get(ctx, member.WakerID); err != nil {
			return err
		}
		if err := validateChannelMemberExecutionConfig(ctx, member, member.WakerID == input.LeadWakerID); err != nil {
			return err
		}
	}
	return nil
}

// validateChannelMemberExecutionConfig 复用 Execution Core 的唯一执行配置校验器。
func validateChannelMemberExecutionConfig(
	ctx context.Context,
	member ChannelMemberInput,
	requireCollaboration bool,
) error {
	if global.ExecutionService == nil {
		return status_error.ChatServiceNotStarted
	}
	configuration := execution.ExecutionConfiguration{
		RuntimeID:       member.RuntimeID,
		AccountID:       member.AccountID,
		Model:           member.Model,
		Reasoning:       member.Reasoning,
		PermissionLevel: member.PermissionLevel,
	}
	if requireCollaboration {
		configuration.RequiredConnectorTransport = constants.RuntimeConnectorTransportStreamableHTTP
	}
	return global.ExecutionService.ValidateConfiguration(ctx, configuration)
}

func channelModels(
	channelID string,
	input ChannelInput,
	createdAt int64,
) (models.Channel, []models.ChannelMember) {
	now := time.Now().UnixMilli()
	if createdAt == 0 {
		createdAt = now
	}
	channel := models.Channel{
		ID:          channelID,
		Name:        input.Name,
		Description: input.Description,
		LeadWakerID: input.LeadWakerID,
		CreatedAt:   createdAt,
		UpdatedAt:   now,
	}
	members := make([]models.ChannelMember, 0, len(input.Members))
	for position, inputMember := range input.Members {
		members = append(members, models.ChannelMember{
			ChannelID:       channelID,
			WakerID:         inputMember.WakerID,
			RuntimeID:       inputMember.RuntimeID,
			AccountID:       inputMember.AccountID,
			Model:           inputMember.Model,
			Reasoning:       inputMember.Reasoning,
			PermissionLevel: inputMember.PermissionLevel,
			Position:        position,
			CreatedAt:       now,
			UpdatedAt:       now,
		})
	}
	return channel, members
}
