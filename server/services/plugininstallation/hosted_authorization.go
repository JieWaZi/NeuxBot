package plugininstallation

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Authorization 选择已固定版本的授权驱动，同一插件使用唯一的 Authorizer。
func (s *Service) Authorization(ctx context.Context, version models.PluginVersion) (*commandbridge.Selection, commandbridge.OperationContext, error) {
	if err := s.ValidateAuthorizationBinding(ctx, version); err != nil {
		return nil, commandbridge.OperationContext{}, err
	}
	target, hosted := version.Package.HostedOAuthServer()
	if !hosted {
		auth, driver, err := s.Bridge(ctx, version)
		if err != nil {
			return nil, commandbridge.OperationContext{}, err
		}
		operation := driver.OperationContext()
		return commandbridge.SelectOperation(auth, operation), operation, nil
	}
	if s.config.HostedOAuth == nil || s.config.Bridges == nil || s.config.Store == nil {
		return nil, commandbridge.OperationContext{}, status_error.PluginNotReady
	}
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return nil, commandbridge.OperationContext{}, err
	}
	if version.TrustedAt == 0 {
		return nil, commandbridge.OperationContext{}, status_error.MCPConnectorTrustRequired
	}
	source, err := packageplugin.ContentPath(s.config.DataDirectory, version.ContentPath)
	if err != nil {
		return nil, commandbridge.OperationContext{}, err
	}
	if err := packageplugin.VerifyContent(source, version.Digest); err != nil {
		return nil, commandbridge.OperationContext{}, err
	}
	revision, err := s.config.Store.ConfigurationRevision(ctx, version.PluginID)
	if err != nil {
		return nil, commandbridge.OperationContext{}, err
	}
	generation, err := s.config.Store.AccountGeneration(ctx, version.PluginID)
	if err != nil {
		return nil, commandbridge.OperationContext{}, err
	}
	operation := commandbridge.OperationContext{
		PluginID: version.PluginID, VersionID: version.ID, Digest: version.Digest,
		ConfigurationRevision: revision, AccountGeneration: generation,
	}
	driver := &hostedOAuthDriver{
		service: s, operation: operation, version: version, resource: target.URL,
	}
	driver.generation.Store(generation)
	auth, err := s.config.Bridges.GetHosted(version.PluginID, version.Digest+":"+filepath.ToSlash(version.Package.SourcePath), driver)
	if err != nil {
		return nil, operation, err
	}
	return commandbridge.SelectOperation(auth, operation), operation, nil
}

// ValidateAuthorizationBinding 防止仍有关联安装或旧授权时改变共享资源边界。
func (s *Service) ValidateAuthorizationBinding(ctx context.Context, target models.PluginVersion) error {
	if s.config.Store == nil {
		return nil
	}
	desired, hosted := target.Package.HostedOAuthServer()
	bound, err := s.config.Store.HostedOAuthBinding(ctx, target.PluginID)
	if err != nil {
		return err
	}
	if bound.Resource != "" && (!hosted || bound.Resource != desired.URL) {
		return status_error.PluginAccountConflict
	}
	if hosted {
		account, err := s.config.Store.Account(ctx, target.PluginID)
		if err != nil {
			return err
		}
		if len(account.DeliveryMaterials) != 0 {
			return status_error.PluginAccountConflict
		}
	}
	changing, err := s.ChangesHostedAuthorization(ctx, target)
	if err != nil {
		return err
	}
	if changing {
		return status_error.PluginAccountConflict
	}
	return nil
}

// ChangesHostedAuthorization 检查所选版本是否与当前任一安装的托管授权边界不同。
func (s *Service) ChangesHostedAuthorization(ctx context.Context, target models.PluginVersion) (bool, error) {
	installed, err := s.config.Store.InstalledVersions(ctx, target.PluginID)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(installed, func(version models.PluginVersion) bool {
		return version.ID != target.ID && version.Package.ChangesHostedOAuthBinding(target.Package)
	}), nil
}

// HostedAccount 读取有关联版本的持久授权信息，不启动 OAuth、访问令牌或探测 MCP。
func (s *Service) HostedAccount(ctx context.Context, version models.PluginVersion) (authorization.Account, error) {
	account := authorization.Account{Scopes: []string{}}
	binding, hosted := version.Package.HostedOAuthServer()
	if !hosted || s.config.HostedOAuth == nil || version.TrustedAt == 0 {
		return account, nil
	}
	account, err := s.config.HostedOAuth.Status(ctx, version.PluginID, binding.URL)
	if err != nil {
		return account, err
	}
	for _, scope := range version.Package.Integration.Authentication.Scopes {
		if !slices.Contains(account.Scopes, scope) {
			account.Available = false
			break
		}
	}
	return account, nil
}

