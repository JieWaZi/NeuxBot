package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 重新识别远端服务
type RedetectConnection struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 请求体
	Body RedetectConnectionInput `in:"body"`
}

// 远端识别请求
type RedetectConnectionInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=64"`
	// 连接版本
	ExpectedRevision int64 `json:"expectedRevision" validate:"min=1"`
}

func (*RedetectConnection) Path() string { return "/:connectionId/detection" }
func (o *RedetectConnection) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeConnectionControllerManager == nil || global.KnowledgeRemoteMountControllerManager == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	current, key, err := global.KnowledgeConnectionControllerManager.Snapshot(ctx.Request.Context(), o.ConnectionID)
	if err != nil {
		return nil, err
	}
	if current.Backend != "integration" {
		return nil, status_error.KnowledgeUnsupported
	}
	mounts, err := global.KnowledgeRemoteMountControllerManager.List(ctx.Request.Context(), current.ID)
	if err != nil {
		return nil, err
	}
	if len(mounts) != 1 {
		return nil, status_error.KnowledgeInvalid
	}
	detected, err := remote.Detect(ctx.Request.Context(), remote.Options{BaseURL: current.BaseURL, Token: key}, mounts[0].KnowledgeBaseID)
	if err != nil {
		return nil, remoteError(err)
	}
	current.Detection = detected
	result, err := global.KnowledgeConnectionControllerManager.Save(ctx.Request.Context(), controllers.SaveKnowledgeConnectionInput{RequestID: o.Body.RequestID, ExpectedRevision: o.Body.ExpectedRevision, Connection: current})
	if err != nil {
		return nil, err
	}
	current.Revision = result.Revision
	return connectionDTO(current), nil
}
