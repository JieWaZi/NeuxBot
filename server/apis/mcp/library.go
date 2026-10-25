package mcp

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"strings"
)

// ListLibraryConnectors 分页查询全局资源。
type ListLibraryConnectors struct {
	ginx.MethodGet
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 当前页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// Path 返回全局资源列表入口。
func (*ListLibraryConnectors) Path() string { return "" }

// ListLibraryConnectorsResponse 是准确分页结果。
type ListLibraryConnectorsResponse struct {
	// 资源列表
	Resources []ConnectorResponse `json:"resources"`
	// 准确总数
	TotalSize int64 `json:"totalSize"`
	// 当前页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
}

// Output 使用单表分页和有界关联补齐。
func (o *ListLibraryConnectors) Output(ctx *gin.Context) (interface{}, error) {
	page := max(o.PageNum, 1)
	size := o.PageSize
	if size < 1 || size > 100 {
		size = 24
	}
	var items []models.MCPConnectorWithState
	var total int64
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		var err error
		items, total, err = global.MCPConnectorControllerManager.ListLibrary(txctx, strings.TrimSpace(o.Query), (page-1)*size, size)
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources, err := connectorResponses(ctx, items)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListLibraryConnectorsResponse{Resources: resources, TotalSize: total, PageNum: page, PageSize: size}, nil
}

// InstallLibraryConnectorRequest 描述明确选择的安装伙伴。
type InstallLibraryConnectorRequest struct {
	// 所选伙伴
	WakerIDs []string `json:"wakerIds" validate:"required,min=1,max=100,dive,required"`
}

// InstallLibraryConnector 只建立全局资源安装关系。
type InstallLibraryConnector struct {
	ginx.MethodPost
	// 资源标识
	ConnectorID string `in:"path" name:"connectorId" validate:"required"`
	// 请求体
	Body InstallLibraryConnectorRequest `in:"body"`
}

// Path 返回明确安装操作入口。
func (*InstallLibraryConnector) Path() string { return "/:connectorId/install" }

// Output 不复制共享内容或设置，重复安装保留原启停状态。
func (o *InstallLibraryConnector) Output(ctx *gin.Context) (interface{}, error) {
	if len(o.Body.WakerIDs) == 0 || len(o.Body.WakerIDs) > 100 {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		resource, err := global.MCPConnectorControllerManager.Get(txctx, o.ConnectorID)
		if err != nil {
			return err
		}
		if resource.OwnerWakerID != "" || resource.Scope != constants.MCPConnectorScopePublic {
			return status_error.BadRequest
		}
		if trustDigest(resource) != resource.TrustedConfigDigest {
			return status_error.MCPConnectorTrustRequired
		}
		for _, id := range o.Body.WakerIDs {
			if _, err := global.WakerControllerManager.Get(txctx, id); err != nil {
				return err
			}
			if err := global.MCPConnectorControllerManager.EnsureWakerConnector(txctx, models.WakerMCPConnector{ID: util.New(), WakerID: id, MCPConnectorID: o.ConnectorID, Enabled: true}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, o.ConnectorID)
	return ginx.CommonSuccessResponse(), nil
}
