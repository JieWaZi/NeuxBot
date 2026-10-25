package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 修订列表
type RevisionList struct {
	// 修订
	Items []Revision `json:"items"`
	// 总数
	Total int64 `json:"total"`
}

// 获取修订列表
type ListRevisions struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 分页参数
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
}

func (o *ListRevisions) Path() string { return "/libraries/:libraryId/pages/:pageId/revisions" }
func (o *ListRevisions) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	offset, limit := pagination(Pagination{Offset: o.Offset, Limit: o.Limit})
	rows, total, err := global.KnowledgeEngine.ListRevisions(ctx, o.LibraryID, o.PageID, offset, limit)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	items := []Revision{}
	for _, v := range rows {
		items = append(items, revisionDTO(v))
	}
	return RevisionList{Items: items, Total: total}, nil
}
