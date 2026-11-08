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

// SkillAuthenticationBinding 只向明确技能交付声明的账号环境。
type SkillAuthenticationBinding struct {
	// Runtime 是受支持的原生运行目标。
	Runtime string `json:"runtime"`
	// Skill 是已发现技能的稳定组件键。
	Skill string `json:"skill"`
	// EnvironmentKeys 是允许账号驱动提供的环境键。
	EnvironmentKeys []string `json:"environmentKeys"`
	// PrivatePathKeys 是环境允许键中必须指向私有账号目录的子集。
	PrivatePathKeys []string `json:"privatePathKeys,omitempty"`
}

func validateSkillAuthenticationBindings(pkg Package) error {
	if pkg.Integration.Authentication == nil {
		return nil
	}
	known := map[string]bool{}
	for _, skill := range pkg.Skills {
		known[skill.Key] = true
	}
	identities := map[string]bool{}
	for _, binding := range pkg.Integration.Authentication.SkillBindings {
		identity := binding.Runtime + "\x00" + binding.Skill
		if (binding.Runtime != "codex" && binding.Runtime != "claude") || !known[binding.Skill] || identities[identity] {
			return status_error.PluginManifestInvalid
		}
		identities[identity] = true
		paths := map[string]bool{}
		for _, key := range binding.PrivatePathKeys {
			if paths[key] {
				return status_error.PluginManifestInvalid
			}
			paths[key] = true
		}
		keys := map[string]bool{}
		folded := map[string]bool{}
		for _, key := range binding.EnvironmentKeys {
			upper := strings.ToUpper(key)
			if !configurationKeyPattern.MatchString(key) || folded[upper] || forbiddenSkillEnvironment(upper) || (upper == "PATH" && !paths[key]) {
				return status_error.PluginManifestInvalid
			}
			keys[key] = true
			folded[upper] = true
		}
		for key := range paths {
			if !keys[key] {
				return status_error.PluginManifestInvalid
			}
		}
	}
	return nil
}

func forbiddenSkillEnvironment(key string) bool {
	for _, prefix := range []string{"LD_", "DYLD_", "PYTHON", "NEUXALLY_", "NEUXBOT_", "ALLY", "CODEX", "CLAUDE", "NODE_"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return slices.Contains([]string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "PWD", "OLDPWD", "SHELL", "ENV", "BASH_ENV", "ZDOTDIR", "PLUGIN_ROOT", "PLUGIN_DATA", "GOROOT", "GOPATH"}, key)
}

// TargetSkillAuthenticationBindings 返回当前运行目标的显式技能绑定。
func TargetSkillAuthenticationBindings(pkg Package, cli string) []SkillAuthenticationBinding {
	result := []SkillAuthenticationBinding{}
	if pkg.Integration.Authentication != nil {
		for _, binding := range pkg.Integration.Authentication.SkillBindings {
			if binding.Runtime == cli {
				result = append(result, binding)
			}
		}
	}
	return result
}
