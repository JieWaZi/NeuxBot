package plugins

import (
	"errors"

	"github.com/JieWaZi/NeuxBot/server/apis/mcp"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查看目录连接器
type GetCatalogPluginConnector struct {
	ginx.MethodGet
	// 目录条目
	Key string `in:"path" name:"key" validate:"required"`
	// 组件标识
	ConnectorKey string `in:"query" name:"connectorKey" validate:"required"`
	// 来源提交
	SourceCommit string `in:"query" name:"sourceCommit"`
	// 压缩摘要
	ArchiveSHA256 string `in:"query" name:"archiveSHA256"`
}

// Path 返回未安装插件连接器的只读入口。
func (*GetCatalogPluginConnector) Path() string { return "/catalog/:key/connector" }

// Output 绑定已展示的目录来源，复用只读详情加载，不准备安装或执行授权。
func (o *GetCatalogPluginConnector) Output(ctx *gin.Context) (interface{}, error) {
	entry, err := catalog.Find(global.PluginCatalogSource(), o.Key)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if (o.SourceCommit != "" && o.SourceCommit != entry.SourceCommit) || (o.ArchiveSHA256 != "" && o.ArchiveSHA256 != entry.ArchiveSHA256) {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	details, err := catalog.ReadDetails(entry)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// 公开资料已通过目录白名单校验，不包含安装包的配置与认证秘密绑定。
	var result mcp.ConnectorResponse
	found := false
	for _, server := range details.Package.Servers {
		if server.Key != o.ConnectorKey {
			continue
		}
		result, err = pluginConnectorSummary(server)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		found = true
		break
	}
	if !found {
		return nil, ginx.WithStack(status_error.PluginContentInvalid)
	}
	resource, lookupErr := global.PluginControllerManager.FindSource(ctx, entry.SourceKey)
	if lookupErr != nil && !errors.Is(lookupErr, status_error.PluginNotFound) {
		return nil, ginx.WithStack(lookupErr)
	}
	if lookupErr == nil {
		versions, err := global.PluginControllerManager.Versions(ctx, resource.ID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		for _, version := range versions {
			if version.Provenance["sourceCommit"] == entry.SourceCommit && version.Provenance["archiveSHA256"] == entry.ArchiveSHA256 {
				if err := pluginConnectorDiagnostics.apply(ctx, version, o.ConnectorKey, &result); err != nil {
					return nil, ginx.WithStack(err)
				}
				break
			}
		}
	}
	return result, nil
}
