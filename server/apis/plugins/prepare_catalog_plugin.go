package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
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
	var resource models.Plugin
	var version string
	err := catalog.WithCurrentArchive(ctx.Request.Context(), global.PluginCatalogSource(), o.Key, o.Body.ExpectedArchiveSHA256, func(entry catalog.Entry, directory string) error {
		stage, err := packageplugin.StageSource(stagedRoot(), directory, entry.PackagePath)
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage.Directory)
		resource, version, err = prepareCatalogResource(ctx, stage, o.Body.TrustConfirmed, entry)
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return pluginDetail(ctx, resource, version)
}

// prepareCatalogResource 仅接收当前目录核验的来源快照，不暴露任意导入入口。
func prepareCatalogResource(ctx context.Context, stage packageplugin.StagedPackage, trusted bool, entry catalog.Entry) (models.Plugin, string, error) {
	if stage.Package.Manifest.Name != entry.Package.Manifest.Name || stage.Package.Manifest.Version != entry.Package.Manifest.Version {
		return models.Plugin{}, "", status_error.PluginVersionConflict
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
	provenance["packagePath"] = entry.PackagePath
	name := stage.Package.Presentation.DisplayName
	if name == "" {
		name = stage.Package.Manifest.Name
	}
	return persistPreparedResource(ctx, stage, trusted, models.Plugin{ID: util.New(), Name: name, Description: stage.Package.Manifest.Description, SourceKey: entry.SourceKey}, provenance, catalogContentBinding(entry.SourceKey, stage.Package.Manifest.Version, stage.Digest, provenance))
}

// persistPreparedResource 为目录准备和本地技能封装复用不可变内容与资源提交边界。
func persistPreparedResource(ctx context.Context, stage packageplugin.StagedPackage, trusted bool, resource models.Plugin, provenance map[string]any, catalogBinding string) (models.Plugin, string, error) {
	if stage.Package.RequiresTrust() && !trusted {
		return models.Plugin{}, "", status_error.MCPConnectorTrustRequired
	}
	if _, err := packageplugin.ClientExtension(stage.Package); err != nil {
		return models.Plugin{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return models.Plugin{}, "", status_error.OperationCanceled
	}
	commitctx, publish := events.HoldCommitEvents(ctx)
	defer publish()
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
	version := models.PluginVersion{ID: util.New(), PluginID: resource.ID, Version: versionName, ContentPath: filepath.ToSlash(relative), Digest: stage.Digest, Package: stage.Package, Provenance: provenance, CatalogBinding: catalogBinding}
	if catalogBinding != "" {
		version.PreparedUntil = time.Now().Add(constants.PluginPreparationLifetime).UnixMilli()
	}
	if trusted {
		version.TrustedAt = time.Now().UnixMilli()
	}
	preparedVersionID := ""
	err = global.TransactionControllerManager.Transaction(commitctx, func(txctx context.Context) error {
		existing, err := global.PluginControllerManager.FindSource(txctx, resource.SourceKey)
		if err != nil {
			return err
		}
		if existing.ID != "" {
			resource = existing
			version.PluginID = existing.ID
		} else {
			if err := global.PluginControllerManager.Create(txctx, resource); err != nil {
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

// catalogContentBinding 将宿主核验的归档身份和解包摘要绑定，清单中的来源说明不能签发此证明。
func catalogContentBinding(source, version, digest string, provenance map[string]any) string {
	commit, _ := provenance["sourceCommit"].(string)
	archive, _ := provenance["archiveSHA256"].(string)
	address, _ := provenance["archiveURL"].(string)
	entry, _ := provenance["packagePath"].(string)
	fields, _ := json.Marshal([]string{source, version, digest, commit, archive, address, entry})
	sum := sha256.Sum256(fields)
	return hex.EncodeToString(sum[:])
}
