package wakers

import (
	"context"
	"errors"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新文档请求
type UpdateWakerDocumentRequest struct {
	// 文件内容
	Content string `json:"content" validate:"max=1048576"`
}

// 更新伙伴文档
type UpdateWakerDocument struct {
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 文档种类
	Kind constants.ProfileDocumentKind `in:"path" name:"kind" validate:"required"`
	// 请求体
	Body UpdateWakerDocumentRequest `in:"body"`
}

// Path 返回更新伙伴文档接口路径。
func (u *UpdateWakerDocument) Path() string {
	return "/:wakerId/documents/:kind"
}

// Validate 只允许更新三份伙伴档案。
func (u *UpdateWakerDocument) Validate(*gin.Context) error {
	if !isEditableProfileKind(u.Kind) {
		return ginx.WithStack(status_error.ProfileDocumentKindInvalid)
	}
	return nil
}

// Output 保存完整 Markdown；档案编辑不再重算关于我展示投影。
func (u *UpdateWakerDocument) Output(ctx *gin.Context) (interface{}, error) {
	if err := profile.ValidateContent(u.Body.Content); err != nil {
		return nil, ginx.WithStack(err)
	}
	now := time.Now().UnixMilli()
	err := global.TransactionControllerManager.Transaction(
		ctx,
		func(transactionContext context.Context) error {
			if _, err := global.WakerControllerManager.Get(transactionContext, u.WakerID); err != nil {
				return err
			}
			documents, err := global.ProfileDocumentControllerManager.ListWakerDocuments(
				transactionContext,
				u.WakerID,
			)
			if err != nil {
				return err
			}
			if _, err := builtinrolepreset.ResolveWakerContents(u.WakerID, documents); err != nil {
				return err
			}
			if _, err := global.ProfileDocumentControllerManager.GetWakerDocument(
				transactionContext,
				u.WakerID,
				u.Kind,
			); err != nil {
				if !errors.Is(err, status_error.ProfileDocumentNotFound) {
					return err
				}
				return global.ProfileDocumentControllerManager.CreateWakerDocuments(
					transactionContext,
					[]models.WakerDocument{{
						ID:        util.New(),
						WakerID:   u.WakerID,
						Kind:      u.Kind,
						Content:   u.Body.Content,
						CreatedAt: now,
						UpdatedAt: now,
					}},
				)
			}
			return global.ProfileDocumentControllerManager.UpdateWakerDocument(
				transactionContext,
				u.WakerID,
				u.Kind,
				u.Body.Content,
				now,
			)
		},
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
