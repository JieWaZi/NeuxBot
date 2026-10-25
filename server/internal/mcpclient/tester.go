// Package mcpclient 提供 NeuxBot 主进程使用的 MCP 协议客户端能力。
package mcpclient

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultTimeout = 10 * time.Second
	maximumPages   = 100
)

// Result 是一次 MCP 协议连接测试的结果。
type Result struct {
	// Status 是连接及工具发现成功或失败状态。
	Status constants.MCPTestStatus
	// DurationMS 是测试从开始到结束的耗时毫秒数。
	DurationMS int64
	// Error 是失败时供上层展示的诊断摘要。
	Error string
	// Tools 是服务声明的工具名称和描述。
	Tools []models.MCPToolSummary
}

// headerRoundTripper 为远程 MCP 请求附加连接器配置的请求头。
type headerRoundTripper struct {
	// base 是实际执行 HTTP 请求的底层传输。
	base http.RoundTripper
	// headers 是每个 MCP 请求都要携带的静态请求头。
	headers http.Header
	// endpoint 在插件诊断中限定真实授权地址。
	endpoint string
	// sameOrigin 仅允许 SSE 协议在同源消息端点继续握手，不授予跨源凭据转发。
	sameOrigin bool
}

// RoundTrip 克隆请求并附加连接器请求头，诊断凭据仅交付核准的协议端点。
func (t *headerRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	if t.endpoint != "" && request.URL.String() != t.endpoint {
		expected, err := url.Parse(t.endpoint)
		if !t.sameOrigin || err != nil || request.URL.User != nil || request.URL.Scheme != expected.Scheme || request.URL.Host != expected.Host {
			return nil, status_error.PluginForbidden
		}
	}
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	for name, values := range t.headers {
		cloned.Header.Del(name)
		for _, value := range values {
			cloned.Header.Add(name, value)
		}
	}
	return t.base.RoundTrip(cloned)
}

// Test 初始化 MCP 会话并读取全部工具摘要。
func Test(ctx context.Context, connector models.MCPConnector) Result {
	return testConnection(ctx, connector, "", false)
}

// TestInDirectory 为包内 STDIO 使用已校验的工作目录，仍只初始化与发现工具。
func TestInDirectory(ctx context.Context, connector models.MCPConnector, directory string) Result {
	return testConnection(ctx, connector, directory, true)
}

func testConnection(ctx context.Context, connector models.MCPConnector, directory string, strict bool) Result {
	startedAt := time.Now()
	timeout := time.Duration(connector.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	testContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	transport, err := newTransport(testContext, connector, timeout, transportOptions{directory: directory, strict: strict})
	if err != nil {
		return failedResult(startedAt, err)
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "ally",
		Title:   "NeuxBot",
		Version: "1",
	}, nil)
	session, err := client.Connect(testContext, transport, nil)
	if err != nil {
		return failedResult(startedAt, err)
	}
	defer session.Close()

	tools, err := listTools(testContext, session)
	if err != nil {
		return failedResult(startedAt, err)
	}
	return Result{
		Status:     constants.MCPTestStatusSucceeded,
		DurationMS: time.Since(startedAt).Milliseconds(),
		Tools:      tools,
	}
}

// transportOptions 只描述调用方已经固定的诊断边界。
type transportOptions struct {
	// directory 是核验后的插件启动目录。
	directory string
	// strict 表示禁止重定向，且仅对 SSE 允许同源协议消息端点。
	strict bool
}

