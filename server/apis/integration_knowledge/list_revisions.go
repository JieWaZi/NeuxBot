package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// Wiki 历史响应
type RevisionListResponse struct {
	// 历史版本
	Revisions []*core.Revision `json:"revisions"`
	// 实际总数
	Total int64 `json:"total"`
	// 当前版本
	CurrentVersion int `json:"current_version"`
}

// 列出集成 Wiki 历史
type PublishedListRevisions struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 分层句柄
	Slug string `in:"path" name:"slug" validate:"required"`
	// 起始位置
	Offset int `in:"query" name:"offset"`
	// 每页数量
	Limit int `in:"query" name:"limit"`
	// 正文版本
	ContentVersion int `in:"query" name:"content_version"`
}

// Path 定位历史句柄路径。
func (*PublishedListRevisions) Path() string { return "/knowledgebase/:kb_id/wiki/revisions/*slug" }

// Output 区分历史列表和明确历史正文，不将当前页混入历史列表。
func (o *PublishedListRevisions) Output(ctx *gin.Context) (interface{}, error) {
	if o.Limit == 0 {
		o.Limit = 50
	}
	if o.Offset < 0 || o.Offset > 10000000 || o.Limit < 1 || o.Limit > 100 || o.ContentVersion < 0 {
		return nil, status_error.KnowledgeInvalid
	}
	slug, err := pathSlug(o.Slug)
	if err != nil {
		return nil, err
	}
	e := dependencies(ctx).engine
	page, err := e.GetPageBySlug(ctx.Request.Context(), o.LibraryID, slug)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	if o.ContentVersion > 0 {
		revision, err := e.GetRevision(ctx.Request.Context(), o.LibraryID, page.ID, o.ContentVersion)
		if err != nil {
			return nil, bridge.MapError(err)
		}
		return RevisionListResponse{Revisions: []*core.Revision{revision}, Total: 1, CurrentVersion: page.Version}, nil
	}
	rows, total, err := e.ListRevisions(ctx.Request.Context(), o.LibraryID, page.ID, o.Offset, o.Limit)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	if rows == nil {
		rows = []*core.Revision{}
	}
	return RevisionListResponse{Revisions: rows, Total: total, CurrentVersion: page.Version}, nil
}
