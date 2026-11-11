package plugins

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 插件配置响应
type PluginConfigurationResponse struct {
	// 所选版本
	VersionID string `json:"versionId"`
	// 当前就绪
	Ready bool `json:"ready"`
	// 阻塞原因
	Blockers []string `json:"blockers"`
	// 配置定义
	Fields []packageplugin.ConfigurationField `json:"fields"`
	// 普通配置
	Values map[string]string `json:"values"`
	// 已配置密钥
	ConfiguredSecrets []string `json:"configuredSecrets"`
	// 配置修订
	ConfigurationRevision int64 `json:"configurationRevision"`
	// 账号代次
	AccountGeneration int64 `json:"accountGeneration"`
}

// 读取插件配置
type GetPluginConfiguration struct {
	ginx.MethodGet
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回用户明确打开设置时的配置读取入口。
func (*GetPluginConfiguration) Path() string { return "/:pluginId/configuration" }

// Output 返回普通值与秘密存在标记，不读取共享账号材料或启动授权。
func (o *GetPluginConfiguration) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := visiblePlugin(ctx, o.PluginID, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	version, err := global.PluginControllerManager.Version(ctx, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if version.PluginID != o.PluginID {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return nil, ginx.WithStack(err)
	}
	extension, err := packageplugin.ClientExtension(version.Package)
	if err != nil {
		return nil, ginx.WithStack(err)
	}

	var config models.PluginConfiguration
	var generation int64
	err = global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		var err error
		config, err = global.PluginControllerManager.Configuration(txctx, o.PluginID)
		if err != nil {
			return err
		}
		generation, err = global.PluginControllerManager.AccountGeneration(txctx, o.PluginID)
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	result := PluginConfigurationResponse{VersionID: version.ID, Fields: extension.Config, Values: config.Values, ConfiguredSecrets: []string{}, ConfigurationRevision: config.Revision, AccountGeneration: generation, Blockers: []string{}}
	_, readinessErr := installationService().Summary(version, config, generation)
	result.Ready = readinessErr == nil
	if readinessErr != nil {
		result.Blockers = append(result.Blockers, errorKey(readinessErr))
	}

	for _, secret := range config.Secrets {
		if secret.Configured {
			result.ConfiguredSecrets = append(result.ConfiguredSecrets, secret.Name)
		}
	}
	return result, nil
}
