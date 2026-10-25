package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// prepareNativeAuthentication 只派生无秘密的模板；原生应用等其他认证声明按原文保留。
func prepareNativeAuthentication(parent, id string, target NativeTarget, cli string, bindings []AuthenticationBinding) (NativeTarget, error) {
	if len(bindings) == 0 {
		return target, nil
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return NativeTarget{}, status_error.PluginStorageError
	}
	stage, err := os.MkdirTemp(parent, "auth-native-")
	if err != nil {
		return NativeTarget{}, status_error.PluginStorageError
	}
	defer os.RemoveAll(stage)
	if err = pluginArchive.CopyDirectory(target.Root, stage); err != nil {
		return NativeTarget{}, err
	}
	found := map[int]bool{}
	target.headerTemplates = map[string]string{}
	patchServers := func(servers map[string]json.RawMessage) error {
		for index, binding := range bindings {
			if binding.Runtime != cli {
				return status_error.PluginManifestInvalid
			}
			raw, exists := servers[binding.Server]
			if !exists {
				continue
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil || !stringEquals(fields["url"], binding.URL) {
				return status_error.PluginManifestInvalid
			}
			headers := map[string]string{}
			if raw, exists := fields["headers"]; exists && json.Unmarshal(raw, &headers) != nil {
				return status_error.PluginManifestInvalid
			}
			if headers == nil {
				headers = map[string]string{}
			}
			for key := range headers {
				if strings.EqualFold(key, binding.Header) {
					delete(headers, key)
				}
			}
			if cli == "codex" {
				// Codex 原生 MCP 不解释 Claude 的 headers/${VAR}，通过变量名读取私有进程环境。
				nativeHeaders, environmentHeaders := map[string]string{}, map[string]string{}
				for name, destination := range map[string]map[string]string{"http_headers": nativeHeaders, "env_http_headers": environmentHeaders} {
					if raw, exists := fields[name]; exists && json.Unmarshal(raw, &destination) != nil {
						return status_error.PluginManifestInvalid
					}
					if destination == nil {
						return status_error.PluginManifestInvalid
					}
					for key := range destination {
						if strings.EqualFold(key, binding.Header) {
							delete(destination, key)
						}
					}
				}
				// 转换后的非授权头同样交给 Codex 已知字段，保留原生已有字段的优先级。
				for key, value := range headers {
					found := false
					for existing := range nativeHeaders {
						found = found || strings.EqualFold(existing, key)
					}
					for existing := range environmentHeaders {
						found = found || strings.EqualFold(existing, key)
					}
					if !found {
						if placeholderPattern.MatchString(value) {
							headerBinding := AuthenticationBinding{Server: binding.Server, URL: binding.URL, Header: key}
							environmentKey := AuthenticationEnvironmentKey(id, headerBinding)
							if previous, exists := target.headerTemplates[environmentKey]; exists && previous != value {
								return status_error.PluginManifestInvalid
							}
							target.headerTemplates[environmentKey] = value
							environmentHeaders[key] = environmentKey
						} else {
							nativeHeaders[key] = value
						}
					}
				}
				environmentHeaders[binding.Header] = AuthenticationEnvironmentKey(id, binding)
				// 同一连接可绑定多个头，后续绑定接管先前扫描到的模板。
				delete(target.headerTemplates, AuthenticationEnvironmentKey(id, binding))
				fields["http_headers"], _ = json.Marshal(nativeHeaders)
				fields["env_http_headers"], _ = json.Marshal(environmentHeaders)
				if strings.EqualFold(binding.Header, "Authorization") {
					delete(fields, "bearer_token_env_var")
				}
			} else {
				headers[binding.Header] = "${" + AuthenticationEnvironmentKey(id, binding) + "}"
			}
			fields["headers"], _ = json.Marshal(headers)
			// 该连接已由明确声明的桥接负责认证，不能再启动原生连接级OAuth。
			delete(fields, "oauth")
			servers[binding.Server], _ = json.Marshal(fields)
			found[index] = true
		}
		return nil
	}
	visited := map[string]bool{}
	patchFile := func(relative string, optional bool) error {
		clean := filepath.Clean(relative)
		if visited[clean] {
			return nil
		}
		visited[clean] = true
		path, err := contained(stage, relative)
		if err != nil {
			if optional && os.IsNotExist(err) {
				return nil
			}
			return status_error.PluginContentInvalid
		}
		document, err := readObject(path)
		if err != nil {
			return err
		}
		servers := document
		wrapped, exists := document["mcpServers"]
		if exists {
			servers = nil
			if json.Unmarshal(wrapped, &servers) != nil || servers == nil {
				return status_error.PluginManifestInvalid
			}
		}
		if err := patchServers(servers); err != nil {
			return err
		}
		if exists {
			document["mcpServers"], _ = json.Marshal(servers)
		}
		data, _ := json.Marshal(document)
		return os.WriteFile(path, data, 0600)
	}
	if err := patchFile(".mcp.json", true); err != nil {
		return NativeTarget{}, err
	}
	manifestPath := filepath.Join(stage, "."+cli+"-plugin", "plugin.json")
	manifest, err := readObject(manifestPath)
	if err != nil {
		return NativeTarget{}, err
	}
	var patchDeclaration func(json.RawMessage) (json.RawMessage, error)
	patchDeclaration = func(raw json.RawMessage) (json.RawMessage, error) {
		var path string
		if json.Unmarshal(raw, &path) == nil {
			return raw, patchFile(path, false)
		}
		var servers map[string]json.RawMessage
		if json.Unmarshal(raw, &servers) == nil && servers != nil {
			if err := patchServers(servers); err != nil {
				return nil, err
			}
			body, _ := json.Marshal(servers)
			return body, nil
		}
		var entries []json.RawMessage
		if json.Unmarshal(raw, &entries) == nil && entries != nil {
			for i, entry := range entries {
				if strings.HasPrefix(strings.TrimSpace(string(entry)), "[") {
					return nil, status_error.PluginManifestInvalid
				}
				next, err := patchDeclaration(entry)
				if err != nil {
					return nil, err
				}
				entries[i] = next
			}
			body, _ := json.Marshal(entries)
			return body, nil
		}
		return nil, status_error.PluginManifestInvalid
	}
	if raw, exists := manifest["mcpServers"]; exists {
		patched, err := patchDeclaration(raw)
		if err != nil {
			return NativeTarget{}, err
		}
		manifest["mcpServers"] = patched
	}
	if len(found) != len(bindings) {
		return NativeTarget{}, status_error.PluginManifestInvalid
	}
	body, _ := json.Marshal(manifest)
	if err := os.WriteFile(manifestPath, body, 0600); err != nil {
		return NativeTarget{}, err
	}
	digest, err := pluginArchive.Digest(stage)
	if err != nil {
		return NativeTarget{}, err
	}
	content, err := CopyContent(parent, stage, digest)
	if err != nil {
		return NativeTarget{}, err
	}
	target.Root = content
	target.Digest = digest
	return target, nil
}

// ConfigurationEnvironment 将派生头模板展开到私有进程环境；不写入源包或派生产物。
func (target NativeTarget) ConfigurationEnvironment(values map[string]string) (map[string]string, error) {
	environment := map[string]string{}
	for key, template := range target.headerTemplates {
		value, err := Expand(template, values)
		if err != nil {
			return nil, err
		}
		environment[key] = value
	}
	return environment, nil
}
