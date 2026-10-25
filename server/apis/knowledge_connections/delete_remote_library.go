package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 永久删除远端知识库
type DeleteRemoteLibrary struct {
	ginx.MethodDelete
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body knowledge.DeleteLibraryInput `in:"body"`
}

// Path 返回知识库删除操作的路由。
func (o *DeleteRemoteLibrary) Path() string { return "/:connectionId/libraries/:libraryId" }

// Output 执行已确认的删除操作并保留原请求回执。
func (o *DeleteRemoteLibrary) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	r, err := remoteInvoke(ctx, c, o.Body.RequestID, "library.delete", o.Body, func() (*core.Receipt, error) {
		return c.custom.DeleteLibrary(ctx, core.DeleteLibraryInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion})
	})
	if err != nil {
		return nil, err
	}
	if err := global.KnowledgeControllerManager.DeleteLibraryReferences(ctx, c.connection.Backend, c.connection.ID, o.LibraryID); err != nil {
		return nil, err
	}
	return r, nil
}
