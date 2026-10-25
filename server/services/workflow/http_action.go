package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func validateHTTPDestination(raw string, origins []string) (*url.URL, error) {
	target, err := url.Parse(raw)
	if err != nil || target.User != nil || target.Hostname() == "" || (target.Scheme != "https" && target.Scheme != "http") || target.Fragment != "" {
		return nil, status_error.WorkflowResourceInvalid.WithField("field", "url")
	}
	origin := target.Scheme + "://" + strings.ToLower(target.Host)
	allowed := false
	for _, declared := range origins {
		value, parseErr := url.Parse(declared)
		if parseErr == nil && value.User == nil && value.Path == "" && value.RawQuery == "" && value.Fragment == "" && (value.Scheme+"://"+strings.ToLower(value.Host)) == origin {
			allowed = true
		}
	}
	if !allowed {
		return nil, status_error.WorkflowCapabilityUnsupported.WithField("capability", "http_origin")
	}
	if address, err := netip.ParseAddr(target.Hostname()); err == nil && !publicAddress(address) {
		return nil, status_error.WorkflowCapabilityUnsupported.WithField("capability", "http_private_network")
	}
	return target, nil
}
func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96"} {
		if netip.MustParsePrefix(prefix).Contains(address) {
			return false
		}
	}
	return true
}
func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		logx.Errorf("parse action dial destination failed: error=%v", err)
		return nil, status_error.WorkflowResourceInvalid
	}
	destinations, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		logx.Errorf("resolve action destination failed: host=%s error=%v", host, err)
		return nil, status_error.WorkflowEffectFailed
	}
	if len(destinations) == 0 {
		return nil, status_error.WorkflowResourceInvalid
	}
	for _, destination := range destinations {
		if !publicAddress(destination) {
			return nil, status_error.WorkflowCapabilityUnsupported.WithField("capability", "http_private_network")
		}
	}
	var last error
	for _, destination := range destinations {
		connection, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(destination.String(), port))
		if err == nil {
			return connection, nil
		}
		last = err
	}
	logx.Errorf("connect action destination failed: host=%s error=%v", host, last)
	return nil, status_error.WorkflowEffectFailed
}
func (s *Service) executeHTTP(ctx context.Context, run models.WorkflowRun, declaration workflowdsl.ActionDeclaration, options map[string]any) (any, bool, error) {
	return s.executeHTTPWithDial(ctx, run, declaration, options, publicDial)
}

