package run_viewing

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetRunViewing 读取指定原运行最终结果的查看状态。
type GetRunViewing struct {
	// MethodGet 声明只读请求。
	ginx.MethodGet
	// OwnerKind 是原运行的类型。
	OwnerKind constants.RunOwnerKind `in:"path" name:"ownerKind" validate:"required"`
	// OwnerID 是指定原运行的标识。
	OwnerID string `in:"path" name:"ownerId" validate:"required,len=32"`
}

// RunViewingResponse 返回原结果定位和可选的安全接纳失败信息。
type RunViewingResponse struct {
	// RunViewingState 保存指定原运行的查看状态。
	models.RunViewingState
	// AttentionCompleteness 标识当前权威 Core 的生命周期。
	models.AttentionCompleteness
	// AutomaticTaskID 定位失败机会的自动任务配置。
	AutomaticTaskID string `json:"automaticTaskId,omitempty"`
	// ErrorCode 是未接纳自动机会的稳定错误码。
	ErrorCode string `json:"errorCode,omitempty"`
}

func (*GetRunViewing) Path() string { return "/:ownerKind/:ownerId/viewing" }
func (d *GetRunViewing) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	state, err := global.RunViewingControllerManager.Get(request, constants.LocalReaderID, models.RunOwnerRef{OwnerKind: d.OwnerKind, OwnerID: d.OwnerID})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	value := RunViewingResponse{RunViewingState: state, AttentionCompleteness: models.AttentionCompleteness{Complete: true, Epoch: global.EventHub.Epoch()}}
	if state.Viewable && d.OwnerKind == constants.RunOwnerKindAutomaticRun {
		run, err := global.AutomaticTaskControllerManager.GetRun(request, d.OwnerID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		value.AutomaticTaskID, value.ErrorCode = run.TaskID, run.ErrorCode
	}
	return value, nil
}
