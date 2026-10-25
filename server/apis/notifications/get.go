package notifications

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取通知
type GetNotification struct {
	ginx.MethodGet
	// 通知标识
	NoticeID string `in:"path" name:"noticeId" validate:"required,max=200"`
}

func (*GetNotification) Path() string { return "/:noticeId" }
func (o *GetNotification) Output(ctx *gin.Context) (interface{}, error) {
	value, err := global.NotificationControllerManager.Get(ctx.Request.Context(), o.NoticeID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