type hostedOAuthDriver struct {
	// service 是当前 Core 的安装授权装配。
	service *Service
	// operation 固定发起管理动作的版本和配置修订。
	operation commandbridge.OperationContext
	// generation 反映当前驱动完成授权后的共享账号代次。
	generation atomic.Int64
	// version 是已核验的授权版本。
	version models.PluginVersion
	// resource 是该版本的唯一 OAuth 资源地址。
	resource string
	// pending 只由 Authorizer 的独占授权工作者读写。
	pending hostedLogin
}

// OperationContext 返回取消及读取会话时须匹配的固定事实。
func (d *hostedOAuthDriver) OperationContext() commandbridge.OperationContext {
	result := d.operation
	result.AccountGeneration = d.generation.Load()
	return result
}

// Status 投影已有 OAuth 授权，工具调用仍负责真实请求鉴权。
func (d *hostedOAuthDriver) Status(ctx context.Context, verify bool) (authorization.Account, error) {
	if verify && d.pending.commit != nil {
		return d.pending.account, nil
	}
	return d.service.HostedAccount(ctx, d.version)
}

// Login 通过共享授权会话发布浏览器地址并取得待提交授权。
func (d *hostedOAuthDriver) Login(ctx context.Context, scopes []string, present func(authorization.Device) error) error {
	operation := d.OperationContext()
	report := func(browserURL string) error {
		expiry := time.Now().Add(10 * time.Minute).UnixMilli()
		return present(authorization.Device{
			Actions:    []authorization.Action{{Type: "open-url", URL: browserURL}},
			DeadlineAt: expiry, ExpiresAt: expiry,
		})
	}
	pending, err := d.service.config.HostedOAuth.prepareLogin(ctx, operation.PluginID, d.resource, operation.AccountGeneration, scopes, report)
	if err != nil {
		return err
	}
	d.pending = pending
	return nil
}

// CommitLogin 在最终核验后绑定版本、端点及修订，并提交授权结果。
func (d *hostedOAuthDriver) CommitLogin(ctx context.Context, _ authorization.Account) error {
	if d.pending.commit == nil {
		return status_error.PluginAuthorizationFailed
	}
	ctx, publish := events.HoldCommitEvents(ctx)
	defer publish()
	if err := d.withCurrentOperation(ctx, d.pending.commit); err != nil {
		var status status_error.StatusError
		code := status_error.PluginAuthorizationFailed.Key()
		if errors.As(err, &status) {
			code = status.Key()
		}
		logx.Warnf("hosted MCP OAuth authorization commit failed: plugin_id=%s version_id=%s error_code=%s", d.operation.PluginID, d.operation.VersionID, code)
		return err
	}
	generation := d.generation.Add(1)
	logx.Infof("hosted MCP OAuth authorization committed: plugin_id=%s version_id=%s account_generation=%d", d.operation.PluginID, d.operation.VersionID, generation)
	d.pending.respond(true)
	return nil
}

// DiscardPendingLogin 在授权工作者结束时移除暂存的授权私有材料。
func (d *hostedOAuthDriver) DiscardPendingLogin() {
	d.pending.respond(false)
	d.pending = hostedLogin{}
}

// Logout 清除本机托管凭据，身份占用与并发的登录退出由共享 Authorizer 管理。
func (d *hostedOAuthDriver) Logout(ctx context.Context) error {
	operation := d.OperationContext()
	return d.withCurrentOperation(ctx, func(txctx context.Context) error {
		return d.service.config.HostedOAuth.Logout(txctx, operation.PluginID, d.resource, operation.AccountGeneration)
	})
}

func (d *hostedOAuthDriver) withCurrentOperation(ctx context.Context, write func(context.Context) error) error {
	if d.service.config.Transactions == nil {
		return status_error.PluginNotReady
	}
	operation := d.OperationContext()
	return d.service.config.Transactions.Transaction(ctx, func(txctx context.Context) error {
		revision, err := d.service.config.Store.ConfigurationRevision(txctx, operation.PluginID)
		if err != nil {
			return err
		}
		if revision != operation.ConfigurationRevision {
			return status_error.PluginConfigurationConflict
		}
		version, err := d.service.config.Store.Version(txctx, operation.VersionID)
		if err != nil {
			return err
		}
		binding, hosted := version.Package.HostedOAuthServer()
		if version.PluginID != operation.PluginID || version.Digest != operation.Digest ||
			version.TrustedAt == 0 || !hosted || binding.URL != d.resource {
			return status_error.PluginVersionConflict
		}
		if err := d.service.ValidateAuthorizationBinding(txctx, version); err != nil {
			return err
		}
		return write(txctx)
	})
}
