package plugins

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
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
	// 优先读取固定摘要；旧目录缺少组件资料时，只临时展开该条目的已核验归档。
	if entry.DetailsURL != "" {
		details, err := catalog.ReadDetails(entry)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		entry.Package = details.ViewPackage("")
	}
	result, err := pluginConnectorResponse(entry.Package, o.ConnectorKey)
	if entry.DetailsURL == "" && errors.Is(err, status_error.PluginContentInvalid) {
		archive, resolveErr := catalog.ResolveEntry(global.PluginCatalogSource(), entry)
		if resolveErr != nil {
			return nil, ginx.WithStack(resolveErr)
		}
		stage, stageErr := packageplugin.StageSource(stagedRoot(), archive, entry.PackagePath)
		if stageErr != nil {
			return nil, ginx.WithStack(stageErr)
		}
		defer os.RemoveAll(filepath.Join(stagedRoot(), stage.Token))
		result, err = pluginConnectorResponse(stage.Package, o.ConnectorKey)
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resource, lookupErr := global.PluginControllerManager.FindSource(ctx, "", entry.SourceKey)
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
