package channels

import (
	"encoding/hex"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 负责人队列收据
type ChannelLeadInputReceipt struct {
	// 输入标识
	ID string `json:"id"`
	// 队列状态
	Status constants.ChannelInputStatus `json:"status"`
	// 失败错误码
	ErrorCode string `json:"errorCode,omitempty"`
}

// 负责人队列收据列表
type ListChannelLeadInputs struct {
	ginx.MethodGet
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
	// 逗号分隔输入标识
	IDs string `in:"query" name:"ids" validate:"required"`
}

// Path 返回负责人队列收据列表接口路径。
func (l *ListChannelLeadInputs) Path() string {
	return "/:channelId/threads/:threadId/lead-inputs/list"
}

// Validate 校验同一批次标识格式和数量。
func (l *ListChannelLeadInputs) Validate(*gin.Context) error {
	const inputIDLength = 32
	if len(l.IDs) > constants.ChannelLeadInputReceiptBatchLimit*(inputIDLength+1)-1 ||
		strings.Count(l.IDs, ",") >= constants.ChannelLeadInputReceiptBatchLimit {
		return ginx.WithStack(status_error.BadRequest)
	}
	for id := range strings.SplitSeq(l.IDs, ",") {
		if len(id) != inputIDLength {
			return ginx.WithStack(status_error.BadRequest)
		}
		if _, err := hex.DecodeString(id); err != nil {
			return ginx.WithStack(status_error.BadRequest)
		}
	}
	return nil
}

// Output 返回指定对话中已有的队列收据，仅包含输入身份及其状态。
func (l *ListChannelLeadInputs) Output(ctx *gin.Context) (interface{}, error) {
	thread, err := channelThread(ctx, l.ChannelID, l.ThreadID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	inputs, err := global.ChannelThreadControllerManager.ListLeadInputReceipts(ctx, thread.ID, strings.Split(l.IDs, ","))
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	receipts := make([]ChannelLeadInputReceipt, 0, len(inputs))
	for _, input := range inputs {
		receipts = append(receipts, ChannelLeadInputReceipt{
			ID:        input.ID,
			Status:    input.Status,
			ErrorCode: input.ErrorCode,
		})
	}
	return receipts, nil
}
