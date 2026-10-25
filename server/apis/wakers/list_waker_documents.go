package wakers

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 伙伴文档列表
type ListWakerDocuments struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
}

// Path 返回伙伴文档列表接口路径。
func (l *ListWakerDocuments) Path() string {
	return "/:wakerId/documents"
}

// Output 返回伙伴的三份可编辑档案文档元数据。
func (l *ListWakerDocuments) Output(ctx *gin.Context) (interface{}, error) {
	waker, err := global.WakerControllerManager.Get(ctx, l.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	rows, err := global.ProfileDocumentControllerManager.ListWakerDocuments(ctx, l.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	documents, err := builtinrolepreset.ResolvedWakerDocuments(
		l.WakerID,
		rows,
		waker.UpdatedAt,
		false,
		true,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return documents, nil
}
