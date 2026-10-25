package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 整理远端知识
type OrganizeRemote struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body RemoteOrganizeInput `in:"body"`
}

func (o *OrganizeRemote) Path() string { return "/:connectionId/libraries/:libraryId/organize" }
func (o *OrganizeRemote) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	return remoteInvoke(ctx, c, o.Body.RequestID, "job.accept", o.Body, func() (*core.Receipt, error) {
		return c.custom.Organize(ctx, remote.OrganizeRequest{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion, StructureVersion: o.Body.StructureVersion, SourceVersionIDs: o.Body.SourceVersionIDs, ProfileID: o.Body.ProfileID, Rebuild: o.Body.Rebuild, Instruction: o.Body.Instruction})
	})
}
