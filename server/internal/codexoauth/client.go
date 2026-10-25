// Package codexoauth 实现 Codex Device Code 认证协议。
package codexoauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// Client 封装有界的官方认证 HTTP 请求。
type Client struct {
	// HTTP 控制超时、代理和测试替身。
	HTTP *http.Client
	// Origin 是官方认证源，测试时可替换为本地服务器。
	Origin string
}

// Device 保存尚未完成的设备授权状态，设备标识不返回页面。
type Device struct {
	// ID 是服务端设备授权标识。
	ID string `json:"device_auth_id"`
	// UserCode 是用户需要填写的设备码。
	UserCode string `json:"user_code"`
	// Interval 是服务端允许的轮询间隔。
	Interval json.RawMessage `json:"interval"`
	// ExpiresIn 是设备码有效秒数。
	ExpiresIn int64 `json:"expires_in"`
}

// Tokens 保存官方可刷新令牌束，不进入任何对外响应。
type Tokens struct {
	// AccessToken 是短期访问令牌。
	AccessToken string `json:"access_token"`
	// RefreshToken 是官方轮换刷新令牌。
	RefreshToken string `json:"refresh_token"`
	// IDToken 是包含稳定用户身份的官方身份令牌。
	IDToken string `json:"id_token"`
	// ExpiresIn 是本次访问令牌有效秒数。
	ExpiresIn int64 `json:"expires_in"`
}

// New 创建访问官方认证源的客户端。
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, Origin: constants.CodexOAuthOrigin}
}

// Start 通过 usercode 请求开始设备授权。
func (c *Client) Start(ctx context.Context) (Device, error) {
	var device Device
	_, err := c.request(ctx, "/api/accounts/deviceauth/usercode", map[string]string{"client_id": constants.CodexOAuthClientID}, nil, &device)
	if err != nil {
		return Device{}, err
	}
	if device.ID == "" || device.UserCode == "" {
		return Device{}, status_error.AIAccountLoginFailed
	}
	if device.ExpiresIn <= 0 {
		device.ExpiresIn = 900
	}
	return device, nil
}

// Poll 读取设备授权结果，403/404 表示用户尚未确认。
func (c *Client) Poll(ctx context.Context, device Device) (*Tokens, error) {
	var authorization struct {
		// Code 是设备授权完成后的短期兑换码。
		Code string `json:"authorization_code"`
		// Verifier 是由服务端返回的 PKCE 校验值。
		Verifier string `json:"code_verifier"`
	}
	status, err := c.request(ctx, "/api/accounts/deviceauth/token", map[string]string{"device_auth_id": device.ID, "user_code": device.UserCode}, nil, &authorization)
	if status == http.StatusForbidden || status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if authorization.Code == "" || authorization.Verifier == "" {
		return nil, status_error.AIAccountLoginFailed
	}
	var tokens Tokens
	_, err = c.request(ctx, "/oauth/token", nil, url.Values{"grant_type": {"authorization_code"}, "code": {authorization.Code}, "code_verifier": {authorization.Verifier}, "client_id": {constants.CodexOAuthClientID}, "redirect_uri": {constants.CodexOAuthRedirectURI}}, &tokens)
	if err != nil {
		return nil, err
	}
	if !tokens.Valid() {
		return nil, status_error.AIAccountLoginFailed
	}
	return &tokens, nil
}

// Refresh 使用设备授权 scope 刷新令牌，缺失轮换字段时保留旧值。
func (c *Client) Refresh(ctx context.Context, previous Tokens) (Tokens, error) {
	var tokens Tokens
	_, err := c.request(ctx, "/oauth/token", nil, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {previous.RefreshToken}, "client_id": {constants.CodexOAuthClientID}, "scope": {"openid profile email"}}, &tokens)
	if err != nil {
		return Tokens{}, err
	}
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = previous.RefreshToken
	}
	if tokens.IDToken == "" {
		tokens.IDToken = previous.IDToken
	}
	if !tokens.Valid() || tokens.Identity() != previous.Identity() {
		return Tokens{}, status_error.AIAccountLoginFailed
	}
	return tokens, nil
}

func (c *Client) request(ctx context.Context, path string, body any, form url.Values, result any) (int, error) {
	var reader io.Reader
	contentType := "application/json"
	if form != nil {
		reader = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, requestFailure(path, "encode", 0, err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Origin+path, reader)
	if err != nil {
		return 0, requestFailure(path, "create_request", 0, err)
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("User-Agent", "ally-codex-oauth")
	response, err := c.HTTP.Do(request)
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return 0, status_error.OperationCanceled
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, status_error.OperationTimeout
		}
		return 0, requestFailure(path, "transport", 0, err)
	}
	// 已收到成功响应时仍解析令牌，让调用方保存已发生的轮换，避免取消丢弃新凭据。
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// 设备授权的 403/404 是尚未确认，不属于外部故障。
		if path == "/api/accounts/deviceauth/token" && (response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound) {
			return response.StatusCode, nil
		}
		return response.StatusCode, requestFailure(path, "http_status", response.StatusCode, nil)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(result); err != nil {
		return response.StatusCode, requestFailure(path, "decode", response.StatusCode, err)
	}
	return response.StatusCode, nil
}

// requestFailure 只记录请求阶段和技术原因，不记录认证载荷或响应正文。
func requestFailure(path, stage string, status int, err error) error {
	// HTTP 包装错误包含 URL；只保留底层原因，避免重定向地址携带认证信息。
	var urlError *url.Error
	if errors.As(err, &urlError) {
		err = urlError.Err
	}
	logx.Errorf("Codex OAuth request failed: path=%s stage=%s status_code=%d error=%v", path, stage, status, err)
	return status_error.AIAccountLoginFailed
}

// PollInterval 返回带安全余量的设备授权轮询间隔。
func (d Device) PollInterval() time.Duration {
	value := strings.Trim(string(d.Interval), "\"")
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 1 {
		seconds = 5
	}
	return time.Duration(seconds+3) * time.Second
}

// Valid 要求登录返回完整且能够识别账号的令牌束。
func (t Tokens) Valid() bool {
	return t.AccessToken != "" && t.RefreshToken != "" && t.IDToken != "" && t.Identity() != "" && t.AccountID() != ""
}

// Identity 组合稳定用户与上游 workspace，避免同组织不同用户混淆。
func (t Tokens) Identity() string {
	claims := tokenClaims(t.IDToken)
	subject, _ := claims["sub"].(string)
	if subject == "" {
		return ""
	}
	return subject + "/" + t.AccountID()
}

// AccountID 返回官方令牌中声明的 ChatGPT workspace 标识。
func (t Tokens) AccountID() string {
	for _, token := range []string{t.IDToken, t.AccessToken} {
		claims := tokenClaims(token)
		auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
		id, _ := auth["chatgpt_account_id"].(string)
		if id != "" {
			return id
		}
	}
	return ""
}

// ExpiresAt 读取访问令牌实际到期时间，用于提前一分钟刷新。
func (t Tokens) ExpiresAt() time.Time {
	value, _ := tokenClaims(t.AccessToken)["exp"].(float64)
	return time.Unix(int64(value), 0)
}

func tokenClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(data, &claims) != nil {
		return nil
	}
	return claims
}
