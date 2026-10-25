package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListSourcePurges 获取已接纳的来源清理清单，关闭弹窗后仍能恢复。
type ListSourcePurges struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

func (o *ListSourcePurges) Path() string { return "/libraries/:libraryId/source-purges" }
func (o *ListSourcePurges) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	limit := o.Limit
	if limit == 0 {
		limit = 50
	}
	v, err := global.KnowledgeEngine.ListSourcePurges(ctx, o.LibraryID, o.Offset, limit)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return v, nil
}
func init() { Router.Register(&ListSourcePurges{}) }
