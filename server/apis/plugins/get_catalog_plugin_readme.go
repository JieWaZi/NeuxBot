package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"os"
	"path/filepath"
)

// 查看目录插件原文
type GetCatalogPluginReadme struct {
	ginx.MethodGet
	// 展示语言
	Locale string `in:"query" name:"locale"`
	// 来源提交
	SourceCommit string `in:"query" name:"sourceCommit"`
	// 压缩摘要
	ArchiveSHA256 string `in:"query" name:"archiveSHA256"`
	// 目录条目
	Key string `in:"path" name:"key" validate:"required"`
}

// Path 返回未安装插件的固定来源原文。
func (*GetCatalogPluginReadme) Path() string { return "/catalog/:key/readme" }

// Output 只读取已核验来源，不建立系统资源或安装关系。
func (o *GetCatalogPluginReadme) Output(ctx *gin.Context) (interface{}, error) {
	entry, err := catalog.Find(global.PluginCatalogSource(), o.Key)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if (o.SourceCommit != "" && o.SourceCommit != entry.SourceCommit) || (o.ArchiveSHA256 != "" && o.ArchiveSHA256 != entry.ArchiveSHA256) {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	if entry.DetailsURL != "" {
		details, err := catalog.ReadDetails(entry)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		content, _, err := details.Readme(o.Locale)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		return PluginReadmeResponse{Content: content}, nil
	}
	archive, err := catalog.ResolveEntry(global.PluginCatalogSource(), entry)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	stage, err := packageplugin.StageSource(stagedRoot(), archive, entry.PackagePath)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer os.RemoveAll(filepath.Join(stagedRoot(), stage.Token))
	content, err := packageplugin.ReadRawReadme(stage.Root, stage.Package, o.Locale)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return PluginReadmeResponse{Content: content}, nil
}
