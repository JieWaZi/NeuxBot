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
	// 运行确认
	TrustConfirmed bool `json:"trustConfirmed"`
	// 预期归档摘要
	ExpectedArchiveSHA256 string `json:"expectedArchiveSHA256,omitempty"`
}

// Path 返回目录项安装准备入口。
func (*PrepareCatalogPlugin) Path() string { return "/catalog/:key/prepare" }

// Output 核验目录固定归档后登记资源与版本，不建立伙伴安装关系。
func (o *PrepareCatalogPlugin) Output(ctx *gin.Context) (interface{}, error) {
	entry, directory, err := catalog.ResolveCurrent(ctx.Request.Context(), global.PluginCatalogSource(), o.Key)
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
	resource, version, err := prepareCatalogResource(ctx, stage, o.Body.TrustConfirmed, entry)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return pluginDetail(ctx, resource, version)
}

// prepareCatalogResource 仅接收当前目录核验的来源快照，不暴露任意导入入口。
func prepareCatalogResource(ctx context.Context, stage packageplugin.StagedPackage, trusted bool, entry catalog.Entry) (models.Plugin, string, error) {
	if stage.Package.RequiresTrust() && !trusted {
		return models.Plugin{}, "", status_error.MCPConnectorTrustRequired
	}
	_, err := packageplugin.ClientExtension(stage.Package)
	if err != nil {
		return models.Plugin{}, "", err
	}
	provenance := map[string]any{}
	if data, readErr := os.ReadFile(filepath.Join(stage.Root, "provenance.json")); readErr == nil {
		if json.Unmarshal(data, &provenance) != nil {
			return models.Plugin{}, "", status_error.PluginContentInvalid
		}
	}
	if provenance == nil {
		return models.Plugin{}, "", status_error.PluginContentInvalid
	}
	provenance["sourceCommit"] = entry.SourceCommit
	provenance["archiveSHA256"] = entry.ArchiveSHA256
	provenance["archiveURL"] = entry.ArchiveURL
	name := stage.Package.Presentation.DisplayName
	if name == "" {
		name = stage.Package.Manifest.Name
	}
	return persistPreparedResource(ctx, stage, trusted, models.Plugin{ID: util.New(), Name: name, Description: stage.Package.Manifest.Description, SourceKey: entry.SourceKey}, provenance)
}

// persistPreparedResource 为目录准备和本地技能封装复用不可变内容与资源提交边界。
func persistPreparedResource(ctx context.Context, stage packageplugin.StagedPackage, trusted bool, resource models.Plugin, provenance map[string]any) (models.Plugin, string, error) {
	if stage.Package.RequiresTrust() && !trusted {
		return models.Plugin{}, "", status_error.MCPConnectorTrustRequired
	}
	if _, err := packageplugin.ClientExtension(stage.Package); err != nil {
		return models.Plugin{}, "", err
	}
	root, err := packageplugin.PackageRoot(stage.Root, stage.Package)
	if err != nil {
		return models.Plugin{}, "", err
	}
	guard := func() func() {
		if global.PluginContents != nil {
			return global.PluginContents.Guard()
		}
		return func() {}
	}
	// 在内容边界内固定资源身份，随后在边界外生成暂存目标。
	// 并发准备可能先提交另一身份；发布原包前复核，并按实际身份重试生成。
	for attempt := 0; ; attempt++ {
		release := guard()
		existing, err := global.PluginControllerManager.FindSource(ctx, resource.SourceKey)
		if existing.ID != "" {
			resource = existing
		}
		release()
		if err != nil {
			return models.Plugin{}, "", err
		}
		if err := ctx.Err(); err != nil {
			return models.Plugin{}, "", err
		}
		if _, err := packageplugin.PrepareTargets(ctx, filepath.Join(global.Config.DataDirectory, "plugins", "targets"), resource.ID, stage.Package, root); err != nil {
			return models.Plugin{}, "", err
		}
		release = guard()
		current, err := global.PluginControllerManager.FindSource(ctx, resource.SourceKey)
		if err != nil {
			release()
			return models.Plugin{}, "", err
		}
		if current.ID != "" && current.ID != resource.ID {
			resource = current
			release()
			if attempt == 2 {
				return models.Plugin{}, "", status_error.PluginVersionConflict
			}
			continue
		}
		defer release()
		break
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
	version := models.PluginVersion{ID: util.New(), PluginID: resource.ID, Version: versionName, ContentPath: filepath.ToSlash(relative), Digest: stage.Digest, Package: stage.Package, Provenance: provenance}
	if trusted {
		version.TrustedAt = time.Now().UnixMilli()
	}
	preparedVersionID := ""
	err = global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		existing, err := global.PluginControllerManager.FindSource(txctx, resource.SourceKey)
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
