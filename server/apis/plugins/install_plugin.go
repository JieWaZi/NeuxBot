package plugins

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	if err := validatePluginInstallationSource(ctx.Request.Context(), resource, version, false); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return nil, ginx.WithStack(err)
	}
	account, err := store.Account(ctx, resource.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if account.Generation != o.Body.ExpectedAccountGeneration {
		return nil, ginx.WithStack(status_error.PluginAccountConflict)
	}
	config, err := store.Configuration(ctx, resource.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if config.Revision != o.Body.ExpectedConfigurationRevision {
		return nil, ginx.WithStack(status_error.PluginConfigurationConflict)
	}
	previousAccount := account
	commitctx, publish := events.HoldCommitEvents(ctx)
	defer publish()
	release, err := installationService().PrepareVersion(ctx.Request.Context(), version, config, &account, false)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer release()
	writeAccount := installationAccountChanged(previousAccount, account)
	generation := account.Generation
	response := InstallPluginResponse{PluginID: o.PluginID, Results: []PartnerInstallResult{}}
	seen := map[string]bool{}
	for _, waker := range o.Body.WakerIDs {
		if seen[waker] {
			continue
		}
		seen[waker] = true
		item := PartnerInstallResult{WakerID: waker}
		err := global.TransactionControllerManager.Transaction(commitctx, func(txctx context.Context) error {
			if _, err := global.WakerControllerManager.Get(txctx, waker); err != nil {
				return err
			}
			currentRevision, err := store.ConfigurationRevision(txctx, resource.ID)
			if err != nil {
				return err
			}
			currentGeneration, err := store.AccountGeneration(txctx, resource.ID)
			if err != nil {
				return err
			}
			if currentGeneration != generation {
				return status_error.PluginAccountConflict
			}
			if currentRevision != config.Revision {
				return status_error.PluginConfigurationConflict
			}
			if version.PreparedUntil > 0 {
				current, err := store.Version(txctx, version.ID)
				if err != nil {
					return err
				}
				if current.PreparedUntil <= time.Now().UnixMilli() {
					pins, err := store.Installations(txctx, resource.ID, "")
					if err != nil {
						return err
					}
					if !slices.ContainsFunc(pins, func(pin models.WakerPlugin) bool { return pin.VersionID == version.ID }) {
						return status_error.PluginVersionConflict
					}
				}
			}
			if err := store.Install(txctx, models.WakerPlugin{ID: util.New(), WakerID: waker, PluginID: resource.ID, VersionID: version.ID, Enabled: true}); err != nil {
				return err
			}
			if writeAccount {
				return store.UpdateAccount(txctx, account, generation)
			}
			return nil
		})
		item.Installed = err == nil
		if err != nil {
			item.ErrorCode = errorKey(err)
		} else if writeAccount {
			generation++
			writeAccount = false
			commitInstallationAccount(version, config.Revision, account.Generation, config.Revision, generation)
		}
		response.Results = append(response.Results, item)
	}
	return response, nil
}

// validatePluginInstallationSource 核对准备时保存的本机证据。缺少证明的旧记录需重新核验原目录。
func validatePluginInstallationSource(ctx context.Context, resource models.Plugin, version models.PluginVersion, rollback bool) error {
	if version.Retired || version.PluginID != resource.ID {
		return status_error.PluginVersionConflict
	}
	if strings.HasPrefix(resource.SourceKey, constants.PluginLocalSkillSourcePrefix) {
		return packageplugin.ValidateSkillBundle(resource.SourceKey, resource.ID, version.Package, version.Provenance)
	}
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
	if version.CatalogBinding != "" {
		if version.CatalogBinding != catalogContentBinding(resource.SourceKey, version.Version, version.Digest, version.Provenance) {
			return status_error.PluginVersionConflict
		}
		return nil
	}
	if rollback {
		// 回滚目标由成功的安装切换事务记录，旧数据库的来源摘要仍需完整。
		return nil
	}
	archive, _ := version.Provenance["archiveSHA256"].(string)
	return catalog.WithCurrentArchive(ctx, global.PluginCatalogSource(), key, archive, func(entry catalog.Entry, location string) error {
		if entry.Package.Manifest.Version != version.Version {
			return status_error.PluginVersionConflict
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
	})
}
