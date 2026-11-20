package plugininstallation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/mcpclient/oauth"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"golang.org/x/sync/singleflight"
)

const (
	hostedOAuthRefreshWindow  = 30 * time.Second
	hostedOAuthRefreshTimeout = 15 * time.Second
)

// hostedOAuthStore 复用插件共享账号的加密存储与公共代次。
type hostedOAuthStore interface {
	HostedOAuth(context.Context, string) (models.PluginHostedOAuth, error)
	HostedOAuthBinding(context.Context, string) (models.PluginHostedOAuth, error)
	HostedOAuthReadiness(context.Context, string, string) (models.PluginHostedOAuthReadiness, bool, error)
	UpdateHostedOAuth(context.Context, models.PluginHostedOAuth, bool) error
}

type hostedOAuthClient interface {
	Authorize(context.Context, []string, func(string) error) (oauth.Credential, func(bool), error)
	Refresh(context.Context, oauth.Credential) (oauth.Credential, error)
	CloseIdleConnections()
}

// HostedOAuth 拥有托管 MCP 凭据用例，等待会话和活跃身份租约仍由插件 Authorizer 管理。
type HostedOAuth struct {
	// root 限定跨实际请求共用的令牌续期生命周期。
	root context.Context
	// store 读写插件共享账号内的托管凭据。
	store hostedOAuthStore
	// client 按已绑定的地址创建标准 OAuth 客户端。
	client func(string) (hostedOAuthClient, error)
	// renewals 保证同一身份的并发请求只交换一次轮换令牌。
	renewals singleflight.Group
}

// NewHostedOAuth 只装配权限持久化和实际请求续期，不访问外部服务。
func NewHostedOAuth(root context.Context, store hostedOAuthStore) *HostedOAuth {
	return &HostedOAuth{
		root: root, store: store, client: func(resource string) (hostedOAuthClient, error) { return oauth.New(resource) },
	}
}

// hostedLogin 只保存当前 Authorizer 工作者的 OAuth 授权结果和最终写入。
type hostedLogin struct {
	// account 是外部授权响应确认的非秘密账号摘要。
	account authorization.Account
	// commit 只在 Authorizer 保留最终写入后才使用获准的令牌。
	commit func(context.Context) error
	// finish 等待共享身份写入成功或结束，向已核验的浏览器回调反馈结果。
	finish func(bool)
}

func (login *hostedLogin) respond(connected bool) {
	if login.finish != nil {
		finish := login.finish
		login.finish = nil
		finish(connected)
	}
}

func (h *HostedOAuth) prepareLogin(ctx context.Context, pluginID, resource string, generation int64, scopes []string, present func(string) error) (hostedLogin, error) {
	account, err := h.store.HostedOAuthBinding(ctx, pluginID)
	if err != nil {
		return hostedLogin{}, err
	}
	if account.Generation != generation {
		return hostedLogin{}, status_error.PluginAccountConflict
	}
	client, err := h.client(resource)
	if err != nil {
		return hostedLogin{}, err
	}
	defer client.CloseIdleConnections()
	credential, finish, err := client.Authorize(ctx, scopes, present)
	if err != nil {
		return hostedLogin{}, err
	}
	if finish == nil {
		return hostedLogin{}, status_error.MCPConnectorProtocolInvalid
	}
	if credential.Resource != resource || credential.AccessToken == "" {
		finish(false)
		return hostedLogin{}, status_error.MCPConnectorProtocolInvalid
	}
	if err := ctx.Err(); err != nil {
		finish(false)
		return hostedLogin{}, err
	}
	account.Resource = resource
	return hostedLogin{
		account: authorization.Account{Configured: true, Available: validHostedAccess(credential, 0) || credential.RefreshToken != "", Scopes: slices.Clone(credential.Scopes)},
		commit:  func(txctx context.Context) error { return h.save(txctx, account, credential, true) },
		finish:  finish,
	}, nil
}

// Status 只投影持久化证据。服务器仍在工具协议实际请求时确认授权。
func (h *HostedOAuth) Status(ctx context.Context, pluginID, resource string) (authorization.Account, error) {
	account := authorization.Account{Configured: true, Scopes: []string{}}
	readiness, found, err := h.store.HostedOAuthReadiness(ctx, pluginID, resource)
	if err != nil || !found {
		return account, err
	}
	account.Available = readiness.Renewable || readiness.ExpiresAt == 0 || readiness.ExpiresAt > time.Now().UnixMilli()
	account.Scopes = slices.Clone(readiness.Scopes)
	if account.Scopes == nil {
		account.Scopes = []string{}
	}
	return account, nil
}

// Logout 清除与当前确切资源匹配的授权，旧执行租约不能使用以后登录的身份。
func (h *HostedOAuth) Logout(ctx context.Context, pluginID, resource string, generation int64) error {
	account, err := h.store.HostedOAuthBinding(ctx, pluginID)
	if err != nil {
		return err
	}
	if account.Generation != generation {
		return status_error.PluginAccountConflict
	}
	if account.Resource == "" {
		return nil
	}
	if account.Resource != resource {
		return status_error.PluginForbidden
	}
	return h.clear(ctx, account)
}

