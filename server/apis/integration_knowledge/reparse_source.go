package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 重建集成资料
type PublishedReparseSource struct {
	ginx.MethodPost
	// 来源版本
	SourceID string `in:"path" name:"id" validate:"required"`
	// 重建要求
	Body PublishedOrganizeInput `in:"body"`
}

func (*PublishedReparseSource) Path() string { return "/knowledge/:id/reparse" }
func (o *PublishedReparseSource) Output(ctx *gin.Context) (interface{}, error) {
	auth, _ := middleware.IntegrationAuthorization(ctx.Request.Context())
	o.Body.SourceVersionIDs = []string{o.SourceID}
	o.Body.Rebuild = true
	return organize(ctx, auth.ResourceID, o.Body)
}
