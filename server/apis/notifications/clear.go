package notifications

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 清空待查看通知
type ClearNotificationUnread struct{ ginx.MethodPost }

func (*ClearNotificationUnread) Path() string { return "/unread/clear" }
func (*ClearNotificationUnread) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.NotificationService.ClearUnread(ctx.Request.Context()); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
