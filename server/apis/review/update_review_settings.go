package review

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/memory/reviewstate"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// UpdateReviewSettings 保存复盘设置。
type UpdateReviewSettings struct {
	ginx.MethodPut
	// 请求体
	Body ReviewSettingsInput `in:"body"`
}

// Path 返回应用级复盘入口。
func (*UpdateReviewSettings) Path() string { return "/settings" }

// Output 保存智能体来源或整理频率。
func (o *UpdateReviewSettings) Output(ctx *gin.Context) (interface{}, error) {
	if global.ReviewWorker == nil {
		return nil, ginx.WithStack(status_error.RuntimeUnavailable)
	}
	v := reviewstate.Settings{Source: o.Body.Source, EveryTurns: o.Body.EveryTurns}
	if v.Source == "" && (v.EveryTurns == 0 || o.Body.RuntimeID != "" || o.Body.AccountID != "" || o.Body.Model != "") {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	if v.Source == reviewstate.SourceGlobal {
		v.RuntimeID, v.AccountID, v.Model = strings.TrimSpace(o.Body.RuntimeID), strings.TrimSpace(o.Body.AccountID), strings.TrimSpace(o.Body.Model)
		if v.RuntimeID == "" || v.Model == "" {
			return nil, ginx.WithStack(status_error.BadRequest)
		}
		if err := global.ValidateReviewSelection(ctx, v); err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	if err := global.ReviewWorker.UpdateSettings(ctx, v); err != nil {
		return nil, ginx.WithStack(err)
	}
	result, err := settingsView(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
