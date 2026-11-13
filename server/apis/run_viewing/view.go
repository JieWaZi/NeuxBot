package run_viewing

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// MarkRunViewed 确认已呈现指定原运行的最终结果。
type MarkRunViewed struct {
	// MethodPost 声明写入操作。
	ginx.MethodPost
	// OwnerKind 是原运行的类型。
	OwnerKind constants.RunOwnerKind `in:"path" name:"ownerKind" validate:"required"`
	// OwnerID 是不可变的原运行标识。
	OwnerID string `in:"path" name:"ownerId" validate:"required,len=32"`
}

func (*MarkRunViewed) Path() string { return "/:ownerKind/:ownerId/viewing" }
func (d *MarkRunViewed) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.RunViewingControllerManager.MarkViewed(ctx.Request.Context(), constants.LocalReaderID, models.RunOwnerRef{OwnerKind: d.OwnerKind, OwnerID: d.OwnerID}); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
