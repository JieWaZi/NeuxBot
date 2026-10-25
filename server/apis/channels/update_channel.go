package channels

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新团队
type UpdateChannel struct {
	ginx.MethodPut
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 请求体
	Body ChannelInput `in:"body"`
}

// Path 返回团队更新接口路径。
func (u *UpdateChannel) Path() string { return "/:channelId" }

// Validate 校验并规范化团队配置。
func (u *UpdateChannel) Validate(*gin.Context) error {
	input, err := normalizeChannelInput(u.Body)
	if err != nil {
		return ginx.WithStack(err)
	}
	u.Body = input
	return nil
}

// Output 更新仅影响未来首次发送的团队对话，不改写既有对话快照。
// @err[ChannelNotFound][40400000012][团队不存在]
// @err[ChannelNameConflict][40900000015][团队名称已存在]
func (u *UpdateChannel) Output(ctx *gin.Context) (interface{}, error) {
	operationContext, release, lockErr := teamwork.Acquire(ctx, u.ChannelID)
	if lockErr != nil {
		return nil, ginx.WithStack(lockErr)
	}
	defer release()
	err := global.TransactionControllerManager.Transaction(operationContext, func(transactionContext context.Context) error {
		existing, err := global.ChannelControllerManager.Get(transactionContext, u.ChannelID)
		if err != nil {
			return err
		}
		if err := validateChannelMembersExist(transactionContext, u.Body); err != nil {
			return err
		}
		occupied, err := global.ChannelControllerManager.NameExists(
			transactionContext,
			u.Body.Name,
			u.ChannelID,
		)
		if err != nil {
			return err
		}
		if occupied {
			return status_error.ChannelNameConflict
		}
		channel, members := channelModels(u.ChannelID, u.Body, existing.CreatedAt)
		if err := global.ChannelControllerManager.Update(transactionContext, channel, members); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
