package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 核查本地已发布 Wiki；不修改正文或来源状态。
type AuditLibrary struct {
	ginx.MethodGet
	// 知识库标识。
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

func (o *AuditLibrary) Path() string { return "/libraries/:libraryId/audit" }
func (o *AuditLibrary) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	v, err := global.KnowledgeEngine.LintLibrary(ctx, o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return v, nil
}

// 导出同一 SQLite 快照中的 Markdown；仅允许通过页面身份选择范围。
type ExportLibraryMarkdown struct {
	ginx.MethodGet
	// 知识库标识。
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 可选页面标识；重复 query 参数表示多个页面，空集合表示全部页面。
	PageIDs []string `in:"query" name:"pageId"`
}

func (o *ExportLibraryMarkdown) Path() string { return "/libraries/:libraryId/markdown-export" }
func (o *ExportLibraryMarkdown) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	v, err := global.KnowledgeEngine.ExportMarkdown(ctx, core.ExportInput{LibraryID: o.LibraryID, PageIDs: o.PageIDs})
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return v, nil
}

func init() {
	Router.Register(&AuditLibrary{})
	Router.Register(&ExportLibraryMarkdown{})
}
