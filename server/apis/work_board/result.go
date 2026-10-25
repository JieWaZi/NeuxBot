package work_board

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

func validOwner(kind constants.WorkResultOwnerKind) bool {
	return kind == constants.WorkResultOwnerKindChat || kind == constants.WorkResultOwnerKindTeamThread || kind == constants.WorkResultOwnerKindWorkflowRun
}

// GetWorkResult 获取原结果身份，读取本身不确认查看。
type GetWorkResult struct {
	// MethodGet 声明只读 HTTP 方法。
	ginx.MethodGet
	// OwnerKind 是原容器种类。
	OwnerKind constants.WorkResultOwnerKind `in:"path" name:"ownerKind" validate:"required,oneof=chat team_thread workflow_run"`
	// OwnerID 是原容器标识。
	OwnerID string `in:"path" name:"ownerId" validate:"required,len=32"`
}

// WorkResultResponse 容器没有新结果时允许空引用。
type WorkResultResponse struct {
	// Result 是已发布结果及其阅读事实。
	Result *models.WorkResultReceipt `json:"result,omitempty"`
}

// Path 返回指定原容器的结果路径。
func (*GetWorkResult) Path() string { return "/results/:ownerKind/:ownerId" }

// Output 返回最新引用，不复制成果正文。
func (d *GetWorkResult) Output(ctx *gin.Context) (interface{}, error) {
	request, cancel := queryContext(ctx)
	defer cancel()
	v, found, err := global.WorkResultControllerManager.Get(request, d.OwnerKind, d.OwnerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if !found {
		return WorkResultResponse{}, nil
	}
	return WorkResultResponse{Result: &v}, nil
}

// ViewWorkResult 确认用户实际看见的精确原结果版本。
type ViewWorkResult struct {
	// MethodPost 声明阅读事实写入方法。
	ginx.MethodPost
	// OwnerKind 是原容器种类。
	OwnerKind constants.WorkResultOwnerKind `in:"path" name:"ownerKind" validate:"required,oneof=chat team_thread workflow_run"`
	// OwnerID 是原容器身份。
	OwnerID string `in:"path" name:"ownerId" validate:"required,len=32"`
	// Body 是本次实际查看的原结果身份。
	Body ViewWorkResultRequest `in:"body"`
}

// ViewWorkResultRequest 固定客户端本次真正渲染的版本与引用。
type ViewWorkResultRequest struct {
	// Version 是本次渲染真正观察到的版本，禁止使用请求时最新版本代替。
	Version int64 `json:"version" validate:"min=1"`
	// Reference 是与观察版本一起获取的原结果引用。
	Reference models.WorkResultReference `json:"reference" validate:"required"`
}

// Path 返回版本化查看路径。
func (*ViewWorkResult) Path() string { return "/results/:ownerKind/:ownerId/view" }

// Output 校验最新观察；已过期观察幂等忽略，校验后的并发发布仍保持未读。
func (d *ViewWorkResult) Output(ctx *gin.Context) (interface{}, error) {
	request, cancel := queryContext(ctx)
	defer cancel()
	if err := global.NotificationService.ViewResult(request, d.OwnerKind, d.OwnerID, d.Body.Version, d.Body.Reference); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
