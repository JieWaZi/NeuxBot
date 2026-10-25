package integration_knowledge

import (
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取集成知识库
type PublishedGetLibrary struct {
	ginx.MethodGet
	// 知识库标识
	ID string `in:"path" name:"id" validate:"required"`
}

// Path 保留 WeKnora 的实际库路径。
func (*PublishedGetLibrary) Path() string { return "/knowledge-bases/:id" }

// Output 返回同一核心的真实库详情。
func (o *PublishedGetLibrary) Output(ctx *gin.Context) (interface{}, error) {
	lib, err := dependencies(ctx).engine.GetLibrary(ctx.Request.Context(), o.ID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return LibraryResponse{Success: true, Data: libraryDTO(lib)}, nil
}
