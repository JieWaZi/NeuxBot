package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"

	"github.com/JieWaZi/neux-knowledge/core"
)

// 恢复正文请求
type RestorePageInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 当前版本
	ExpectedVersion int `json:"expectedVersion" validate:"min=1"`
	// 历史版本
	RevisionVersion int `json:"revisionVersion" validate:"min=1"`
	// 目录版本
	StructureVersion int64 `json:"structureVersion" validate:"min=1"`
}

// 恢复页面正文
type RestorePage struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
	// 请求体
	Body RestorePageInput `in:"body"`
}

func (o *RestorePage) Path() string { return "/libraries/:libraryId/pages/:pageId/restore" }
func (o *RestorePage) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	r, err := executeWrite(ctx, o.Body.RequestID, "page.restore", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		return global.KnowledgeEngine.RestorePage(ctx, core.RestorePageInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, PageID: o.PageID, ExpectedVersion: o.Body.ExpectedVersion, RevisionVersion: o.Body.RevisionVersion, StructureVersion: o.Body.StructureVersion})
	})
	if err != nil {
		return nil, err
	}
	return receiptDTO(r), nil
}
