package notifications

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"strconv"
	"strings"
	"time"
)

// 确认通知阅读
type ViewNotification struct {
	ginx.MethodPost
	// 通知标识
	NoticeID string `in:"path" name:"noticeId" validate:"required,max=200"`
	// 请求体
	Body ViewNotificationRequest `in:"body"`
}

// 通知阅读请求
type ViewNotificationRequest struct {
	// 观察版本
	Version int64 `json:"version" validate:"min=1"`
	// 原目标引用
	Target *models.NotificationTarget `json:"target,omitempty"`
}

func (*ViewNotification) Path() string { return "/:noticeId/view" }

// Output 确认该次原交付或失败身份，不处理真实待办。
func (o *ViewNotification) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	if o.Body.Version < 1 {
		return nil, status_error.BadRequest
	}
	if strings.HasPrefix(o.NoticeID, "automatic-run-failed:") {
		if o.Body.Version != 1 {
			return nil, status_error.BadRequest
		}
		if err := global.NotificationControllerManager.ConfirmFailure(request, o.NoticeID, time.Now().UnixMilli()); err != nil {
			return nil, ginx.WithStack(err)
		}
		return ginx.CommonSuccessResponse(), nil
	}
	parts := strings.Split(o.NoticeID, ":")
	if len(parts) != 4 || parts[0] != "result" {
		return nil, status_error.BadRequest
	}
	version, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || version != o.Body.Version {
		return nil, status_error.BadRequest
	}
	kind := constants.WorkResultOwnerKind(parts[1])
	target := o.Body.Target
	if target == nil || target.Kind != constants.NotificationKindResult || target.OwnerKind != kind || target.OwnerID != parts[2] || target.ResultVersion != version || target.Reference == nil {
		return nil, status_error.BadRequest
	}
	if err := global.NotificationService.ViewResult(request, kind, parts[2], version, *target.Reference); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
