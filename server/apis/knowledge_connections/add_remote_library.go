package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 远端知识库接入请求
type AddRemoteLibraryInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=64"`
	// 远程地址
	URL string `json:"url" validate:"required,max=4096"`
	// 集成密钥
	APIKey string `json:"apiKey" validate:"required,max=16384"`
	// 知识库标识
	KnowledgeBaseID string `json:"knowledgeBaseId" validate:"required,max=200"`
}

// 远端知识挂载回执
type RemoteMountReceipt struct {
	// 指定库挂载
	Mount models.KnowledgeRemoteMount `json:"mount"`
	// 已验证连接
	Connection Connection `json:"connection"`
}

// 添加指定远端知识库
type AddRemoteLibrary struct {
	ginx.MethodPost
	// 接入配置
	Body AddRemoteLibraryInput `in:"body"`
}

func (*AddRemoteLibrary) Path() string { return "/mounts" }
func (o *AddRemoteLibrary) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeRemoteMountControllerManager == nil || global.KnowledgeConnectionControllerManager == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	identity, err := requestidempotency.New(o.Body.RequestID, nil)
	if err != nil {
		return nil, err
	}
	base, err := remote.NormalizeAPIBaseURL(o.Body.URL)
	if err != nil {
		return nil, remoteError(err)
	}
	if o.Body.APIKey == "" || o.Body.KnowledgeBaseID == "" {
		return nil, status_error.KnowledgeInvalid
	}
	in := controllers.AddKnowledgeRemoteMountInput{RequestID: identity.ResourceID("remote-mount-add"), ConnectionID: identity.ResourceID("remote-mount-connection"), MountID: identity.ResourceID("remote-mount"), Credential: o.Body.APIKey, Detection: remote.Detection{BaseURL: base, Library: remote.Library{ID: o.Body.KnowledgeBaseID}}}
	mount, exists, err := global.KnowledgeRemoteMountControllerManager.Replay(ctx.Request.Context(), in)
	if err != nil {
		return nil, err
	}
	if !exists {
		detection, err := remote.Detect(ctx.Request.Context(), remote.Options{BaseURL: base, Token: o.Body.APIKey, AuthMode: "api_key"}, o.Body.KnowledgeBaseID)
		if err != nil {
			return nil, remoteError(err)
		}
		in.Detection = detection
		mount, err = global.KnowledgeRemoteMountControllerManager.Add(ctx.Request.Context(), in)
		if err != nil {
			return nil, err
		}
	}
	connection, err := global.KnowledgeConnectionControllerManager.Get(ctx.Request.Context(), mount.ConnectionID)
	if err != nil {
		return nil, err
	}
	return RemoteMountReceipt{Mount: mount, Connection: connectionDTO(connection)}, nil
}
