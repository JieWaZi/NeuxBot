package notifications

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取通知摘要
type GetNotificationSnapshot struct{ ginx.MethodGet }

func (*GetNotificationSnapshot) Path() string { return "/snapshot" }
func (*GetNotificationSnapshot) Output(ctx *gin.Context) (interface{}, error) {
	value, err := global.NotificationService.Snapshot()
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
