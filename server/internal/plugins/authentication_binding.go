package plugins

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// AuthenticationBinding 将桥接秘密限定到一个原生运行目标的远端请求头。
type AuthenticationBinding struct {
	// Runtime 是原生目标标识。
	Runtime string `json:"runtime"`
	// Server 是原生包已有的 MCP 连接键。
	Server string `json:"server"`
	// URL 是允许接收秘密的精确 HTTPS 地址。
	URL string `json:"url"`
	// Header 是允许桥接提供的请求头名称。
	Header string `json:"header"`
}

func validateAuthenticationBindings(integration Integration) error {
	if action := integration.Authentication.PreferredAction; action != "" && action != "show-qr" && action != "open-url" {
		return status_error.PluginManifestInvalid
	}
	seen := map[string]bool{}
	for _, binding := range integration.Authentication.Bindings {
		address, err := url.Parse(binding.URL)
		identity := binding.Runtime + "\x00" + binding.Server + "\x00" + strings.ToLower(binding.Header)
		if (binding.Runtime != "codex" && binding.Runtime != "claude") || binding.Server == "" || err != nil || address.Scheme != "https" || address.Host == "" || address.User != nil || address.Fragment != "" || !validRemote(binding.URL, map[string]string{binding.Header: ""}) || binding.Header == "" || seen[identity] {
			return status_error.PluginManifestInvalid
		}
		seen[identity] = true
	}
	return nil
}

// TargetAuthenticationBindings 返回当前目标的声明；未声明目标不能借用其他目标授权。
func TargetAuthenticationBindings(logical Package, cli string) []AuthenticationBinding {
	result := []AuthenticationBinding{}
	if logical.Integration.Authentication != nil {
		for _, binding := range logical.Integration.Authentication.Bindings {
			if binding.Runtime == cli {
				result = append(result, binding)
			}
		}
	}
	return result
}

func authenticationTarget(logical, target Package, cli string) (Package, error) {
	bindings := TargetAuthenticationBindings(logical, cli)
	if len(bindings) == 0 {
		return target, nil
	}
	// 两个独立桥接不能隐式合并账号、权限和租约。
	if _, referenced := logical.Integration.NativePlugins[cli]; referenced && target.Integration.Authentication != nil {
		return Package{}, status_error.PluginManifestInvalid
	}
	target.Servers = slices.Clone(target.Servers)
	for _, binding := range bindings {
		index := slices.IndexFunc(target.Servers, func(server Server) bool { return server.Key == binding.Server })
		if index < 0 || target.Servers[index].URL != binding.URL || target.Servers[index].Type == "stdio" {
			return Package{}, status_error.PluginManifestInvalid
		}
		server := &target.Servers[index]
		server.Headers = maps.Clone(server.Headers)
		// 只去掉桥接确实接管的头，其他位置的同名占位变量仍须正常配置。
		for key := range server.Headers {
			if strings.EqualFold(key, binding.Header) {
				delete(server.Headers, key)
			}
		}
	}
	return target, nil
}

// AuthenticationEnvironmentKey 按插件、组件和请求头隔离私有进程环境中的秘密。
func AuthenticationEnvironmentKey(id string, binding AuthenticationBinding) string {
	digest := sha256.Sum256([]byte(id + "\x00" + binding.Server + "\x00" + strings.ToLower(binding.Header) + "\x00" + binding.URL))
	return constants.PluginAuthEnvironmentPrefix + fmt.Sprintf("%X", digest[:16])
}
