package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"time"
)

// 获取知识库详情
type GetLibrary struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

func (o *GetLibrary) Path() string { return "/libraries/:libraryId" }
func (o *GetLibrary) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	v, err := global.KnowledgeEngine.GetLibrary(ctx, o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	item := libraryDTO(v)
	_, total, err := global.KnowledgeEngine.ListPageDirectory(ctx, v.ID, 0, 1)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	item.PageTotal = &total
	item.PageTotalObservedAt = time.Now().UnixMilli()
	return item, nil
}
