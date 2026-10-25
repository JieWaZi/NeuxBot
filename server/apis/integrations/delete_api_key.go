package integrations

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除 API Key
type DeleteAPIKey struct {
	ginx.MethodDelete
	// 密钥标识
	KeyID string `in:"path" name:"keyId" validate:"required,max=32"`
	// 请求体
	Body APIKeyRevisionInput `in:"body"`
}

// Path 返回密钥删除入口。
func (*DeleteAPIKey) Path() string { return "/keys/:keyId" }

// Output 在授权提交屏障内删除密钥，阻止后续请求和迟到知识提交。
func (o *DeleteAPIKey) Output(ctx *gin.Context) (interface{}, error) {
	if err := available(); err != nil {
		return nil, err
	}
	err := global.IntegrationService.WithAuthorizationWrite(ctx, func(tx context.Context) error {
		return global.IntegrationKeyControllerManager.Delete(tx, o.KeyID, o.Body.ExpectedRevision)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
