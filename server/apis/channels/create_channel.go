package channels

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建团队
type CreateChannel struct {
	ginx.MethodPost
	// 请求体
	Body ChannelInput `in:"body"`
}

// Path 返回团队创建接口路径。
func (c *CreateChannel) Path() string { return "" }

// Validate 校验并规范化团队配置。
func (c *CreateChannel) Validate(*gin.Context) error {
	input, err := normalizeChannelInput(c.Body)
	if err != nil {
		return ginx.WithStack(err)
	}
	c.Body = input
	return nil
}

// Output 原子创建团队和成员默认配置；此时不创建任何 Chat 或 JSONL。
// @err[ChannelMemberCountInvalid][40000000019][团队必须包含 2 至 5 位互不重复的成员]
// @err[ChannelLeadInvalid][40000000020][团队负责人必须且只能是所选成员之一]
// @err[ChannelNameConflict][40900000015][团队名称已存在]
func (c *CreateChannel) Output(ctx *gin.Context) (interface{}, error) {
	var detail ChannelDetail
	err := global.TransactionControllerManager.Transaction(ctx, func(transactionContext context.Context) error {
		if err := validateChannelMembersExist(transactionContext, c.Body); err != nil {
			return err
		}
		occupied, err := global.ChannelControllerManager.NameExists(
			transactionContext,
			c.Body.Name,
			"",
		)
		if err != nil {
			return err
		}
		if occupied {
			return status_error.ChannelNameConflict
		}
		channel, members := channelModels(util.New(), c.Body, 0)
		if err := global.ChannelControllerManager.Create(transactionContext, channel, members); err != nil {
			return err
		}
		created, err := loadChannelDetail(
			transactionContext,
			channel,
			models.ActivitySummary{},
		)
		if err != nil {
			return err
		}
		detail = created
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return detail, nil
}
