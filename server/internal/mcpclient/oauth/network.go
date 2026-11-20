package oauth

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

func endpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		strings.Contains(raw, "#") || parsed.RawQuery != "" || parsed.ForceQuery || parsed.RawPath != "" || strings.TrimSpace(raw) != raw {
		return nil, status_error.PluginForbidden
	}
	return parsed, nil
}

// ParseResource 验证托管 OAuth 使用可绑定的公开 HTTPS MCP 资源地址。
func ParseResource(raw string) (*url.URL, error) {
	parsed, err := endpoint(raw)
	if err != nil || !publicHost(parsed.Hostname()) {
		return nil, status_error.PluginForbidden
	}
	return parsed, nil
}

// originTransport 将请求固定到当前阶段已验证的资源源或签发方源。
type originTransport struct {
	// origin 是该客户端唯一可访问的 HTTPS 主机与端口。
	origin string
	// base 是执行已核验请求的 HTTP 传输。
	base http.RoundTripper
}

// RoundTrip 校验请求源并限制授权协商响应体大小。
func (t originTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	target, err := endpoint(request.URL.String())
	if err != nil || target.Host != t.origin {
		return nil, status_error.PluginForbidden
	}
	start := time.Now()
	response, err := t.base.RoundTrip(request)
	if err != nil {
		logx.Warnf("hosted MCP OAuth transport failed: host=%s method=%s cause=%s elapsed_ms=%d",
			target.Hostname(), request.Method, oauthFailureKind(request.Context(), err), time.Since(start).Milliseconds())
		return nil, err
	}
	if response.ContentLength > maxTokenBytes {
		_ = response.Body.Close()
		return nil, status_error.MCPConnectorProtocolInvalid
	}
	response.Body = http.MaxBytesReader(nil, response.Body, maxTokenBytes)
	return response, nil
}

// CloseIdleConnections 释放底层授权连接池。
func (t originTransport) CloseIdleConnections() {
	if base, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		base.CloseIdleConnections()
	}
}

// NewPublicTransport 钉住已核验的直接公网地址。macOS 仅在基准网段的虚拟 DNS 环境下使用系统回环代理。
func NewPublicTransport() *http.Transport {
	transport := &http.Transport{
		DialContext:            dialPublic,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           20,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    protocolTimeout,
		MaxResponseHeaderBytes: 64 << 10,
	}
	if runtime.GOOS == "darwin" {
		applySystemProxy(transport, net.DefaultResolver.LookupNetIP, readSystemHTTPSProxy)
	}
	return transport
}

// applySystemProxy 仅在命名 HTTPS 目标的所有地址均为虚拟 DNS 基准 IPv4 时交给明确配置的系统代理。
func applySystemProxy(transport *http.Transport, lookup func(context.Context, string, string) ([]netip.Addr, error), settings func(context.Context) string) {
	direct := transport.DialContext
	var once sync.Once
	var proxy atomic.Pointer[url.URL]
	transport.Proxy = func(request *http.Request) (*url.URL, error) {
		host := strings.TrimSuffix(request.URL.Hostname(), ".")
		// 目标请求永远不能直接到达回环代理，也不能以私网字面地址换取 CONNECT。
		if !publicHost(host) {
			return nil, status_error.PluginForbidden
		}
		// 系统解析器还接受十六进制、八进制等旧式 IP 拼写。公网站点须具有以字母开头的顶级域。
		lastDot := strings.LastIndexByte(host, '.')
		if request.URL.Scheme != "https" || lastDot < 0 {
			return nil, nil
		}
		first, _ := utf8.DecodeRuneInString(host[lastDot+1:])
		if !unicode.IsLetter(first) {
			return nil, nil
		}
		ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
		addresses, err := lookup(ctx, "ip", host)
		cancel()
		if err != nil || !onlyBenchmarkIPv4(addresses) {
			return nil, nil
		}
		once.Do(func() {
			address, err := netip.ParseAddrPort(settings(request.Context()))
			if err == nil && address.Addr().IsLoopback() && address.Addr().Zone() == "" && address.Port() != 0 {
				proxy.Store(&url.URL{Scheme: "http", Host: address.String()})
			}
		})
		return proxy.Load(), nil
	}
	dialer := &net.Dialer{Timeout: protocolTimeout}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if selected := proxy.Load(); selected != nil && address == selected.Host {
			return dialer.DialContext(ctx, network, selected.Host)
		}
		return direct(ctx, network, address)
	}
}

func onlyBenchmarkIPv4(addresses []netip.Addr) bool {
	if len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		if !benchmarkIPv4.Contains(address.Unmap()) {
			return false
		}
	}
	return true
}

func readSystemHTTPSProxy(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy")
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return parseSystemHTTPSProxy(string(output))
}

func parseSystemHTTPSProxy(output string) string {
	if len(output) > 64<<10 {
		return ""
	}
	fields := map[string]string{}
	depth := 0
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasSuffix(line, "{"):
			depth++
			continue
		case line == "}":
			depth--
			continue
		}
		if depth != 1 {
			continue
		}
		key, val, _ := strings.Cut(line, " : ")
		switch key {
		case "HTTPSEnable", "HTTPSProxy", "HTTPSPort":
			if _, duplicate := fields[key]; duplicate {
				return ""
			}
			fields[key] = val
		}
	}
	if depth != 0 || fields["HTTPSEnable"] != "1" {
		return ""
	}
	address, err := netip.ParseAddr(fields["HTTPSProxy"])
	if err != nil || !address.IsLoopback() || address.Zone() != "" {
		return ""
	}
	port, err := strconv.ParseUint(fields["HTTPSPort"], 10, 16)
	if err != nil || port == 0 {
		return ""
	}
	return netip.AddrPortFrom(address, uint16(port)).String()
}

// dialPublic 解析后只连接已核验的公网 IP，避免第二次 DNS 查询改变地址。
func dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, status_error.PluginForbidden
	}
	destinations, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(destinations) == 0 {
		return nil, status_error.PluginNotReady
	}
	for _, destination := range destinations {
		if !publicAddress(destination) {
			return nil, status_error.PluginForbidden
		}
	}
	dialer := &net.Dialer{Timeout: protocolTimeout}
	var last error
	for _, destination := range destinations {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(destination.String(), port))
		if err == nil {
			return connection, nil
		}
		last = err
	}
	return nil, last
}

var benchmarkIPv4 = netip.MustParsePrefix("198.18.0.0/15")

var reservedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	benchmarkIPv4,
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var publicIPv6 = netip.MustParsePrefix("2000::/3")

func publicHost(host string) bool {
	if address, err := netip.ParseAddr(host); err == nil {
		return publicAddress(address)
	}
	return !strings.EqualFold(strings.TrimSuffix(host, "."), "localhost")
}

func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || (address.Is6() && !publicIPv6.Contains(address)) {
		return false
	}
	for _, prefix := range reservedNetworks {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
