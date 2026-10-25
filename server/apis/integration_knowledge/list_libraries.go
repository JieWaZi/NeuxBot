package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 列出集成知识库
type PublishedListLibraries struct{ ginx.MethodGet }

// Path 定位集合路径。
func (*PublishedListLibraries) Path() string { return "/knowledge-bases" }

// Output 仅按已授权 ID 读取，不暴露其他库的名称或总数。
func (*PublishedListLibraries) Output(ctx *gin.Context) (interface{}, error) {
	d := dependencies(ctx)
	identity, _ := middleware.IntegrationPrincipal(ctx.Request.Context())
	ids, err := d.auth.ResourceIDs(ctx.Request.Context(), identity, constants.IntegrationResourceKindKnowledge, []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve})
	if err != nil {
		return nil, err
	}
	data := []PublishedLibrary{}
	for _, id := range ids {
		library, err := d.engine.GetLibrary(ctx.Request.Context(), id)
		if err != nil {
			return nil, bridge.MapError(err)
		}
		data = append(data, libraryDTO(library))
	}
	return LibraryListResponse{Success: true, Data: data}, nil
}
