package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取集成资料
type PublishedGetSource struct {
	ginx.MethodGet
	// 资料标识
	ID string `in:"path" name:"id" validate:"required"`
}

// Path 定位按实际资料身份读取的路径。
func (*PublishedGetSource) Path() string { return "/knowledge/:id" }

// Output 使用鉴权阶段核实的真实所属库，不接受客户端拼接归属。
func (o *PublishedGetSource) Output(ctx *gin.Context) (interface{}, error) {
	auth, _ := middleware.IntegrationAuthorization(ctx.Request.Context())
	source, err := dependencies(ctx).engine.GetSource(ctx.Request.Context(), auth.ResourceID, o.ID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return SourceResponse{Success: true, Data: sourceDTO(source)}, nil
}
