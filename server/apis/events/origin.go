package events

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
)

const trustedOriginsEnvironment = "ALLY_EVENT_TRUSTED_ORIGINS"

// trustedOrigin 仅追加明确配置的完整Web来源；令牌认证仍由根中间件独立拥有。
func trustedOrigin(request *http.Request) bool {
	if global.LocalWeb != nil && global.LocalWeb.TrustedOrigin(request) {
		return true
	}
	origins := request.Header.Values("Origin")
	if len(origins) > 1 {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" || origin == "ally://app" || origin == "http://127.0.0.1:1420" {
		return true
	}
	allowed, err := configuredWebOrigins(os.Getenv(trustedOriginsEnvironment))
	return err == nil && allowed[origin]
}

// configuredWebOrigins 对整个追加配置先验证；任意无效项都不会授权其他追加来源。
func configuredWebOrigins(raw string) (map[string]bool, error) {
	allowed := map[string]bool{}
	if strings.TrimSpace(raw) == "" {
		return allowed, nil
	}
	var values []string
	if json.Unmarshal([]byte(raw), &values) != nil || values == nil {
		return nil, status_error.CoreConfigurationInvalid
	}
	for _, origin := range values {
		if !validWebOrigin(origin) {
			return nil, status_error.CoreConfigurationInvalid
		}
		allowed[origin] = true
	}
	return allowed, nil
}

// validWebOrigin 只接受浏览器序列化的HTTP(S) origin，不把路径、通配或userinfo视为授权范围。
func validWebOrigin(origin string) bool {
	if origin == "" || strings.TrimSpace(origin) != origin || strings.Contains(origin, "*") {
		return false
	}
	value, err := url.Parse(origin)
	if err != nil || value == nil || value.Scheme != "http" && value.Scheme != "https" || value.Host == "" || value.User != nil || value.Opaque != "" || value.Path != "" || value.RawPath != "" || value.RawQuery != "" || value.ForceQuery || value.Fragment != "" || value.RawFragment != "" || value.String() != origin {
		return false
	}
	host := value.Hostname()
	if !validOriginHost(host) {
		return false
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(value.Host, "[") {
		return false
	}
	if port := value.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return false
		}
	} else if strings.HasSuffix(value.Host, ":") {
		return false
	}
	return true
}

func validOriginHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if host == "" || len(host) > 253 {
		return false
	}
	// DNS名使用ASCII／punycode；IPv6仅由上面的IP解析接受，不接收zone或模糊主机语法。
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character != '-' && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
				return false
			}
		}
	}
	return true
}

// TrustedOrigin 供使用同一认证边界的独立终端流复用精确来源校验。
func TrustedOrigin(request *http.Request) bool { return trustedOrigin(request) }
