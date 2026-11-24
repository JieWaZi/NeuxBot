package review

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// RetryWakerReview 重试伙伴复盘。
type RetryWakerReview struct {
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
}

// Path 返回失败批次的重试入口。
func (*RetryWakerReview) Path() string { return "/wakers/:wakerId/retry" }

// Output 将当前伙伴失败批次重新交给后台。
func (o *RetryWakerReview) Output(ctx *gin.Context) (interface{}, error) {
	if global.ReviewWorker == nil {
		return nil, ginx.WithStack(status_error.RuntimeUnavailable)
	}
	if _, err := global.WakerControllerManager.Get(ctx, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.ReviewWorker.Retry(ctx, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	global.ReviewWorker.Wake()
	return ginx.CommonSuccessResponse(), nil
}
