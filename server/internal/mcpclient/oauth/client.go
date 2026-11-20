// Package oauth 实现主进程使用的托管 MCP OAuth 公共客户端协议。
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

const (
	protocolTimeout      = 10 * time.Second
	maxTokenBytes        = 64 << 10
	maxAccessTokenLength = 16 << 10
)

// Client 从固定 MCP 资源发现签发方，并将凭据交换限制在该签发方。
type Client struct {
	// resource 是完整的 OAuth 资源标识。
	resource *url.URL
	// http 只允许访问 MCP 资源源，用于获取资源元数据。
	http *http.Client
	// transport 是资源及签发方客户端共享的受限公网传输。
	transport http.RoundTripper
}

// Attempt 保存一次授权的私有状态，随等待会话结束释放。
type Attempt struct {
	// State 是回调分派使用的随机一次性值。
	State string `json:"-"`
	// URL 是用户在外部浏览器中确认授权的地址。
	URL string `json:"-"`
	// owner 固定创建本次 OAuth 请求的客户端。
	owner *Client
	// consumed 防止同时到达的回调尝试重复交换授权码。
	consumed atomic.Bool
	// verifier 将授权码交换绑定到前序浏览器请求。
	verifier string
	// redirect 是动态注册与授权请求共同使用的回调地址。
	redirect string
	// requireIssuer 表示授权服务器声明返回 RFC 9207 签发方标识。
	requireIssuer bool
	// binding 保存认证来源、注册客户端和请求权限。
	binding Credential
}

// Credential 保存可用于重启后续期的凭据，调用方必须将完整记录加密存储。
type Credential struct {
	// Resource 是访问令牌可交付的唯一 MCP 地址。
	Resource string `json:"resource"`
	// Issuer 是颁发令牌的授权服务器标识。
	Issuer string `json:"issuer"`
	// TokenEndpoint 是与签发方绑定的令牌交换地址。
	TokenEndpoint string `json:"tokenEndpoint"`
	// ClientID 是动态注册的公开客户端标识。
	ClientID string `json:"clientId"`
	// AccessToken 是只允许转发至资源地址的访问令牌。
	AccessToken string `json:"accessToken"`
	// RefreshToken 是仅交付原始令牌端点的可选续期令牌。
	RefreshToken string `json:"refreshToken,omitempty"`
	// ExpiresAt 是签发方声明的过期时间，零值表示未声明时间。
	ExpiresAt time.Time `json:"expiresAt"`
	// Scopes 是确认授权的权限。
	Scopes []string `json:"scopes"`
}

// New 为固定的 HTTPS MCP 地址创建 OAuth 客户端，不发出远程请求。
func New(resource string) (*Client, error) {
	if _, err := ParseResource(resource); err != nil {
		return nil, err
	}
	return newClient(resource, NewPublicTransport())
}

func newClient(resource string, transport http.RoundTripper) (*Client, error) {
	parsed, err := endpoint(resource)
	if err != nil {
		return nil, status_error.PluginForbidden
	}
	client := &Client{resource: parsed, transport: transport}
	client.http = client.httpForOrigin(parsed.Host)
	return client, nil
}

