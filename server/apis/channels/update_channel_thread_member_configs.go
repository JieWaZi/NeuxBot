package channels

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新团队成员配置请求
type UpdateChannelThreadMemberConfigsRequest struct {
	// 成员配置
	Members []ChannelMemberInput `json:"members" validate:"required,min=1,max=5,dive"`
}

// 更新团队成员配置
type UpdateChannelThreadMemberConfigs struct {
	ginx.MethodPut
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
	// 请求体
	Body UpdateChannelThreadMemberConfigsRequest `in:"body"`
}

// Path 返回团队对话成员执行配置接口路径。
func (u *UpdateChannelThreadMemberConfigs) Path() string {
	return "/:channelId/threads/:threadId/members/config"
}

// Validate 规范化成员完整执行配置。
func (u *UpdateChannelThreadMemberConfigs) Validate(*gin.Context) error {
	members, err := normalizeChannelMemberInputs(u.Body.Members)
	if err != nil || len(members) == 0 {
		return ginx.WithStack(status_error.BadRequest)
	}
	u.Body.Members = members
	return nil
}

// Output 更新后续新执行使用的配置，不影响正在运行的执行。
func (u *UpdateChannelThreadMemberConfigs) Output(ctx *gin.Context) (interface{}, error) {
	operationContext, release, lockErr := teamwork.Acquire(ctx, u.ChannelID)
	if lockErr != nil {
		return nil, ginx.WithStack(lockErr)
	}
	defer release()
	thread, err := writableChannelThread(operationContext, u.ChannelID, u.ThreadID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	now := time.Now().UnixMilli()
	updated := make([]models.ChannelThreadMember, 0, len(u.Body.Members))
	for _, config := range u.Body.Members {
		if validationErr := validateChannelMemberExecutionConfig(
			operationContext,
			config,
			config.WakerID == thread.LeadWakerID,
		); validationErr != nil {
			return nil, ginx.WithStack(validationErr)
		}
		member, getErr := global.ChannelThreadControllerManager.GetMember(operationContext, thread.ID, config.WakerID)
		if getErr != nil {
			return nil, ginx.WithStack(getErr)
		}
		member.RuntimeID = config.RuntimeID
		member.AccountID = config.AccountID
		member.Model = config.Model
		member.Reasoning = config.Reasoning
		member.PermissionLevel = config.PermissionLevel
		member.UpdatedAt = now
		updated = append(updated, member)
	}
	err = global.TransactionControllerManager.Transaction(operationContext, func(transactionContext context.Context) error {
		current, getErr := global.ChannelThreadControllerManager.Get(transactionContext, thread.ID)
		if getErr != nil {
			return getErr
		}
		if current.CancelingAt != nil {
			return status_error.ChannelTaskStateConflict
		}
		if writableErr := ensureChannelWritable(transactionContext, thread.ChannelID); writableErr != nil {
			return writableErr
		}
		if updateErr := global.ChannelThreadControllerManager.UpdateMemberExecutionConfigs(
			transactionContext,
			updated,
		); updateErr != nil {
			return updateErr
		}
		wakerIDs := make([]string, 0, len(updated))
		for _, member := range updated {
			wakerIDs = append(wakerIDs, member.WakerID)
		}
		if _, eventErr := appendChannelEvent(
			transactionContext,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventMemberConfigsUpdated,
			map[string]interface{}{"wakerIds": wakerIDs},
		); eventErr != nil {
			return eventErr
		}
		return global.ChannelThreadControllerManager.Touch(transactionContext, thread.ID, now)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
