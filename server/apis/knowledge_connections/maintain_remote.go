package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 维护远端页面
type MaintainRemote struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body RemoteMaintenanceInput `in:"body"`
}

func (o *MaintainRemote) Path() string { return "/:connectionId/libraries/:libraryId/maintenance" }
func (o *MaintainRemote) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	return remoteInvoke(ctx, c, o.Body.RequestID, "maintenance.request", o.Body, func() (*core.Receipt, error) {
		return c.custom.Maintain(ctx, remote.MaintenanceRequest{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion, StructureVersion: o.Body.StructureVersion, ProfileID: o.Body.ProfileID, Instruction: o.Body.Instruction, PageID: o.Body.PageID, ExpectedPageVersion: o.Body.ExpectedPageVersion})
	})
}
