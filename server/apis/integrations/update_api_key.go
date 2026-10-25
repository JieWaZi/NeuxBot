package integrations

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 修改 API Key 授权
type UpdateAPIKey struct {
	ginx.MethodPut
	// 主体标识
	KeyID string `in:"path" name:"keyId" validate:"required,max=32"`
	// 请求体
	Body UpdateAPIKeyInput `in:"body"`
}

// Path 返回原主体的授权编辑入口。
func (*UpdateAPIKey) Path() string { return "/keys/:keyId" }

// Output 保留原凭据并条件替换授权，不静默覆盖并发编辑。
func (o *UpdateAPIKey) Output(ctx *gin.Context) (interface{}, error) {
	if err := available(); err != nil {
		return nil, err
	}
	name, grants, err := normalizeInput(o.Body.Name, o.Body.Grants)
	if err != nil {
		return nil, err
	}
	err = global.IntegrationService.WithAuthorizationWrite(ctx, func(tx context.Context) error {
		key, err := global.IntegrationKeyControllerManager.Get(tx, o.KeyID)
		if err != nil {
			return err
		}
		if key.Revision != o.Body.ExpectedRevision {
			return status_error.IntegrationConflict
		}
		if err := validateResources(tx, grants, o.Body.ExpiresAt); err != nil {
			return err
		}
		key.Name, key.Grants, key.ExpiresAt = name, grants, o.Body.ExpiresAt
		_, err = global.IntegrationKeyControllerManager.UpdateAuthorization(tx, key, o.Body.ExpectedRevision)
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
