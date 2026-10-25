// Package integrations 提供仅供本机管理身份调用的 API 集成设置。
package integrations

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	rules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// 密钥签发结果
type APIKeyIssuance struct {
	// 主体标识
	KeyID string `json:"keyId"`
	// 密钥记录
	Key *models.IntegrationKey `json:"key,omitempty"`
	// 一次性秘密
	Secret string `json:"secret,omitempty"`
	// 签发状态
	IssuanceState string `json:"issuanceState"`
}

// 创建密钥请求
type CreateAPIKeyInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=200"`
	// 集成名称
	Name string `json:"name" validate:"required,max=120"`
	// 资源授权
	Grants []models.IntegrationGrant `json:"grants"`
	// 失效时间
	ExpiresAt *int64 `json:"expiresAt"`
}

// 修改密钥请求
type UpdateAPIKeyInput struct {
	// 预期版本
	ExpectedRevision int64 `json:"expectedRevision" validate:"min=1"`
	// 集成名称
	Name string `json:"name" validate:"required,max=120"`
	// 资源授权
	Grants []models.IntegrationGrant `json:"grants"`
	// 失效时间
	ExpiresAt *int64 `json:"expiresAt"`
}

// 密钥版本请求
type APIKeyRevisionInput struct {
	// 预期版本
	ExpectedRevision int64 `json:"expectedRevision" validate:"min=1"`
}

func available() error {
	if global.IntegrationService == nil || global.IntegrationKeyControllerManager == nil {
		return status_error.IntegrationListenerError
	}
	return nil
}

func normalizeInput(name string, grants []models.IntegrationGrant) (string, []models.IntegrationGrant, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return "", nil, status_error.IntegrationInvalid
	}
	normalized, err := rules.NormalizeGrants(grants)
	return name, normalized, err
}

func validateResources(ctx context.Context, grants []models.IntegrationGrant, expiresAt *int64) error {
	if expiresAt != nil && *expiresAt <= time.Now().UnixMilli() {
		return status_error.IntegrationInvalid
	}
	for _, grant := range grants {
		for _, id := range grant.ResourceIDs {
			if grant.ResourceKind == constants.IntegrationResourceKindKnowledge {
				if global.KnowledgeEngine == nil {
					return status_error.KnowledgeUnsupported
				}
				// 发布目录只取本机权威核心，不枚举连接，也不解密远端凭据。
				if _, err := global.KnowledgeEngine.GetLibrary(ctx, id); err != nil {
					return bridge.MapError(err)
				}
			} else {
				if global.AutomaticTaskControllerManager == nil {
					return status_error.IntegrationInvalid
				}
				task, err := global.AutomaticTaskControllerManager.Get(ctx, id)
				if err != nil {
					return err
				}
				if task.Trigger != constants.AutomaticTaskTriggerAPI || !task.Enabled || task.Deleting || task.WorkflowTriggerDetached {
					return status_error.IntegrationInvalid
				}
			}
		}
	}
	return nil
}

func replayIssuance(ctx context.Context, mutation models.IntegrationKeyMutation) (APIKeyIssuance, error) {
	result := APIKeyIssuance{KeyID: mutation.KeyID, IssuanceState: "incomplete"}
	if mutation.Completed {
		key, err := global.IntegrationKeyControllerManager.Get(ctx, mutation.KeyID)
		if err != nil {
			return result, err
		}
		result.Key, result.IssuanceState = &key, "secret_unavailable"
	}
	return result, nil
}
