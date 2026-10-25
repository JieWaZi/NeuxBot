package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
)

// 知识连接
type Connection struct {
	// 连接标识
	ID string `json:"id"`
	// 服务类型
	Backend string `json:"backend"`
	// 连接名称
	Name string `json:"name"`
	// 服务地址
	BaseURL string `json:"baseUrl"`
	// 认证类型
	AuthMode string `json:"authMode"`
	// 配置版本
	Revision int64 `json:"revision"`
	// 识别结果
	Detection remote.Detection `json:"detection"`
	// 已启用
	Enabled bool `json:"enabled"`
}

// 连接配置请求
type ConnectionInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 配置版本
	ExpectedRevision int64 `json:"expectedRevision" validate:"min=0"`
	// 服务类型
	Backend string `json:"backend" validate:"required,oneof=weknora custom integration"`
	// 连接名称
	Name string `json:"name" validate:"required,max=120"`
	// 服务地址
	BaseURL string `json:"baseUrl" validate:"required,max=4096"`
	// 认证类型
	AuthMode string `json:"authMode" validate:"required,oneof=bearer api_key"`
	// 认证凭据
	Credential string `json:"credential" validate:"max=16384"`
	// 已启用
	Enabled bool `json:"enabled"`
}

// 连接编辑回执
type ConnectionReceipt struct {
	// 连接标识
	ConnectionID string `json:"connectionId"`
	// 提交版本
	Revision int64 `json:"revision"`
}

func connectionDTO(v models.KnowledgeConnection) Connection {
	return Connection{ID: v.ID, Backend: v.Backend, Name: v.Name, BaseURL: v.BaseURL, AuthMode: v.AuthMode, Revision: v.Revision, Enabled: v.Enabled, Detection: v.Detection}
}
func save(ctx *gin.Context, id string, in ConnectionInput) (ConnectionReceipt, error) {
	if in.Backend == "custom" && in.AuthMode != "bearer" {
		return ConnectionReceipt{}, status_error.KnowledgeInvalid
	}
	if global.KnowledgeConnectionControllerManager == nil {
		return ConnectionReceipt{}, status_error.KnowledgeUnsupported
	}
	if id == "" {
		id = (requestidempotency.Identity{ID: in.RequestID}).ResourceID("knowledge_connection", "local")
	}
	value := models.KnowledgeConnection{ID: id, Name: in.Name, Backend: in.Backend, BaseURL: in.BaseURL, AuthMode: in.AuthMode, Enabled: in.Enabled}
	if in.Backend == "integration" {
		if in.ExpectedRevision < 1 || in.AuthMode != "api_key" || global.KnowledgeRemoteMountControllerManager == nil {
			return ConnectionReceipt{}, status_error.KnowledgeInvalid
		}
		old, secret, err := global.KnowledgeConnectionControllerManager.Snapshot(ctx.Request.Context(), id)
		if err != nil {
			return ConnectionReceipt{}, err
		}
		base, err := remote.NormalizeAPIBaseURL(in.BaseURL)
		if err != nil {
			return ConnectionReceipt{}, remoteError(err)
		}
		value.BaseURL = base
		value.Detection = old.Detection
		if base != old.BaseURL && in.Credential == "" {
			return ConnectionReceipt{}, status_error.KnowledgeInvalid
		}
		if base != old.BaseURL || in.Credential != "" {
			if in.Credential != "" {
				secret = in.Credential
			}
			mounts, err := global.KnowledgeRemoteMountControllerManager.List(ctx.Request.Context(), id)
			if err != nil {
				return ConnectionReceipt{}, err
			}
			if len(mounts) != 1 {
				return ConnectionReceipt{}, status_error.KnowledgeInvalid
			}
			value.Detection, err = remote.Detect(ctx.Request.Context(), remote.Options{BaseURL: base, Token: secret}, mounts[0].KnowledgeBaseID)
			if err != nil {
				return ConnectionReceipt{}, remoteError(err)
			}
		}
	}
	r, err := global.KnowledgeConnectionControllerManager.Save(ctx, controllers.SaveKnowledgeConnectionInput{RequestID: in.RequestID, ExpectedRevision: in.ExpectedRevision, Credential: in.Credential, Connection: value})
	return ConnectionReceipt{ConnectionID: r.ConnectionID, Revision: r.Revision}, err
}