func (c *Client) httpForOrigin(host string) *http.Client {
	return &http.Client{
		Transport: originTransport{origin: host, base: c.transport},
		Timeout:   protocolTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (c *Client) verifiedIssuer(raw string) (*url.URL, bool) {
	issuer, err := endpoint(raw)
	return issuer, err == nil && (issuer.Host == c.resource.Host || publicHost(issuer.Hostname()))
}

func belongsToIssuer(raw string, issuer *url.URL) bool {
	target, err := endpoint(raw)
	return err == nil && target.Host == issuer.Host
}

// CloseIdleConnections 释放当前客户端的闲置授权连接。
func (c *Client) CloseIdleConnections() {
	c.http.CloseIdleConnections()
}

// Begin 在用户显式发起授权时发现元数据、注册公开客户端并生成 PKCE 浏览器地址。
func (c *Client) Begin(ctx context.Context, redirect string, requestedScopes []string) (*Attempt, error) {
	if !validCallback(redirect) {
		return nil, status_error.PluginForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, protocolTimeout)
	defer cancel()
	resource := c.resource.String()
	start := time.Now()
	prm, err := c.protectedResourceMetadata(ctx)
	if err != nil {
		return nil, c.preparationFailure(ctx, start, "protected_resource", err)
	}
	var issuer *url.URL
	for _, candidate := range prm.AuthorizationServers {
		if verified, ok := c.verifiedIssuer(candidate); ok {
			issuer = verified
			break
		}
	}
	if issuer == nil {
		return nil, c.preparationFailure(ctx, start, "issuer", nil)
	}
	issuerID := issuer.String()
	issuerHTTP := c.httpForOrigin(issuer.Host)
	start = time.Now()
	meta, err := authorizationServerMetadata(ctx, issuerID, issuerHTTP)
	if err != nil {
		return nil, c.preparationFailure(ctx, start, "authorization_server", err)
	}
	if meta.Issuer != issuerID ||
		!belongsToIssuer(meta.AuthorizationEndpoint, issuer) || !belongsToIssuer(meta.TokenEndpoint, issuer) ||
		!belongsToIssuer(meta.RegistrationEndpoint, issuer) || !slices.Contains(meta.CodeChallengeMethodsSupported, "S256") ||
		!slices.Contains(meta.TokenEndpointAuthMethodsSupported, "none") ||
		(len(meta.GrantTypesSupported) > 0 && !slices.Contains(meta.GrantTypesSupported, "authorization_code")) ||
		(len(meta.ResponseTypesSupported) > 0 && !slices.Contains(meta.ResponseTypesSupported, "code")) {
		return nil, c.preparationFailure(ctx, start, "authorization_server", nil)
	}
	scopes := slices.Clone(requestedScopes)
	if len(scopes) == 0 {
		scopes = slices.Clone(prm.ScopesSupported)
	}
	if slices.Contains(meta.ScopesSupported, "offline_access") && !slices.Contains(scopes, "offline_access") {
		scopes = append(scopes, "offline_access")
	}
	grants := []string{"authorization_code"}
	if len(meta.GrantTypesSupported) == 0 || slices.Contains(meta.GrantTypesSupported, "refresh_token") {
		grants = append(grants, "refresh_token")
	}
	start = time.Now()
	registration, err := oauthex.RegisterClient(ctx, meta.RegistrationEndpoint, &oauthex.ClientRegistrationMetadata{
		ClientName:              "NeuxBot",
		RedirectURIs:            []string{redirect},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              grants,
		ResponseTypes:           []string{"code"},
		ApplicationType:         "native",
		Scope:                   strings.Join(scopes, " "),
	}, issuerHTTP)
	if err != nil || registration == nil || registration.ClientID == "" || registration.ClientSecret != "" ||
		registration.TokenEndpointAuthMethod != "none" || !slices.Equal(registration.RedirectURIs, []string{redirect}) {
		return nil, c.preparationFailure(ctx, start, "client_registration", err)
	}
	attempt := &Attempt{
		State: oauth2.GenerateVerifier(), verifier: oauth2.GenerateVerifier(), owner: c, redirect: redirect,
		requireIssuer: meta.AuthorizationResponseIssParameterSupported,
		binding: Credential{
			Resource: resource, Issuer: issuerID, TokenEndpoint: meta.TokenEndpoint,
			ClientID: registration.ClientID, Scopes: scopes,
		},
	}
	config := oauth2.Config{
		ClientID: registration.ClientID, RedirectURL: redirect, Scopes: scopes,
		Endpoint: oauth2.Endpoint{AuthURL: meta.AuthorizationEndpoint},
	}
	attempt.URL = config.AuthCodeURL(attempt.State, oauth2.S256ChallengeOption(attempt.verifier), oauth2.SetAuthURLParam("resource", resource))
	return attempt, nil
}

// Complete 核对回调与原始授权请求，并只允许交换一次授权码。
func (c *Client) Complete(ctx context.Context, attempt *Attempt, code, state, issuer string) (Credential, error) {
	if attempt == nil || attempt.owner != c || code == "" || len(code) > maxTokenBytes ||
		!attempt.matchesCallback(state, issuer) ||
		!attempt.consumed.CompareAndSwap(false, true) {
		return Credential{}, status_error.PluginAuthorizationFailed
	}
	return c.exchange(ctx, attempt.binding, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {attempt.redirect}, "code_verifier": {attempt.verifier},
	})
}

// Refresh 使用原授权服务器签发新的令牌；调用方按共享身份串行化续期并原子保存结果。
func (c *Client) Refresh(ctx context.Context, saved Credential) (Credential, error) {
	if saved.RefreshToken == "" {
		return Credential{}, status_error.PluginNotReady
	}
	return c.exchange(ctx, saved, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {saved.RefreshToken}})
}

