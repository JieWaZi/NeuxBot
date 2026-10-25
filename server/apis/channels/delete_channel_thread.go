package channels

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除团队对话
type DeleteChannelThread struct {
	ginx.MethodDelete
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
}

// Path 返回团队对话永久删除接口路径。
func (d *DeleteChannelThread) Path() string { return "/:channelId/threads/:threadId" }

// Output 永久删除没有活动工作的团队对话及其成员 Chat。
// @err[ChannelNotFound][40400000012][团队不存在]
// @err[ChannelThreadNotFound][40400000013][团队对话不存在]
// @err[ChannelHasActiveWork][40900000016][团队对话仍有运行中或待执行的工作]
// @err[ChatHasActiveExecution][40900000006][目标成员 Chat 存在活跃执行]
// @err[ChatLogError][50000000004][Chat 权威日志读写失败]
// @err[ChatDirectoryOwnershipInvalid][50000000007][Chat 目录归属校验失败]
func (d *DeleteChannelThread) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.ChannelService.DeleteThread(ctx, d.ChannelID, d.ThreadID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
