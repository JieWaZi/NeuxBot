package wakers

import (
	"context"
	"reflect"
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新伙伴默认模型请求
type UpdateWakerDefaultModelRequest struct {
	// 默认模型
	DefaultModel *models.WakerDefaultModel `json:"defaultModel" validate:"required"`
}

// 更新伙伴默认模型
type UpdateWakerDefaultModel struct {
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body UpdateWakerDefaultModelRequest `in:"body"`
}

// Path 返回伙伴默认模型接口路径。
func (u *UpdateWakerDefaultModel) Path() string {
	return "/:wakerId/default-model"
}

// Output 只更新默认模型；空配置表示新对话跟随全局默认。
func (u *UpdateWakerDefaultModel) Output(ctx *gin.Context) (interface{}, error) {
	defaultModel, err := normalizeDefaultModel(u.Body.DefaultModel)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	change := func() (bool, error) {
		changed := false
		err := global.TransactionControllerManager.Transaction(ctx, func(tx context.Context) error {
			waker, err := global.WakerControllerManager.Get(tx, u.WakerID)
			if err != nil || reflect.DeepEqual(waker.DefaultModel, defaultModel) {
				return err
			}
			changed = true
			return global.WakerControllerManager.UpdateDefaultModel(tx, u.WakerID, defaultModel, time.Now().UnixMilli())
		})
		return changed, err
	}
	if global.ReviewWorker == nil {
		_, err = change()
	} else {
		err = global.ReviewWorker.ChangeFollowedDefault(ctx, u.WakerID, change)
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