// tokenResponse 是令牌端点返回的公开 OAuth 格式，仅在本进程解码。
type tokenResponse struct {
	// AccessToken 是新签发的访问令牌。
	AccessToken string `json:"access_token"`
	// RefreshToken 是新签发的可选续期令牌。
	RefreshToken string `json:"refresh_token"`
	// TokenType 指明访问令牌的请求头格式。
	TokenType string `json:"token_type"`
	// ExpiresIn 是访问令牌的有效秒数。
	ExpiresIn *int64 `json:"expires_in"`
	// Scope 是实际授予的空格分隔权限。
	Scope string `json:"scope"`
	// Error 是签发方拒绝时可用于判断本地处理的标准错误码。
	Error string `json:"error"`
}

func (c *Client) exchange(ctx context.Context, binding Credential, form url.Values) (Credential, error) {
	issuer, ok := c.verifiedIssuer(binding.Issuer)
	if !ok || binding.Resource != c.resource.String() || !belongsToIssuer(binding.TokenEndpoint, issuer) || binding.ClientID == "" {
		return Credential{}, status_error.PluginForbidden
	}
	start := time.Now()
	grantType := form.Get("grant_type")
	recordFailure := func(stage, cause, providerError string, status int) {
		logx.Warnf("hosted MCP OAuth token exchange failed: host=%s grant=%s stage=%s cause=%s upstream_status=%d provider_error=%s elapsed_ms=%d",
			c.resource.Hostname(), grantType, stage, cause, status, safeProviderError(providerError), time.Since(start).Milliseconds())
	}
	form.Set("client_id", binding.ClientID)
	form.Set("resource", binding.Resource)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, binding.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		recordFailure("token_request", "invalid_request", "", 0)
		return Credential{}, protocolError(ctx, "token_request")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := c.httpForOrigin(issuer.Host).Do(request)
	if err != nil {
		recordFailure("token_request", oauthFailureKind(ctx, err), "", 0)
		return Credential{}, oauthRequestFailure(ctx, err)
	}
	defer response.Body.Close()
	status := response.StatusCode
	if status == http.StatusTooManyRequests || status >= http.StatusInternalServerError {
		recordFailure("token_service", "upstream_unavailable", "", status)
		return Credential{}, status_error.PluginOAuthServiceUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTokenBytes+1))
	if err != nil || len(body) > maxTokenBytes {
		recordFailure("token_response", "invalid_body", "", status)
		return Credential{}, protocolError(ctx, "token_response")
	}
	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		recordFailure("token_response", tokenDecodeFailure(err), "", status)
		return Credential{}, protocolError(ctx, "token_response")
	}
	if status != http.StatusOK {
		recordFailure("token_service", "provider_rejected", token.Error, status)
		if grantType == "refresh_token" && (status == http.StatusBadRequest || status == http.StatusUnauthorized) &&
			(token.Error == "invalid_grant" || token.Error == "invalid_client" || token.Error == "unauthorized_client") {
			return Credential{}, status_error.PluginNotReady
		}
		return Credential{}, status_error.PluginAuthorizationFailed.WithField("stage", "token_service")
	}
	if invalid := token.invalidField(); invalid != "" {
		recordFailure("token_response", invalid, token.Error, status)
		return Credential{}, protocolError(ctx, "token_response")
	}
	binding.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		binding.RefreshToken = token.RefreshToken
	}
	if token.Scope != "" {
		binding.Scopes = strings.Fields(token.Scope)
	}
	binding.ExpiresAt = time.Time{}
	if token.ExpiresIn != nil {
		binding.ExpiresAt = time.Now().Add(time.Duration(*token.ExpiresIn) * time.Second)
	}
	if grantType == "authorization_code" {
		logx.Infof("hosted MCP OAuth token exchange verified: host=%s upstream_status=%d elapsed_ms=%d",
			c.resource.Hostname(), status, time.Since(start).Milliseconds())
	}
	return binding, nil
}

