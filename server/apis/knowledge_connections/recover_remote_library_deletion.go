package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 恢复远端知识库删除清理
type RecoverRemoteLibraryDeletion struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body knowledge.DeleteLibraryInput `in:"body"`
}

// Path 返回知识库删除操作的路由。
func (o *RecoverRemoteLibraryDeletion) Path() string {
	return "/:connectionId/libraries/:libraryId/deletion/recover"
}

// Output 执行已确认的删除操作并保留原请求回执。
func (o *RecoverRemoteLibraryDeletion) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	input := core.RecoverLibraryDeletionInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion}
	return remoteInvoke(ctx, c, o.Body.RequestID, "library.delete.recover", input, func() (*core.Receipt, error) { return c.custom.RecoverLibraryDeletion(ctx, input) })
}