func (s *Service) executeHTTPWithDial(ctx context.Context, run models.WorkflowRun, declaration workflowdsl.ActionDeclaration, options map[string]any, dial func(context.Context, string, string) (net.Conn, error)) (any, bool, error) {
	raw, _ := options["url"].(string)
	target, err := validateHTTPDestination(raw, declaration.AllowedOrigins)
	if err != nil {
		return nil, false, err
	}
	method, _ := options["method"].(string)
	if method == "" {
		method = "GET"
	}
	allowed := false
	for _, entry := range declaration.Methods {
		if entry == method {
			allowed = true
		}
	}
	if !allowed {
		return nil, false, status_error.WorkflowCapabilityUnsupported.WithField("capability", "http_method")
	}
	body := []byte{}
	if value, found := options["body"]; found {
		body, err = json.Marshal(value)
		if err != nil {
			return nil, false, status_error.WorkflowSchemaInvalid
		}
	}
	if len(body) > constants.WorkflowOutputLimitBytes {
		return nil, false, status_error.WorkflowBudgetExceeded
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		logx.Errorf("create HTTP action request failed: error=%v", err)
		return nil, false, status_error.WorkflowEffectFailed
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	secrets := []string{}
	if headers, ok := options["headers"].(map[string]any); ok {
		for key, value := range headers {
			header, ok := value.(string)
			if !ok || secretHeader(key) || strings.EqualFold(key, "Host") || strings.ContainsAny(header, "\r\n") {
				return nil, false, status_error.WorkflowSchemaInvalid.WithField("field", "headers")
			}
			request.Header.Set(key, header)
		}
	}
	if headers, ok := options["secretHeaders"].(map[string]any); ok {
		for key, value := range headers {
			reference, _ := value.(map[string]any)
			ref, _ := reference["secretRef"].(string)
			secret, err := s.resolveSecret(ctx, run.DefinitionID, ref)
			if err != nil {
				return nil, false, err
			}
			if strings.ContainsAny(secret, "\r\n") || strings.EqualFold(key, "Host") {
				return nil, false, status_error.WorkflowSchemaInvalid
			}
			request.Header.Set(key, secret)
			secrets = append(secrets, secret)
		}
	}
	transport := &http.Transport{DialContext: dial, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: time.Duration(constants.WorkflowHTTPMaxTimeoutMillis) * time.Millisecond, MaxResponseHeaderBytes: 32 << 10, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return status_error.WorkflowBudgetExceeded
		}
		if _, err := validateHTTPDestination(next.URL.String(), declaration.AllowedOrigins); err != nil {
			return err
		}
		if next.URL.Host != via[0].URL.Host || next.URL.Scheme != via[0].URL.Scheme {
			for key := range request.Header {
				if secretHeader(key) {
					next.Header.Del(key)
				}
			}
			if len(secrets) > 0 {
				return status_error.WorkflowCapabilityUnsupported.WithField("capability", "secret_cross_origin_redirect")
			}
		}
		return nil
	}}
	started := time.Now()
	// 请求头写入后无法以客户端连接错误证明服务端未执行；trace 回调可能在传输 goroutine 调用。
	var dispatched atomic.Bool
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), &httptrace.ClientTrace{WroteHeaders: func() { dispatched.Store(true) }}))
	uncertain := func() bool { return dispatched.Load() && method != "GET" && method != "HEAD" && method != "OPTIONS" }
	response, err := client.Do(request)
	if err != nil {
		logx.Errorf("HTTP action failed: action_id=%s error_type=%T", declaration.ID, err)
		if response != nil {
			// 重定向被策略拒绝时 HTTP Client 仍提供已收到的响应，不把明确响应当成丢回执。
			return map[string]any{"data": nil, "durationMs": time.Since(started).Milliseconds(), "http": map[string]any{"status": response.StatusCode}}, false, status_error.WorkflowEffectFailed
		}
		if ctx.Err() != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return nil, uncertain(), status_error.WorkflowTimeout
			}
			return nil, uncertain(), status_error.WorkflowCancelled
		}
		return nil, uncertain(), status_error.WorkflowEffectFailed
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, constants.WorkflowOutputLimitBytes+1))
	if err != nil {
		logx.Errorf("read HTTP action body failed: action_id=%s error=%v", declaration.ID, err)
		return nil, uncertain(), status_error.WorkflowEffectFailed
	}
	if len(content) > constants.WorkflowOutputLimitBytes {
		return nil, false, status_error.WorkflowBudgetExceeded
	}
	text := string(content)
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	headers := map[string]string{}
	for key, value := range response.Header {
		if !secretHeader(key) {
			header := strings.Join(value, ", ")
			for _, secret := range secrets {
				if secret != "" {
					header = strings.ReplaceAll(header, secret, "[redacted]")
				}
			}
			headers[key] = header
		}
	}
	data := any(text)
	result := map[string]any{"data": data, "durationMs": time.Since(started).Milliseconds(), "http": map[string]any{"status": response.StatusCode, "headers": headers, "body": text}}
	if strings.Contains(response.Header.Get("Content-Type"), "json") || len(declaration.OutputSchema) > 0 || options["schema"] != nil {
		if err = json.Unmarshal([]byte(text), &data); err != nil {
			logx.Errorf("decode HTTP action JSON failed: action_id=%s error=%v", declaration.ID, err)
			return result, false, status_error.WorkflowSchemaInvalid.WithField("code", "HTTP_RESPONSE_INVALID").WithField("field", "data")
		}
	}
	result["data"] = data
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, false, status_error.WorkflowEffectFailed.WithField("code", "HTTP_STATUS_FAILED")
	}
	return result, false, nil
}
func secretHeader(key string) bool {
	key = strings.ToLower(key)
	return key == "authorization" || key == "proxy-authorization" || key == "cookie" || key == "set-cookie" || strings.Contains(key, "token") || strings.Contains(key, "api-key") || strings.Contains(key, "secret")
}
func connectorSecrets(connector models.MCPConnector) []models.MCPSecretDefinition {
	return append(append([]models.MCPSecretDefinition{}, connector.Environment...), connector.Headers...)
}