func (r tokenResponse) invalidField() string {
	switch {
	case r.Error != "":
		return "provider_rejected"
	case r.AccessToken == "":
		return "missing_access_token"
	case len(r.AccessToken) > maxAccessTokenLength:
		return "access_token_too_large"
	case !ValidAccessToken(r.AccessToken):
		return "invalid_access_token"
	case !strings.EqualFold(r.TokenType, "Bearer"):
		return "invalid_token_type"
	case r.ExpiresIn != nil && (*r.ExpiresIn < 0 || *r.ExpiresIn > math.MaxInt64/int64(time.Second)):
		return "invalid_expires_in"
	default:
		return ""
	}
}

func tokenDecodeFailure(err error) string {
	var invalid *json.UnmarshalTypeError
	if errors.As(err, &invalid) {
		switch invalid.Field {
		case "access_token", "refresh_token", "token_type", "expires_in", "scope", "error":
			return "invalid_" + invalid.Field
		}
	}
	return "invalid_json"
}

// safeProviderError 只记录协议规定的错误码，屏蔽供应商附加内容。
func safeProviderError(code string) string {
	switch code {
	case "invalid_request", "invalid_grant", "invalid_client", "invalid_target", "invalid_scope",
		"unauthorized_client", "unsupported_grant_type", "server_error", "temporarily_unavailable":
		return code
	case "":
		return "none"
	default:
		return "unknown"
	}
}

// ValidAccessToken 验证供应商的不透明令牌可原样写入单个 Authorization 请求头。
func ValidAccessToken(token string) bool {
	if len(token) == 0 || len(token) > maxAccessTokenLength {
		return false
	}
	for index := range len(token) {
		character := token[index]
		if character < '!' || character > '~' || character == ',' || character == ';' || character == '"' || character == '\\' {
			return false
		}
	}
	return true
}

func validCallback(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil ||
		parsed.Path == "" || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.ForceQuery {
		return false
	}
	address, err := netip.ParseAddrPort(parsed.Host)
	return err == nil && address.Port() != 0 && address.Addr().IsLoopback()
}

func (c *Client) preparationFailure(ctx context.Context, start time.Time, stage string, cause error) error {
	logx.Warnf("hosted MCP OAuth preparation failed: host=%s stage=%s cause=%s elapsed_ms=%d",
		c.resource.Hostname(), stage, oauthFailureKind(ctx, cause), time.Since(start).Milliseconds())
	var known status_error.StatusError
	if errors.As(cause, &known) {
		return known
	}
	return protocolError(ctx, stage)
}

func oauthRequestFailure(ctx context.Context, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var known status_error.StatusError
	if errors.As(cause, &known) {
		return known
	}
	return status_error.PluginOAuthServiceUnavailable
}

// oauthFailureKind 只产生固定类别，避免 OAuth URL、代理响应或上游错误正文进入日志。
func oauthFailureKind(ctx context.Context, cause error) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(cause, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(cause, context.Canceled):
		return "canceled"
	case errors.Is(cause, status_error.PluginForbidden):
		return "destination_rejected"
	case errors.Is(cause, status_error.PluginOAuthMetadataUnavailable):
		return "metadata_unavailable"
	case errors.Is(cause, status_error.PluginOAuthServiceUnavailable):
		return "upstream_unavailable"
	}
	var network net.Error
	if errors.As(cause, &network) {
		if network.Timeout() {
			return "network_timeout"
		}
		return "network_error"
	}
	var registration *oauthex.ClientRegistrationError
	if errors.As(cause, &registration) {
		return "registration_rejected"
	}
	return "invalid_response"
}

func protocolError(ctx context.Context, stage string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return status_error.MCPConnectorProtocolInvalid.WithField("stage", stage)
}
