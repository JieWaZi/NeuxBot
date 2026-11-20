package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const protectedResourceWellKnown = "/.well-known/oauth-protected-resource"

// protectedResourceMetadata 只在用户发起授权时查找固定 MCP 资源的公开声明。
func (c *Client) protectedResourceMetadata(ctx context.Context) (*oauthex.ProtectedResourceMetadata, error) {
	metadataURL := *c.resource
	metadataURL.Path = protectedResourceWellKnown + metadataURL.Path
	metadataURL.RawPath = ""
	metadata, err := c.readProtectedResourceMetadata(ctx, metadataURL.String())
	if err != nil || metadata != nil {
		return metadata, err
	}
	if metadataURL.Path != protectedResourceWellKnown {
		metadataURL.Path = protectedResourceWellKnown
		metadata, err = c.readProtectedResourceMetadata(ctx, metadataURL.String())
		if err != nil || metadata != nil {
			return metadata, err
		}
	}
	location, err := c.protectedResourceChallenge(ctx)
	if err != nil {
		return nil, err
	}
	metadata, err = c.readProtectedResourceMetadata(ctx, location)
	if err == nil && metadata == nil {
		err = status_error.PluginOAuthMetadataUnavailable
	}
	return metadata, err
}

func (c *Client) readProtectedResourceMetadata(ctx context.Context, location string) (*oauthex.ProtectedResourceMetadata, error) {
	response, err := c.getProtectedResourceDiscovery(ctx, location, "application/json")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNotFound, http.StatusGone:
		return nil, nil
	case http.StatusOK:
	default:
		return nil, discoveryHTTPError(response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, status_error.MCPConnectorProtocolInvalid
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTokenBytes+1))
	if err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			return nil, status_error.MCPConnectorProtocolInvalid
		}
		return nil, oauthRequestFailure(ctx, err)
	}
	var metadata oauthex.ProtectedResourceMetadata
	if len(body) > maxTokenBytes || json.Unmarshal(body, &metadata) != nil ||
		metadata.Resource != c.resource.String() || len(metadata.AuthorizationServers) == 0 {
		return nil, status_error.MCPConnectorProtocolInvalid
	}
	for _, issuer := range metadata.AuthorizationServers {
		if _, err := endpoint(issuer); err != nil {
			return nil, err
		}
	}
	return &metadata, nil
}

// protectedResourceChallenge 只从原资源的未授权响应中接受明确的 Bearer 元数据地址。
func (c *Client) protectedResourceChallenge(ctx context.Context) (string, error) {
	response, err := c.getProtectedResourceDiscovery(ctx, c.resource.String(), "text/event-stream, application/json")
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		return "", discoveryHTTPError(response.StatusCode)
	}
	challenges, err := oauthex.ParseWWWAuthenticate(response.Header.Values("WWW-Authenticate"))
	if err != nil {
		return "", status_error.MCPConnectorProtocolInvalid
	}
	var location string
	for _, challenge := range challenges {
		candidate := challenge.Params["resource_metadata"]
		if challenge.Scheme != "bearer" || candidate == "" {
			continue
		}
		if location != "" && location != candidate {
			return "", status_error.MCPConnectorProtocolInvalid
		}
		location = candidate
	}
	if location == "" {
		return "", status_error.PluginOAuthMetadataUnavailable
	}
	return location, nil
}

// getProtectedResourceDiscovery 在授权服务器可跨源时仍禁止资源发现跨源、携带 Cookie 或跟随重定向。
func (c *Client) getProtectedResourceDiscovery(ctx context.Context, location, accept string) (*http.Response, error) {
	target, err := endpoint(location)
	if err != nil || target.Scheme != "https" || target.Host != c.resource.Host {
		return nil, status_error.PluginForbidden
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	client := *c.http
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, oauthRequestFailure(ctx, err)
	}
	return response, nil
}

func discoveryHTTPError(status int) status_error.StatusError {
	switch {
	case status == http.StatusTooManyRequests || status >= http.StatusInternalServerError:
		return status_error.PluginOAuthServiceUnavailable
	case status == http.StatusForbidden || status == http.StatusUnauthorized || (status >= 300 && status < 400):
		return status_error.PluginForbidden
	default:
		return status_error.PluginOAuthMetadataUnavailable
	}
}

// issuerMetadataTransport 保留发送层的业务错误。上游 SDK 只向调用方返回错误文本。
type issuerMetadataTransport struct {
	// base 限定本次授权允许访问的签发方。
	base http.RoundTripper
	// failure 保留单次串行发现中的网络或 HTTP 失败。
	failure error
}

// RoundTrip 执行签发方元数据请求并在 SDK 转换前留存失败原因。
func (t *issuerMetadataTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil {
		t.failure = oauthRequestFailure(request.Context(), err)
		return nil, t.failure
	}
	switch response.StatusCode {
	case http.StatusOK, http.StatusNotFound, http.StatusGone:
		return response, nil
	default:
		_ = response.Body.Close()
		t.failure = discoveryHTTPError(response.StatusCode)
		return nil, t.failure
	}
}

func authorizationServerMetadata(ctx context.Context, issuer string, client *http.Client) (*oauthex.AuthServerMeta, error) {
	probe := *client
	transport := &issuerMetadataTransport{base: client.Transport}
	probe.Transport = transport
	meta, err := mcpauth.GetAuthServerMetadata(ctx, issuer, &probe)
	if transport.failure != nil {
		return nil, transport.failure
	}
	if err == nil && meta == nil {
		return nil, status_error.PluginOAuthMetadataUnavailable
	}
	return meta, err
}
