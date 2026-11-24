package review

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetWakerReviewSettings 获取伙伴复盘偏好。
type GetWakerReviewSettings struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
}

// Path 返回伙伴复盘入口。
func (*GetWakerReviewSettings) Path() string { return "/wakers/:wakerId" }

// Output 返回当前伙伴设置和状态。
func (o *GetWakerReviewSettings) Output(ctx *gin.Context) (interface{}, error) {
	if global.ReviewWorker == nil {
		return nil, ginx.WithStack(status_error.RuntimeUnavailable)
	}
	if _, err := global.WakerControllerManager.Get(ctx, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	v, err := wakerView(ctx, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return v, nil
}
