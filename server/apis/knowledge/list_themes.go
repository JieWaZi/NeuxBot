package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 主题列表
type ThemeList struct {
	// 主题
	Items []Theme `json:"items"`
}

// 获取知识主题
type ListThemes struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

func (o *ListThemes) Path() string { return "/libraries/:libraryId/themes" }
func (o *ListThemes) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	rows, err := global.KnowledgeEngine.ListThemes(ctx, o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	items := []Theme{}
	for _, v := range rows {
		items = append(items, themeDTO(v))
	}
	return ThemeList{Items: items}, nil
}
