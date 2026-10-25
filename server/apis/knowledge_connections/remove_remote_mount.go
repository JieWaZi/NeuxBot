package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 移除远端知识库入口
type RemoveRemoteMount struct {
	ginx.MethodDelete
	// 挂载标识
	MountID string `in:"path" name:"mountId" validate:"required"`
	// 入口版本
	Revision int64 `in:"query" name:"revision" validate:"min=1"`
}

func (*RemoveRemoteMount) Path() string { return "/mounts/:mountId" }
func (o *RemoveRemoteMount) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeRemoteMountControllerManager == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	if err := global.KnowledgeRemoteMountControllerManager.Remove(ctx.Request.Context(), o.MountID, o.Revision); err != nil {
		return nil, err
	}
	return gin.H{"removed": true}, nil
}
