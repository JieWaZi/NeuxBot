package review

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetReviewSettings 获取复盘设置。
type GetReviewSettings struct{ ginx.MethodGet }

// Path 返回应用级复盘入口。
func (*GetReviewSettings) Path() string { return "/settings" }

// Output 读取配置和已支持的智能体。
func (*GetReviewSettings) Output(ctx *gin.Context) (interface{}, error) {
	if global.ReviewWorker == nil {
		return nil, ginx.WithStack(status_error.RuntimeUnavailable)
	}
	v, err := settingsView(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return v, nil
}
