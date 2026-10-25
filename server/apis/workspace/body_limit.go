package workspace

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"net/http"
	"strings"
)

// BodyLimit 在 JSON 绑定前限制文件操作请求，避免超大 base64 分配。
type BodyLimit struct {
	// MiddlewareType 声明请求体边界中间件。
	ginx.MiddlewareType
}

// Output 按操作允许有界编码开销；解码后的业务上限由文件服务再次检查。
func (*BodyLimit) Output(*gin.Context) (interface{}, error) {
	return gin.HandlerFunc(func(ctx *gin.Context) {
		limit := int64(2 << 20)
		if strings.HasSuffix(ctx.Request.URL.Path, "/import") {
			limit = 90 << 20
		}
		if strings.HasSuffix(ctx.Request.URL.Path, "/write") {
			limit = 64 << 20
		}
		if ctx.Request.Body != nil {
			ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, limit)
		}
		ctx.Next()
	}), nil
}
