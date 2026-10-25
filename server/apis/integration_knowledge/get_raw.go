package integration_knowledge

import (
	"mime"

	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 下载集成原件
type PublishedGetRaw struct {
	ginx.MethodGet
	// 资料标识
	ID string `in:"path" name:"id" validate:"required"`
}

// Path 定位原件下载路径。
func (*PublishedGetRaw) Path() string { return "/knowledge/:id/download" }

// Output 只读取核心受管原件，不接受任意本机路径。
func (o *PublishedGetRaw) Output(ctx *gin.Context) (interface{}, error) {
	d := dependencies(ctx)
	auth, _ := middleware.IntegrationAuthorization(ctx.Request.Context())
	source, err := d.engine.GetSource(ctx.Request.Context(), auth.ResourceID, o.ID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	raw, err := d.engine.ReadRaw(ctx.Request.Context(), auth.ResourceID, source.ID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	ctx.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": source.Filename}))
	ctx.Data(200, source.MIME, raw)
	return nil, nil
}
