package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 核查远端自有 Wiki；官方 WeKnora 尚无匹配的只读核查合同。
type AuditRemoteLibrary struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

func (o *AuditRemoteLibrary) Path() string { return "/:connectionId/libraries/:libraryId/audit" }
func (o *AuditRemoteLibrary) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	v, err := c.custom.LintLibrary(ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	return v, nil
}

// 导出远端自有 Wiki 的同一版本快照；官方 WeKnora 尚无匹配合同。
type ExportRemoteLibraryMarkdown struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 可选页面标识；重复 query 参数表示多个页面，空集合表示全部页面。
	PageIDs []string `in:"query" name:"pageId"`
}

func (o *ExportRemoteLibraryMarkdown) Path() string {
	return "/:connectionId/libraries/:libraryId/markdown-export"
}
func (o *ExportRemoteLibraryMarkdown) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	v, err := c.custom.ExportMarkdown(ctx, o.PageIDs)
	if err != nil {
		return nil, remoteError(err)
	}
	return v, nil
}

func init() {
	Router.Register(&AuditRemoteLibrary{})
	Router.Register(&ExportRemoteLibraryMarkdown{})
}
