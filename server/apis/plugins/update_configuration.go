package plugins

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
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
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		config, err := store.Configuration(txctx, o.PluginID)
		if err != nil {
			return err
		}
		if config.Revision != o.Body.ExpectedRevision {
			return status_error.PluginConfigurationConflict
		}
		config = configurationChange(config, o.Body)
		versions, err := store.Versions(txctx, o.PluginID)
		if err != nil {
			return err
		}
		relations, err := store.Installations(txctx, o.PluginID, "")
		if err != nil {
			return err
		}
		used := map[string]bool{}
		for _, r := range relations {
			used[r.VersionID] = true
		}
		if len(relations) == 0 && len(versions) > 0 {
			used[versions[0].ID] = true
		}
		if len(used) == 0 {
			return status_error.PluginNotFound
		}
		for _, v := range versions {
			if !used[v.ID] {
				continue
			}
			if err := packageplugin.ValidateContract(v.Package); err != nil {
				return err
			}
			if err := packageplugin.ValidateValues(v.Package, config.Values, secretValues(config)); err != nil {
				return err
			}
		}
		return store.UpdateConfiguration(txctx, config, o.Body.ExpectedRevision)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