// AccessToken 只在实际 MCP 请求使用时取得令牌，rejected 用于处理服务器提前撤销的令牌。
func (h *HostedOAuth) AccessToken(ctx context.Context, pluginID, resource string, generation int64, rejected string) (string, error) {
	account, credential, err := h.load(ctx, pluginID, resource)
	if err != nil {
		return "", err
	}
	if account.Generation != generation {
		return "", status_error.PluginAccountConflict
	}
	if !needsHostedRefresh(credential, rejected) {
		return credential.AccessToken, nil
	}

	key := pluginID + ":" + strconv.FormatInt(generation, 10)
	result := h.renewals.DoChan(key, func() (any, error) {
		renew, cancel := context.WithTimeout(h.root, hostedOAuthRefreshTimeout)
		defer cancel()
		return h.refresh(renew, pluginID, resource, generation, rejected)
	})
	select {
	case completed := <-result:
		if completed.Err != nil {
			return "", completed.Err
		}
		token := completed.Val.(string)
		if token == rejected {
			return "", status_error.PluginAuthorizationFailed
		}
		return token, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (h *HostedOAuth) refresh(ctx context.Context, pluginID, resource string, generation int64, rejected string) (string, error) {
	account, credential, err := h.load(ctx, pluginID, resource)
	if err != nil {
		return "", err
	}
	if account.Generation != generation {
		return "", status_error.PluginAccountConflict
	}
	// 其他请求可能已交换并保存旧令牌，加载最新材料后才决定是否访问令牌端点。
	if !needsHostedRefresh(credential, rejected) {
		return credential.AccessToken, nil
	}
	if credential.RefreshToken == "" {
		if rejected == credential.AccessToken {
			if err := h.clear(ctx, account); err != nil {
				return "", err
			}
		}
		return "", status_error.PluginNotReady
	}
	client, err := h.client(resource)
	if err != nil {
		return "", err
	}
	defer client.CloseIdleConnections()
	next, err := client.Refresh(ctx, credential)
	if errors.Is(err, status_error.PluginNotReady) {
		if clearErr := h.clear(ctx, account); clearErr != nil {
			return "", clearErr
		}
		return "", err
	}
	if err != nil {
		if rejected != credential.AccessToken && validHostedAccess(credential, 0) {
			return credential.AccessToken, nil
		}
		return "", err
	}
	if err := h.save(ctx, account, next, false); err != nil {
		return "", err
	}
	return next.AccessToken, nil
}

func (h *HostedOAuth) load(ctx context.Context, pluginID, resource string) (models.PluginHostedOAuth, oauth.Credential, error) {
	account, err := h.store.HostedOAuth(ctx, pluginID)
	if err != nil {
		return account, oauth.Credential{}, err
	}
	if account.Secret == "" || account.Resource != resource {
		return account, oauth.Credential{}, status_error.PluginNotReady
	}
	var credential oauth.Credential
	if json.Unmarshal([]byte(account.Secret), &credential) != nil || credential.Resource != resource || credential.AccessToken == "" {
		logx.Errorf("invalid stored hosted MCP credential: plugin_id=%s", pluginID)
		return account, oauth.Credential{}, status_error.MCPConnectorCredentialError
	}
	credential.Scopes = account.Readiness.Scopes
	if account.Readiness.ExpiresAt != 0 {
		credential.ExpiresAt = time.UnixMilli(account.Readiness.ExpiresAt)
	}
	return account, credential, nil
}

func (h *HostedOAuth) save(ctx context.Context, account models.PluginHostedOAuth, credential oauth.Credential, replaceIdentity bool) error {
	if credential.Resource != account.Resource || credential.AccessToken == "" {
		return status_error.MCPConnectorProtocolInvalid
	}
	account.Readiness = models.PluginHostedOAuthReadiness{Renewable: credential.RefreshToken != "", Scopes: credential.Scopes}
	if !credential.ExpiresAt.IsZero() {
		account.Readiness.ExpiresAt = credential.ExpiresAt.UnixMilli()
	}
	credential.ExpiresAt, credential.Scopes = time.Time{}, nil
	raw, err := json.Marshal(credential)
	if err != nil {
		logx.Errorf("encode hosted MCP credential failed: plugin_id=%s", account.PluginID)
		return status_error.MCPConnectorCredentialError
	}
	account.Secret = string(raw)
	return h.store.UpdateHostedOAuth(ctx, account, replaceIdentity)
}

func (h *HostedOAuth) clear(ctx context.Context, account models.PluginHostedOAuth) error {
	account.Resource, account.Secret = "", ""
	return h.store.UpdateHostedOAuth(ctx, account, true)
}

func needsHostedRefresh(credential oauth.Credential, rejected string) bool {
	return rejected == credential.AccessToken || !validHostedAccess(credential, 0) ||
		(credential.RefreshToken != "" && !validHostedAccess(credential, hostedOAuthRefreshWindow))
}

func validHostedAccess(credential oauth.Credential, margin time.Duration) bool {
	return credential.ExpiresAt.IsZero() || credential.ExpiresAt.After(time.Now().Add(margin))
}
