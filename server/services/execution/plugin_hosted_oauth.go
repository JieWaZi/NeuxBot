package execution

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/mcpclient/oauthrelay"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
)

type hostedPluginOAuth interface {
	// Status 从本地持久化摘要读取当前资源的授权就绪状态。
	Status(ctx context.Context, pluginID, resource string) (authorization.Account, error)
	// AccessToken 只在已授权的本机客户端实际发起 MCP 请求时读取或续期令牌。
	AccessToken(ctx context.Context, pluginID, resource string, generation int64, rejected string) (string, error)
}

func (m *service) acquireHostedMCPRoute(ctx context.Context, pluginID, resource string, generation int64) (oauthrelay.Lease, error) {
	credentials, relay := m.pluginHostedOAuth, m.pluginOAuthRelay
	if credentials == nil || relay == nil {
		return oauthrelay.Lease{}, status_error.PluginNotReady
	}
	account, err := credentials.Status(ctx, pluginID, resource)
	if err != nil {
		return oauthrelay.Lease{}, err
	}
	if !account.Available {
		return oauthrelay.Lease{}, status_error.PluginNotReady
	}
	return relay.CreateLease(oauthrelay.LeaseOptions{
		Resource: resource,
		AccessToken: func(requestCtx context.Context) (string, error) {
			return credentials.AccessToken(requestCtx, pluginID, resource, generation, "")
		},
		RefreshRejected: func(requestCtx context.Context, rejected string) (string, error) {
			return credentials.AccessToken(requestCtx, pluginID, resource, generation, rejected)
		},
	})
}
