package review

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/memory/reviewstate"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// UpdateWakerReviewSettings 保存伙伴复盘偏好。
type UpdateWakerReviewSettings struct {
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body WakerReviewInput `in:"body"`
}

// Path 返回伙伴复盘入口。
func (*UpdateWakerReviewSettings) Path() string { return "/wakers/:wakerId" }

// Output 保存独立的功能授权。
func (o *UpdateWakerReviewSettings) Output(ctx *gin.Context) (interface{}, error) {
	if global.ReviewWorker == nil {
		return nil, ginx.WithStack(status_error.RuntimeUnavailable)
	}
	if _, err := global.WakerControllerManager.Get(ctx, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.ReviewWorker.UpdatePartner(ctx, o.WakerID, reviewstate.Partner{MemoryEnabled: o.Body.MemoryEnabled, SkillMode: o.Body.SkillMode}); err != nil {
		return nil, ginx.WithStack(err)
	}
	v, err := wakerView(ctx, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return v, nil
}
