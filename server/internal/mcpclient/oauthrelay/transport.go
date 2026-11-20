package oauthrelay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/mcpclient/oauth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const (
	maxRequestBytes  = 4 << 20
	maxResponseBytes = 8 << 20
	tokenTimeout     = 30 * time.Second
	otherTimeout     = 30 * time.Second
	postTimeout      = 5 * time.Minute
	streamWriteLimit = 30 * time.Second
)

var requestHeaders = []string{
	"Accept", "Content-Type", "Last-Event-Id", "Mcp-Session-Id", "Mcp-Protocol-Version", "Mcp-Method", "Mcp-Name",
}
var responseHeaders = []string{
	"Allow", "Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version", "Retry-After",
}

func (r *Relay) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	entry := r.authorize(request)
	if entry == nil {
		reject(writer, http.StatusForbidden)
		return
	}
	switch request.Method {
	case http.MethodGet, http.MethodDelete, http.MethodPost:
	default:
		writer.Header().Set("Allow", "POST, GET, DELETE")
		reject(writer, http.StatusMethodNotAllowed)
		return
	}

	slots := r.otherRequests
	if request.Method == http.MethodGet {
		slots = r.eventRequests
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		reject(writer, http.StatusServiceUnavailable)
		return
	}

	var payload []byte
	if request.Method == http.MethodPost {
		var ok bool
		payload, ok = readRequest(writer, request)
		if !ok {
			return
		}
	} else if request.ContentLength != 0 {
		reject(writer, http.StatusBadRequest)
		return
	}

	// 将本地连接和租约任一取消传播给令牌刷新及上游 HTTP。
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	stop := context.AfterFunc(entry.ctx, cancel)
	defer stop()
	if entry.ctx.Err() != nil {
		reject(writer, http.StatusForbidden)
		return
	}
	tokenCtx, tokenCancel := context.WithTimeout(ctx, tokenTimeout)
	token, err := entry.options.AccessToken(tokenCtx)
	tokenDeadline := tokenCtx.Err()
	tokenCancel()
	if ctx.Err() != nil || entry.ctx.Err() != nil {
		reject(writer, http.StatusForbidden)
		return
	}
	if err != nil || tokenDeadline != nil || !oauth.ValidAccessToken(token) {
		writeAuthorizationRequired(writer, request.Method, payload)
		return
	}

	response, release, err := r.requestUpstream(ctx, request, entry.options.Resource, payload, token)
	if err != nil {
		reject(writer, http.StatusBadGateway)
		return
	}
	var renewed string
	if response.StatusCode == http.StatusUnauthorized {
		shouldRenew := rejectedBearer(response.Header)
		release()
		if shouldRenew {
			renewed = entry.renewRejected(ctx, token)
		}
		if renewed == "" || !canRetryMCPRequest(request, payload) {
			writeAuthorizationRequired(writer, request.Method, payload)
			return
		}
		response, release, err = r.requestUpstream(ctx, request, entry.options.Resource, payload, renewed)
		if err != nil {
			reject(writer, http.StatusBadGateway)
			return
		}
	}
	defer release()
	switch {
	case response.StatusCode >= 300 && response.StatusCode < 400:
		reject(writer, http.StatusBadGateway)
	case response.StatusCode == http.StatusUnauthorized:
		writeAuthorizationRequired(writer, request.Method, payload)
	default:
		writeResponse(writer, response, release, upstreamTokenRedactor(token, renewed))
	}
}

func reject(writer http.ResponseWriter, status int) {
	http.Error(writer, http.StatusText(status), status)
}

func readRequest(writer http.ResponseWriter, request *http.Request) ([]byte, bool) {
	if request.ContentLength > maxRequestBytes {
		reject(writer, http.StatusRequestEntityTooLarge)
		return nil, false
	}
	body := http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	defer body.Close()
	payload, err := io.ReadAll(body)
	if err == nil {
		return payload, true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		reject(writer, http.StatusRequestEntityTooLarge)
	} else {
		reject(writer, http.StatusBadRequest)
	}
	return nil, false
}

func rejectedBearer(headers http.Header) bool {
	challenges, err := oauthex.ParseWWWAuthenticate(headers.Values("WWW-Authenticate"))
	if err != nil {
		return false
	}
	shouldRenew := false
	for _, challenge := range challenges {
		if challenge.Scheme != "bearer" {
			continue
		}
		switch strings.ToLower(challenge.Params["error"]) {
		case "insufficient_scope":
			return false
		case "", "invalid_token":
			shouldRenew = true
		}
	}
	return shouldRenew
}

