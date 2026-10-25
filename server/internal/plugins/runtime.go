package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// Snapshot 保存执行接纳时的插件事实，不包含配置值或凭据。
type Snapshot struct {
	// ID 是稳定资源身份。
	ID string `json:"id"`
	// Name 是接纳时展示名称。
	Name string `json:"name"`
	// VersionID 是固定版本身份。
	VersionID string `json:"versionId"`
	// Version 是固定版本显示值。
	Version string `json:"version"`
	// NativePluginID 是目标原生插件命名空间，便于解释组件来源。
	NativePluginID string `json:"nativePluginId,omitempty"`
	// TargetVersion 是当前CLI实际交付版本。
	TargetVersion string `json:"targetVersion,omitempty"`
	// TargetDigest 是当前CLI实际完整内容摘要。
	TargetDigest string `json:"targetDigest,omitempty"`
	// Delivery 是原生插件或Skill/MCP投射。
	Delivery string `json:"delivery,omitempty"`
	// LoadStage 区分准备与实际会话加载。
	LoadStage string `json:"loadStage,omitempty"`
	// ConfigurationRevision 是本次使用的共享配置修订。
	ConfigurationRevision int64 `json:"configurationRevision"`
	// ProviderAccountID 是本次共享提供者账号身份，不含凭据。
	ProviderAccountID string `json:"providerAccountId,omitempty"`
	// ComponentIDs 是本次交付组件的稳定授权身份。
	ComponentIDs []string `json:"componentIds"`
}

// ResolvedServer 是按当前版本和共享设置解析的一项真实启动配置。
type ResolvedServer struct {
	// Server 是本次执行的完整连接快照。
	Server Server
	// WorkingDirectory 是经过边界校验的启动目录。
	WorkingDirectory string
}

// ComponentID 从资源和组件键形成与版本、名称无关的稳定身份。
func ComponentID(plugin, key string) string {
	sum := sha256.Sum256([]byte(plugin + "\x00" + key))
	return hex.EncodeToString(sum[:16])
}

// ResolveServer 保留标准字段的语义，仅在明确的客户端扩展中注入共享配置。
func ResolveServer(pkg Package, input Server, root, data string, values map[string]string) (ResolvedServer, error) {
	var result ResolvedServer
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return result, storageFailure(err)
	}
	realData, err := filepath.EvalSymlinks(data)
	if err != nil {
		return result, storageFailure(err)
	}
	server := input
	server.Args = append([]string{}, input.Args...)
	server.Env = map[string]string{}
	server.Headers = map[string]string{}
	variables := map[string]string{}
	for key, value := range values {
		variables[key] = value
	}
	variables["PLUGIN_ROOT"] = realRoot
	variables["PLUGIN_DATA"] = realData
	variables["CLAUDE_PLUGIN_ROOT"] = realRoot
	variables["CLAUDE_PLUGIN_DATA"] = realData
	expand := func(value string) (string, error) {
		if pkg.Format == constants.PluginFormatPortable {
			return ExpandPortable(value, realRoot, realData), nil
		}
		return Expand(value, variables)
	}
	if pkg.compatibilityFormat() {
		server.Command, err = Expand(input.Command, variables)
		if err != nil {
			return result, err
		}
	}
	if strings.HasPrefix(server.Command, "./") {
		server.Command, err = contained(realRoot, server.Command)
		if err != nil {
			return result, status_error.PluginContentInvalid
		}
	}
	for index, arg := range server.Args {
		server.Args[index], err = expand(arg)
		if err != nil {
			return result, err
		}
	}
	for key, value := range input.Env {
		server.Env[key], err = expand(value)
		if err != nil {
			return result, err
		}
	}
	for key, value := range input.Headers {
		if pkg.compatibilityFormat() {
			server.Headers[key], err = Expand(value, variables)
			if err != nil {
				return result, err
			}
		} else {
			server.Headers[key] = value
		}
	}
	if pkg.compatibilityFormat() {
		server.URL, err = Expand(input.URL, variables)
		if err != nil {
			return result, err
		}
	}
	extension, err := ClientExtension(pkg)
	if err != nil {
		return result, err
	}
	for _, binding := range extension.Bindings {
		if binding.Server != server.Key {
			continue
		}
		value, err := Expand(binding.Value, variables)
		if err != nil {
			return result, err
		}
		if binding.Target == constants.PluginBindingTargetEnvironment {
			server.Env[binding.Name] = value
		} else {
			for key := range server.Headers {
				if strings.EqualFold(key, binding.Name) {
					delete(server.Headers, key)
				}
			}
			server.Headers[binding.Name] = value
		}
	}
	if runtime.GOOS == "windows" {
		for key := range server.Env {
			if strings.EqualFold(key, "PLUGIN_ROOT") || strings.EqualFold(key, "PLUGIN_DATA") {
				delete(server.Env, key)
			}
		}
	}
	server.Env["PLUGIN_ROOT"] = realRoot
	server.Env["PLUGIN_DATA"] = realData
	working := realRoot
	if input.Cwd != "" {
		cwd, err := expand(input.Cwd)
		if err != nil {
			return result, err
		}
		switch {
		case strings.HasPrefix(input.Cwd, "${PLUGIN_DATA}") || strings.HasPrefix(input.Cwd, "${CLAUDE_PLUGIN_DATA}"):
			relative, err := filepath.Rel(realData, cwd)
			if err != nil {
				return result, status_error.PluginContentInvalid
			}
			working, err = persistentDirectory(realData, relative)
			if err != nil {
				return result, err
			}
		default:
			relative := cwd
			if filepath.IsAbs(cwd) {
				relative, err = filepath.Rel(realRoot, cwd)
				if err != nil {
					return result, status_error.PluginContentInvalid
				}
			}
			working, err = contained(realRoot, relative)
			if err != nil {
				return result, status_error.PluginContentInvalid
			}
		}
		info, err := os.Stat(working)
		if err != nil || !info.IsDir() {
			return result, status_error.PluginContentInvalid
		}
	}
	// 配置绑定可能改变请求头或 Claude 远程地址，交付前复验最终值。
	if server.Type != "stdio" && !validRemote(server.URL, server.Headers) {
		return result, status_error.PluginContentInvalid
	}
	server.Cwd = working
	return ResolvedServer{Server: server, WorkingDirectory: working}, nil
}
func persistentDirectory(root, relative string) (string, error) {
	relative = filepath.Clean(relative)
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", status_error.PluginContentInvalid
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		candidate := filepath.Join(current, part)
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			if err := os.Mkdir(candidate, 0o700); err != nil {
				return "", storageFailure(err)
			}
		} else if err != nil {
			return "", storageFailure(err)
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", status_error.PluginContentInvalid
		}
		inside, err := filepath.Rel(root, resolved)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return "", status_error.PluginContentInvalid
		}
		current = resolved
	}
	return current, nil
}
