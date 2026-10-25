package plugins

import (
	"context"
	"encoding/hex"
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
	"strings"
)

// 安装插件
type InstallPlugin struct {
	ginx.MethodPost
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 请求体
	Body InstallPluginRequest `in:"body"`
}

// Path 返回伙伴批量安装入口。
func (*InstallPlugin) Path() string { return "/:pluginId/installations" }

// Output 先验证必要认证，再逐伙伴提交真实安装，不预建占位关系。
func (o *InstallPlugin) Output(ctx *gin.Context) (interface{}, error) {
	store := global.PluginControllerManager
	resource, err := store.Get(ctx, o.PluginID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	version, err := store.Version(ctx, o.Body.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if version.PluginID != resource.ID {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	if err := validateCatalogInstallation(resource, version); err != nil {
		return nil, ginx.WithStack(err)
	}
	config, err := store.Configuration(ctx, resource.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if config.Revision != o.Body.ExpectedConfigurationRevision {
		return nil, ginx.WithStack(status_error.PluginConfigurationConflict)
	}
	release, err := prepareInstallationVersions(ctx, []models.PluginVersion{version}, config)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer release()
	response := InstallPluginResponse{PluginID: o.PluginID, Results: []PartnerInstallResult{}}
	seen := map[string]bool{}
	for _, waker := range o.Body.WakerIDs {
		if seen[waker] {
			continue
		}
		seen[waker] = true
		item := PartnerInstallResult{WakerID: waker}
		if resource.OwnerWakerID != "" && resource.OwnerWakerID != waker {
			item.ErrorCode = status_error.PluginForbidden.Key()
			response.Results = append(response.Results, item)
			continue
		}
		if _, err := global.WakerControllerManager.Get(ctx, waker); err != nil {
			item.ErrorCode = errorKey(err)
			response.Results = append(response.Results, item)
			continue
		}
		err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
			current, err := store.Configuration(txctx, resource.ID)
			if err != nil {
				return err
			}
			if current.Revision != config.Revision {
				return status_error.PluginConfigurationConflict
			}
			return store.Install(txctx, models.WakerPlugin{ID: util.New(), WakerID: waker, PluginID: resource.ID, VersionID: version.ID, Enabled: true})
		})
		item.Installed = err == nil
		if err != nil {
			item.ErrorCode = errorKey(err)
		}
		response.Results = append(response.Results, item)
	}
	return response, nil
}

// validateCatalogInstallation 用配置目录的真实固定归档核验内容，包内来源声明不能证明自有身份。
func validateCatalogInstallation(resource models.Plugin, version models.PluginVersion) error {
	parts := strings.Split(resource.SourceKey, ":")
	key := parts[len(parts)-1]
	if source := catalog.SourceKey(global.PluginCatalogSource(), key); source == "" || source != resource.SourceKey {
		return status_error.PluginForbidden
	}
	for field, length := range map[string]int{"sourceCommit": 40, "archiveSHA256": 64} {
		value, _ := version.Provenance[field].(string)
		if len(value) != length {
			return status_error.PluginForbidden
		}
		if _, err := hex.DecodeString(value); err != nil {
			return status_error.PluginForbidden
		}
	}
	entry, err := catalog.Find(global.PluginCatalogSource(), key)
	if err != nil {
		return err
	}
	location, err := catalog.ResolveEntry(global.PluginCatalogSource(), entry)
	if err != nil {
		return err
	}
	stage, err := packageplugin.StageSource(stagedRoot(), location, entry.PackagePath)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Join(stagedRoot(), stage.Token))
	if stage.Digest != version.Digest {
		return status_error.PluginVersionConflict
	}
	return nil
}
