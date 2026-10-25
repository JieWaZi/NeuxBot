package notifications

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"time"
)

// 收起通知提示
type DismissNotification struct {
	ginx.MethodPost
	// 通知标识
	NoticeID string `in:"path" name:"noticeId" validate:"required,max=200"`
}

func (*DismissNotification) Path() string { return "/:noticeId/dismiss" }
func (o *DismissNotification) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	if _, err := global.NotificationControllerManager.Get(request, o.NoticeID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.AttentionNoticeControllerManager.Dismiss(request, o.NoticeID, time.Now().UnixMilli()); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
