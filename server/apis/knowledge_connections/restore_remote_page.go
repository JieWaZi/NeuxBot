package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 恢复远端页面
type RestoreRemotePage struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
	// 请求体
	Body knowledge.RestorePageInput `in:"body"`
}

func (o *RestoreRemotePage) Path() string {
	return "/:connectionId/libraries/:libraryId/pages/:pageId/restore"
}
func (o *RestoreRemotePage) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	return remoteInvoke(ctx, c, o.Body.RequestID, "page.restore", o.Body, func() (*core.Receipt, error) {
		return c.custom.RestorePage(ctx, core.RestorePageInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, PageID: o.PageID, ExpectedVersion: o.Body.ExpectedVersion, RevisionVersion: o.Body.RevisionVersion, StructureVersion: o.Body.StructureVersion})
	})
}
