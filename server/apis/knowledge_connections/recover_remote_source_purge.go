package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// RecoverRemoteSourcePurge 使用完整远端身份代理原件清理；不在本机执行删除。
type RecoverRemoteSourcePurge struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 清理标识
	PurgeID string `in:"path" name:"purgeId" validate:"required"`
	// 请求体
	Body knowledge.RecoverSourcePurgeInput `in:"body"`
}

func (o *RecoverRemoteSourcePurge) Path() string {
	return "/:connectionId/libraries/:libraryId/source-purges/:purgeId/recover"
}
func (o *RecoverRemoteSourcePurge) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	input := core.RecoverSourcePurgeInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, PurgeID: o.PurgeID, ExpectedPolicyVersion: o.Body.ExpectedPolicyVersion}
	return remoteInvoke(ctx, c, o.Body.RequestID, "source.purge.recover", input, func() (*core.Receipt, error) { return c.custom.RecoverSourcePurge(ctx, input) })
}
func init() { Router.Register(&RecoverRemoteSourcePurge{}) }
