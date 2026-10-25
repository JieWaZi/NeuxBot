package integrations

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/global"
	rules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建 API Key
type CreateAPIKey struct {
	ginx.MethodPost
	// 请求体
	Body CreateAPIKeyInput `in:"body"`
}

// Path 返回本机签发入口。
func (*CreateAPIKey) Path() string { return "/keys" }

// Output 先预留稳定意图，再生成仅本次响应可见的秘密。
func (o *CreateAPIKey) Output(ctx *gin.Context) (interface{}, error) {
	ctx.Header("Cache-Control", "no-store")
	ctx.Header("Pragma", "no-cache")
	if err := available(); err != nil {
		return nil, err
	}
	name, grants, err := normalizeInput(o.Body.Name, o.Body.Grants)
	if err != nil {
		return nil, err
	}
	normalized := CreateAPIKeyInput{Name: name, Grants: grants, ExpiresAt: o.Body.ExpiresAt}
	identity, err := requestidempotency.New(o.Body.RequestID, normalized)
	if err != nil {
		return nil, err
	}
	mutation, fresh, err := global.IntegrationKeyControllerManager.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: identity.ResourceID("integration-key-create"), Digest: identity.Hash, KeyID: identity.ResourceID("integration-key"), Action: "create"})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if !fresh {
		return replayIssuance(ctx, mutation)
	}
	secret, err := rules.GenerateKey()
	if err != nil {
		return nil, err
	}
	key := models.IntegrationKey{ID: mutation.KeyID, Name: name, Grants: grants, Enabled: true, ExpiresAt: o.Body.ExpiresAt, KeyHash: rules.HashKey(secret), DisplayPrefix: secret[:8]}
	err = global.IntegrationService.WithAuthorizationWrite(ctx, func(tx context.Context) error {
		if err := validateResources(tx, grants, o.Body.ExpiresAt); err != nil {
			return err
		}
		var saveErr error
		key, saveErr = global.IntegrationKeyControllerManager.CompleteMutation(tx, mutation.RequestID, identity.Hash, key, 0)
		return saveErr
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return APIKeyIssuance{KeyID: key.ID, Key: &key, Secret: secret, IssuanceState: "issued"}, nil
}