func newTransport(
	ctx context.Context,
	connector models.MCPConnector,
	timeout time.Duration,
	options ...transportOptions,
) (mcp.Transport, error) {
	option := transportOptions{}
	if len(options) > 0 {
		option = options[0]
	}
	endpoint := ""
	if option.strict {
		endpoint = connector.URL
	}
	switch connector.Transport {
	case constants.MCPTransportStdio:
		if strings.TrimSpace(connector.Command) == "" {
			return nil, status_error.BadRequest
		}
		environment, err := mergedEnvironment(
			os.Environ(),
			connector.Environment,
		)
		if err != nil {
			return nil, err
		}
		command := exec.CommandContext(ctx, connector.Command, connector.Arguments...)
		command.Env = environment
		command.Dir = option.directory
		command.Stderr = io.Discard
		return &mcp.CommandTransport{
			Command:           command,
			TerminateDuration: min(timeout, time.Second),
		}, nil
	case constants.MCPTransportStreamableHTTP:
		client, err := connectorHTTPClient(connector.Headers, timeout, endpoint, false)
		if err != nil {
			return nil, err
		}
		return &mcp.StreamableClientTransport{
			Endpoint:             connector.URL,
			HTTPClient:           client,
			MaxRetries:           -1,
			DisableStandaloneSSE: true,
		}, nil
	case constants.MCPTransportSSE:
		client, err := connectorHTTPClient(connector.Headers, timeout, endpoint, true)
		if err != nil {
			return nil, err
		}
		return &mcp.SSEClientTransport{
			Endpoint:   connector.URL,
			HTTPClient: client,
		}, nil
	default:
		return nil, status_error.BadRequest
	}
}

func connectorHTTPClient(
	definitions []models.MCPSecretDefinition,
	timeout time.Duration,
	endpoint string,
	sameOrigin bool,
) (*http.Client, error) {
	headers := make(http.Header, len(definitions))
	for _, definition := range definitions {
		if !validHeaderName(definition.Name) {
			return nil, status_error.BadRequest
		}
		if definition.Configured && definition.Value == "" {
			return nil, status_error.MCPConnectorCredentialError
		}
		if definition.Configured {
			headers.Set(definition.Name, definition.Value)
		}
	}
	client := &http.Client{
		Transport: &headerRoundTripper{
			base:       http.DefaultTransport,
			headers:    headers,
			endpoint:   endpoint,
			sameOrigin: sameOrigin,
		},
		Timeout: timeout,
	}
	if endpoint != "" {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return client, nil
}

func mergedEnvironment(
	base []string,
	definitions []models.MCPSecretDefinition,
) ([]string, error) {
	values := make(map[string]string, len(base)+len(definitions))
	order := make([]string, 0, len(base)+len(definitions))
	for _, entry := range base {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, exists := values[name]; !exists {
			order = append(order, name)
		}
		values[name] = value
	}
	for _, definition := range definitions {
		if !validEnvironmentName(definition.Name) {
			return nil, status_error.BadRequest
		}
		if definition.Configured && definition.Value == "" {
			return nil, status_error.MCPConnectorCredentialError
		}
		if !definition.Configured {
			continue
		}
		if _, exists := values[definition.Name]; !exists {
			order = append(order, definition.Name)
		}
		values[definition.Name] = definition.Value
	}
	result := make([]string, 0, len(order))
	for _, name := range order {
		result = append(result, name+"="+values[name])
	}
	return result, nil
}

func listTools(
	ctx context.Context,
	session *mcp.ClientSession,
) ([]models.MCPToolSummary, error) {
	tools := make([]models.MCPToolSummary, 0)
	cursor := ""
	seenCursors := make(map[string]struct{})
	for page := 0; page < maximumPages; page++ {
		result, err := session.ListTools(ctx, &mcp.ListToolsParams{
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		for _, tool := range result.Tools {
			if tool == nil {
				continue
			}
			tools = append(tools, models.MCPToolSummary{
				Name:        tool.Name,
				Description: tool.Description,
			})
		}
		if result.NextCursor == "" {
			return tools, nil
		}
		if _, exists := seenCursors[result.NextCursor]; exists {
			return nil, status_error.MCPConnectorProtocolInvalid
		}
		seenCursors[result.NextCursor] = struct{}{}
		cursor = result.NextCursor
	}
	return nil, status_error.MCPConnectorProtocolInvalid
}

func validEnvironmentName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "=\x00")
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if character > 127 || !isHeaderTokenByte(byte(character)) {
			return false
		}
	}
	return true
}

func isHeaderTokenByte(character byte) bool {
	if character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' {
		return true
	}
	return strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character))
}

func failedResult(startedAt time.Time, err error) Result {
	return Result{
		Status:     constants.MCPTestStatusFailed,
		DurationMS: time.Since(startedAt).Milliseconds(),
		Error:      err.Error(),
	}
}