func (entry *relayLease) renewRejected(ctx context.Context, rejected string) string {
	if entry.options.RefreshRejected == nil || ctx.Err() != nil || entry.ctx.Err() != nil {
		return ""
	}
	refreshCtx, cancel := context.WithTimeout(ctx, tokenTimeout)
	defer cancel()
	token, err := entry.options.RefreshRejected(refreshCtx, rejected)
	if err == nil && refreshCtx.Err() == nil && entry.ctx.Err() == nil && token != rejected && oauth.ValidAccessToken(token) {
		return token
	}
	return ""
}

func (r *Relay) requestUpstream(ctx context.Context, incoming *http.Request, resource string, payload []byte, token string) (*http.Response, func(), error) {
	var requestCtx context.Context
	var cancel context.CancelFunc
	if incoming.Method == http.MethodPost {
		// 工具响应也可能是 SSE，截止时间须覆盖整个 POST 响应。
		requestCtx, cancel = context.WithTimeout(ctx, postTimeout)
	} else {
		requestCtx, cancel = context.WithCancel(ctx)
		timer := time.AfterFunc(otherTimeout, cancel)
		defer timer.Stop()
	}
	request, err := http.NewRequestWithContext(requestCtx, incoming.Method, resource, bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, nil, err
	}
	// RoundTrip 不处理重定向。移除重放函数，避免传输层重发送已上传的请求体。
	request.GetBody = nil
	for _, name := range requestHeaders {
		for _, value := range incoming.Header.Values(name) {
			request.Header.Add(name, value)
		}
	}
	for name, values := range incoming.Header {
		if strings.HasPrefix(strings.ToLower(name), "mcp-param-") {
			request.Header[name] = append([]string(nil), values...)
		}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := r.transport.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		cancel()
		return nil, nil, status_error.MCPConnectorProtocolInvalid
	}
	return response, sync.OnceFunc(func() {
		cancel()
		_ = response.Body.Close()
	}), nil
}

func upstreamTokenRedactor(tokens ...string) *strings.Replacer {
	var replacements []string
	for _, token := range tokens {
		if token == "" {
			continue
		}
		replacements = append(replacements, token, "[REDACTED]")
		encoded, _ := json.Marshal(token)
		if escaped := string(encoded[1 : len(encoded)-1]); escaped != token {
			replacements = append(replacements, escaped, "[REDACTED]")
		}
	}
	return strings.NewReplacer(replacements...)
}

func writeResponse(writer http.ResponseWriter, response *http.Response, release func(), redactor *strings.Replacer) {
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	stream := mediaType == "text/event-stream"
	var payload []byte
	if !stream {
		if response.ContentLength > maxResponseBytes {
			reject(writer, http.StatusBadGateway)
			return
		}
		timer := time.AfterFunc(otherTimeout, release)
		var err error
		payload, err = io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		timer.Stop()
		if err != nil || len(payload) > maxResponseBytes {
			reject(writer, http.StatusBadGateway)
			return
		}
	}
	for _, name := range responseHeaders {
		for _, value := range response.Header.Values(name) {
			if redactor.Replace(value) == value {
				writer.Header().Add(name, value)
			}
		}
	}
	controller := http.NewResponseController(writer)
	_ = controller.SetWriteDeadline(time.Now().Add(streamWriteLimit))
	writer.WriteHeader(response.StatusCode)
	if !stream {
		_, _ = redactor.WriteString(writer, string(payload))
		return
	}
	if controller.Flush() != nil {
		return
	}
	streamResponse(writer, controller, response.Body, redactor)
}

func streamResponse(writer http.ResponseWriter, controller *http.ResponseController, upstream io.Reader, redactor *strings.Replacer) {
	// 以有界完整 SSE 行处理，避免令牌横跨底层读取边界时漏掉，并保留原来的行结束符。
	lines := bufio.NewScanner(upstream)
	lines.Buffer(make([]byte, 16<<10), maxResponseBytes)
	lines.Split(splitEventLine)
	for lines.Scan() {
		_ = controller.SetWriteDeadline(time.Now().Add(streamWriteLimit))
		if _, err := redactor.WriteString(writer, lines.Text()); err != nil || controller.Flush() != nil {
			return
		}
	}
}

func splitEventLine(data []byte, atEOF bool) (int, []byte, error) {
	if end := bytes.IndexAny(data, "\r\n"); end >= 0 {
		return end + 1, data[:end+1], nil
	}
	if atEOF && len(data) != 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
