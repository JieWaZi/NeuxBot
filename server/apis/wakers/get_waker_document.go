package wakers

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取伙伴文档
type GetWakerDocument struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 文档种类
	Kind constants.ProfileDocumentKind `in:"path" name:"kind" validate:"required"`
}

// Path 返回单份伙伴文档接口路径。
func (g *GetWakerDocument) Path() string {
	return "/:wakerId/documents/:kind"
}

// Validate 校验文档种类。
func (g *GetWakerDocument) Validate(*gin.Context) error {
	if !validProfileDocumentKind(g.Kind) {
		return ginx.WithStack(status_error.ProfileDocumentKindInvalid)
	}
	return nil
}

// Output 返回一份完整伙伴文档。
func (g *GetWakerDocument) Output(ctx *gin.Context) (interface{}, error) {
	waker, err := global.WakerControllerManager.Get(ctx, g.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	rows, err := global.ProfileDocumentControllerManager.ListWakerDocuments(ctx, g.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	documents, err := builtinrolepreset.ResolvedWakerDocuments(
		g.WakerID,
		rows,
		waker.UpdatedAt,
		true,
		true,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	for _, document := range documents {
		if document.Kind == g.Kind {
			return document, nil
		}
	}
	return nil, ginx.WithStack(status_error.ProfileDocumentNotFound)
}
