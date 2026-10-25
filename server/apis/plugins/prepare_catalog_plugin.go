package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"os"
	"path/filepath"
	"time"
)

// 准备目录插件
type PrepareCatalogPlugin struct {
	ginx.MethodPost
	// 目录条目
	Key string `in:"path" name:"key" validate:"required"`
	// 请求体
	Body PrepareCatalogPluginRequest `in:"body"`
}

// 目录准备请求
type PrepareCatalogPluginRequest struct {
	// 所属伙伴
	OwnerWakerID string `json:"ownerWakerId,omitempty"`
	// 运行确认
	TrustConfirmed bool `json:"trustConfirmed"`
	// 预期归档摘要
	ExpectedArchiveSHA256 string `json:"expectedArchiveSHA256,omitempty"`
}

// Path 返回目录项安装准备入口。
func (*PrepareCatalogPlugin) Path() string { return "/catalog/:key/prepare" }

// Output 核验目录固定归档后登记资源与版本，不建立伙伴安装关系。
func (o *PrepareCatalogPlugin) Output(ctx *gin.Context) (interface{}, error) {
	entry, directory, err := catalog.ResolveCurrent(global.PluginCatalogSource(), o.Key)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// 分发恢复可能重新解析目录；必须核验最终归档，不能静默替换用户确认的目标。
	if o.Body.ExpectedArchiveSHA256 != "" && o.Body.ExpectedArchiveSHA256 != entry.ArchiveSHA256 {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	stage, err := packageplugin.StageSource(stagedRoot(), directory, entry.PackagePath)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer os.RemoveAll(filepath.Join(stagedRoot(), stage.Token))
	resource, version, err := prepareCatalogResource(ctx, stage, o.Body.OwnerWakerID, o.Body.TrustConfirmed, entry)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return pluginDetail(ctx, resource, version)
}

// prepareCatalogResource 仅接收当前目录核验的来源快照，不暴露任意导入入口。
func prepareCatalogResource(ctx context.Context, stage packageplugin.StagedPackage, owner string, trusted bool, entry catalog.Entry) (models.Plugin, string, error) {
	if stage.Package.RequiresTrust() && !trusted {
		return models.Plugin{}, "", status_error.MCPConnectorTrustRequired
	}
	if owner != "" {
		if _, err := global.WakerControllerManager.Get(ctx, owner); err != nil {
			return models.Plugin{}, "", err
		}
	}
	extension, err := packageplugin.ClientExtension(stage.Package)
	if err != nil {
		return models.Plugin{}, "", err
	}
	source := entry.SourceKey
	resource, err := global.PluginControllerManager.FindSource(ctx, owner, source)
	if err != nil {
		return models.Plugin{}, "", err
	}
	if resource.ID == "" {
		name := extension.DisplayName
		if name == "" {
			name = stage.Package.Presentation.DisplayName
		}
		if name == "" {
			name = stage.Package.Manifest.Name
		}
		resource = models.Plugin{ID: util.New(), Name: name, Description: stage.Package.Manifest.Description, SourceKey: source, OwnerWakerID: owner}
	}
	if global.PluginContents != nil {
		release := global.PluginContents.Guard()
		defer release()
	}
	content, err := packageplugin.CopyContent(filepath.Join(global.Config.DataDirectory, "plugins", "content"), stage.Root, stage.Digest)
	if err != nil {
		return models.Plugin{}, "", err
	}
	relative, err := filepath.Rel(global.Config.DataDirectory, content)
	if err != nil {
		return models.Plugin{}, "", status_error.PluginStorageError
	}
	versionName := stage.Package.Manifest.Version
	if versionName == "" {
		versionName = stage.Digest[:12]
	}
	version := models.PluginVersion{ID: util.New(), PluginID: resource.ID, Version: versionName, ContentPath: filepath.ToSlash(relative), Digest: stage.Digest, Package: stage.Package, Provenance: map[string]any{}}
	if trusted {
		version.TrustedAt = time.Now().UnixMilli()
	}
	if data, readErr := os.ReadFile(filepath.Join(stage.Root, "provenance.json")); readErr == nil {
		if json.Unmarshal(data, &version.Provenance) != nil {
			return models.Plugin{}, "", status_error.PluginContentInvalid
		}
	}
	if version.Provenance == nil {
		return models.Plugin{}, "", status_error.PluginContentInvalid
	}
	version.Provenance["sourceCommit"] = entry.SourceCommit
	version.Provenance["archiveSHA256"] = entry.ArchiveSHA256
	version.Provenance["archiveURL"] = entry.ArchiveURL
	preparedVersionID := ""
	err = global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		existing, err := global.PluginControllerManager.FindSource(txctx, resource.OwnerWakerID, resource.SourceKey)
		if err != nil {
			return err
		}
		if existing.ID != "" {
			resource = existing
			version.PluginID = existing.ID
		} else {
			if _, err := global.PluginControllerManager.Get(txctx, resource.ID); errors.Is(err, status_error.PluginNotFound) {
				if err := global.PluginControllerManager.Create(txctx, resource); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		stored, err := global.PluginControllerManager.PutVersion(txctx, version)
		preparedVersionID = stored.ID
		return err
	})
	if err != nil {
		return models.Plugin{}, "", err
	}
	return resource, preparedVersionID, nil
}
