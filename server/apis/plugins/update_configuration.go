package plugins

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 保存插件配置
type UpdatePluginConfiguration struct {
	ginx.MethodPut
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
	// 请求体
	Body UpdateConfigurationRequest `in:"body"`
}

// Path 返回只保存共享配置的入口，不执行版本切换。
func (*UpdatePluginConfiguration) Path() string { return "/:pluginId/configuration" }

// Output 在同一短事务中核验实际保留版本和配置修订。
func (o *UpdatePluginConfiguration) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := visiblePlugin(ctx, o.PluginID, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	store := global.PluginControllerManager
	commitctx, publish := events.HoldCommitEvents(ctx)
	defer publish()
	if err := closeIdlePlugin(ctx.Request.Context(), o.PluginID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if global.PluginBridges != nil {
		guard, err := global.PluginBridges.BeginChange(o.PluginID, func() error {
			_, err := store.Get(ctx, o.PluginID)
			return err
		})
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		defer guard.Close()
	}
	err := global.TransactionControllerManager.Transaction(commitctx, func(txctx context.Context) error {
		config, err := store.Configuration(txctx, o.PluginID)
		if err != nil {
			return err
		}
		if config.Revision != o.Body.ExpectedRevision {
			return status_error.PluginConfigurationConflict
		}
		config = configurationChange(config, o.Body)
		relations, err := store.Installations(txctx, o.PluginID, "")
		if err != nil {
			return err
		}
		if len(relations) == 0 {
			version, err := store.LatestVersion(txctx, o.PluginID)
			if err != nil {
				return err
			}
			relations = append(relations, models.WakerPlugin{VersionID: version.ID})
		}
		checked := map[string]bool{}
		secrets := secretValues(config)
		for _, relation := range relations {
			if checked[relation.VersionID] {
				continue
			}
			version, err := store.Version(txctx, relation.VersionID)
			if err != nil {
				return err
			}
			if err := packageplugin.ValidateContract(version.Package); err != nil {
				return err
			}
			if err := packageplugin.ValidateValues(version.Package, config.Values, secrets); err != nil {
				return err
			}
			checked[version.ID] = true
		}
		return store.UpdateConfiguration(txctx, config, o.Body.ExpectedRevision)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
