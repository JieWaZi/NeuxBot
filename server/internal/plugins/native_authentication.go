package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// prepareNativeAuthentication 仅为实际支持的 Claude 派生模板注入环境引用，不写凭据。
func prepareNativeAuthentication(parent, id string, target NativeTarget, cli string, bindings []AuthenticationBinding) (NativeTarget, error) {
	if len(bindings) == 0 {
		return target, nil
	}
	if cli != "claude" {
		return NativeTarget{}, status_error.PluginCapabilityUnsupported
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return NativeTarget{}, status_error.PluginStorageError
	}
	stage, err := os.MkdirTemp(parent, "auth-native-")
	if err != nil {
		return NativeTarget{}, status_error.PluginStorageError
	}
	defer os.RemoveAll(stage)
	if err := pluginArchive.CopyDirectory(target.Root, stage); err != nil {
		return NativeTarget{}, err
	}
	path := filepath.Join(stage, ".claude-plugin/plugin.json")
	manifest, err := readObject(path)
	if err != nil {
		return NativeTarget{}, err
	}
	var servers map[string]json.RawMessage
	if json.Unmarshal(manifest["mcpServers"], &servers) != nil || servers == nil {
		return NativeTarget{}, status_error.PluginManifestInvalid
	}
	for _, binding := range bindings {
		if binding.Runtime != cli {
			return NativeTarget{}, status_error.PluginManifestInvalid
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(servers[binding.Server], &fields) != nil || fields == nil || !stringEquals(fields["url"], binding.URL) {
			return NativeTarget{}, status_error.PluginManifestInvalid
		}
		headers := map[string]string{}
		if value, exists := fields["headers"]; exists {
			if json.Unmarshal(value, &headers) != nil || headers == nil {
				return NativeTarget{}, status_error.PluginManifestInvalid
			}
		}
		for key := range headers {
			if strings.EqualFold(key, binding.Header) {
				delete(headers, key)
			}
		}
		headers[binding.Header] = "${" + AuthenticationEnvironmentKey(id, binding) + "}"
		fields["headers"], _ = json.Marshal(headers)
		servers[binding.Server], _ = json.Marshal(fields)
	}
	manifest["mcpServers"], _ = json.Marshal(servers)
	body, _ := json.Marshal(manifest)
	if os.WriteFile(path, body, 0600) != nil {
		return NativeTarget{}, status_error.PluginStorageError
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
