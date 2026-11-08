package plugins

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"path/filepath"
)

// 插件共享账号
type GetPluginAuthorization struct {
	ginx.MethodGet
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回纯缓存的共享账号读取入口。
func (*GetPluginAuthorization) Path() string { return "/:pluginId/account" }

// Output 只读取公开并发事实与已存在的共享身份缓存，不解密材料或创建驱动。
func (o *GetPluginAuthorization) Output(ctx *gin.Context) (interface{}, error) {
	result, _, err := cachedAccount(ctx, o.PluginID, o.WakerID, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
func cachedAccount(ctx *gin.Context, id, waker, versionID string) (PluginAuthorizationResponse, models.PluginVersion, error) {
	var result PluginAuthorizationResponse
	var version models.PluginVersion
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		var err error
		result, version, err = readCachedAccount(txctx, id, waker, versionID)
		return err
	})
	return result, version, err
}

func readCachedAccount(ctx context.Context, id, waker, versionID string) (PluginAuthorizationResponse, models.PluginVersion, error) {
	result := PluginAuthorizationResponse{Provider: constants.PluginAuthCommandStrategy, RequiredScopes: []string{}, MissingScopes: []string{}, ActiveUses: []authorization.Usage{}, Account: authorization.Account{Scopes: []string{}}}
	if _, err := visiblePlugin(ctx, id, waker); err != nil {
		return result, models.PluginVersion{}, err
	}
	version, err := global.PluginControllerManager.Version(ctx, versionID)
	if err != nil {
		return result, version, err
	}
	if version.PluginID != id {
		return result, version, status_error.PluginVersionConflict
	}
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return result, version, err
	}
	revision, err := global.PluginControllerManager.ConfigurationRevision(ctx, id)
	if err != nil {
		return result, version, err
	}
	generation, err := global.PluginControllerManager.AccountGeneration(ctx, id)
	if err != nil {
		return result, version, err
	}
	result.VersionID = version.ID
	result.ConfigurationRevision = revision
	result.AccountGeneration = generation
	if declaration := version.Package.Integration.Authentication; declaration != nil {
		result.RequiredScopes = append(result.RequiredScopes, declaration.Scopes...)
		if global.PluginBridges != nil {
			result.Account, result.Session, result.ActiveUses = global.PluginBridges.CachedAuthorization(id, version.Digest+":"+filepath.ToSlash(version.Package.SourcePath), commandbridge.OperationContext{PluginID: id, VersionID: version.ID, Digest: version.Digest, ConfigurationRevision: revision, AccountGeneration: generation})
		}
		if result.Account.CheckedAt > 0 {
			for _, scope := range result.RequiredScopes {
				if !containsScope(result.Account.Scopes, scope) {
					result.MissingScopes = append(result.MissingScopes, scope)
				}
			}
			if len(result.MissingScopes) > 0 {
				result.Account.Available = false
			}
		}
	}
	return result, version, nil
}
func containsScope(scopes []string, scope string) bool {
	for _, candidate := range scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}
func validateAccountRequest(ctx *gin.Context, id, waker, versionID string, request PluginAccountRequest) (PluginAuthorizationResponse, error) {
	result, _, err := cachedAccount(ctx, id, waker, versionID)
	if err != nil {
		return result, err
	}
	if result.ConfigurationRevision != request.ExpectedConfigurationRevision {
		return result, status_error.PluginConfigurationConflict
	}
	if result.AccountGeneration != request.ExpectedAccountGeneration {
		return result, status_error.PluginAccountConflict
	}
	return result, nil
}
