package plugins

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"path/filepath"
	"strings"
)

// PrepareNativeTarget 向 Codex 交付标准原包，为 Claude 派生原生清单与无秘密授权模板。
func PrepareNativeTarget(parent, id string, pkg Package, root, cli string, authentication ...AuthenticationBinding) (NativeTarget, error) {
	return prepareNativeTarget(parent, id, pkg, root, cli, "", authentication...)
}

// PrepareNativeTargetWithDigest 使用调用方已完成的内容摘要，避免同一准备流程重复扫描不可变目录。
func PrepareNativeTargetWithDigest(parent, id string, pkg Package, root, cli, verifiedDigest string, authentication ...AuthenticationBinding) (NativeTarget, error) {
	return prepareNativeTarget(parent, id, pkg, root, cli, verifiedDigest, authentication...)
}

func prepareNativeTarget(parent, id string, pkg Package, root, cli, verifiedDigest string, authentication ...AuthenticationBinding) (NativeTarget, error) {
	if err := ValidateContract(pkg); err != nil {
		return NativeTarget{}, err
	}
	digest := verifiedDigest
	var err error
	if digest == "" {
		digest, err = pluginArchive.Digest(root)
	}
	if err != nil {
		return NativeTarget{}, err
	}
	target := NativeTarget{ID: id, Name: pkg.Manifest.Name, Version: pkg.Manifest.Version, Root: root, Digest: digest}
	if cli != "codex" && cli != "claude" {
		return NativeTarget{}, status_error.PluginCapabilityUnsupported
	}
	// Codex 根标准 MCP 不能由兼容 overlay 替换；有秘密绑定时使用明确的 MCP 投射。
	if cli == "codex" && len(authentication) > 0 {
		return NativeTarget{}, status_error.PluginCapabilityUnsupported
	}
	extension, err := ClientExtension(pkg)
	if err != nil {
		return NativeTarget{}, err
	}
	if len(extension.Bindings) > 0 {
		return NativeTarget{}, status_error.PluginCapabilityUnsupported
	}

	if target.Version == "" {
		target.Version = digest[:12]
	}
	if cli == "codex" {
		return prepareNativeAuthentication(parent, id, target, cli, authentication)
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return NativeTarget{}, status_error.PluginStorageError
	}
	stage, err := os.MkdirTemp(parent, "native-")
	if err != nil {
		return NativeTarget{}, status_error.PluginStorageError
	}
	defer os.RemoveAll(stage)
	if err := pluginArchive.CopyDirectory(root, stage); err != nil {
		return NativeTarget{}, err
	}
	manifest := map[string]any{"name": pkg.Manifest.Name}
	if pkg.Manifest.Version != "" {
		manifest["version"] = pkg.Manifest.Version
	}
	if pkg.Manifest.Description != "" {
		manifest["description"] = pkg.Manifest.Description
	}
	if pkg.Manifest.Author.Name != "" {
		manifest["author"] = pkg.Manifest.Author
	}
	if pkg.Manifest.License != "" {
		manifest["license"] = pkg.Manifest.License
	}
	if pkg.Manifest.Homepage != "" {
		manifest["homepage"] = pkg.Manifest.Homepage
	}
	if pkg.Manifest.Repository != "" {
		manifest["repository"] = pkg.Manifest.Repository
	}
	if len(pkg.Manifest.Keywords) > 0 {
		manifest["keywords"] = pkg.Manifest.Keywords
	}

	if len(pkg.Skills) > 0 {
		paths := []string{}
		for _, skill := range pkg.Skills {
			paths = append(paths, "./"+skill.Path)
		}
		manifest["skills"] = paths
	}
	if len(pkg.Servers) > 0 {
		servers := map[string]any{}
		for _, server := range pkg.Servers {
			data, _ := json.Marshal(server)
			data = []byte(strings.NewReplacer("${PLUGIN_ROOT}", "${CLAUDE_PLUGIN_ROOT}", "${PLUGIN_DATA}", "${CLAUDE_PLUGIN_DATA}").Replace(string(data)))
			var fields map[string]any
			_ = json.Unmarshal(data, &fields)
			delete(fields, "key")
			delete(fields, "authentication")
			if fields["type"] == "streamable-http" {
				fields["type"] = "http"
			}
			servers[server.Key] = fields
		}
		manifest["mcpServers"] = servers
	}
	directory := ".claude-plugin"
	if os.MkdirAll(filepath.Join(stage, directory), 0700) != nil {
		return NativeTarget{}, status_error.PluginStorageError
	}
	data, _ := json.Marshal(manifest)
	if os.WriteFile(filepath.Join(stage, directory, "plugin.json"), data, 0600) != nil {
		return NativeTarget{}, status_error.PluginStorageError
	}
	derivedDigest, err := pluginArchive.Digest(stage)
	if err != nil {
		return NativeTarget{}, err
	}
	content, err := CopyContent(parent, stage, derivedDigest)
	if err != nil {
		return NativeTarget{}, err
	}
	target.Root = content
	target.Digest = derivedDigest
	return prepareNativeAuthentication(parent, id, target, cli, authentication)
}
