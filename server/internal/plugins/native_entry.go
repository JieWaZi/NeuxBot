package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// validateNativeEntries 仅接纳与唯一组件清单等价的已登记原生薄清单。
func validateNativeEntries(root string, pkg Package) error {
	ownedMCP := map[string]bool{}
	paths := []string{".claude-plugin/plugin.json", ".cursor-plugin/plugin.json", ".codebuddy-plugin/plugin.json", ".workbuddy-plugin/plugin.json", ".qoder-plugin/plugin.json", ".kimi-plugin/plugin.json", "kimi.plugin.json", "gemini-extension.json", ".grok-plugin/plugin.json"}
	runtimes := map[string]string{".claude-plugin/plugin.json": "claude", ".cursor-plugin/plugin.json": "cursor", ".codebuddy-plugin/plugin.json": "workbuddy", ".workbuddy-plugin/plugin.json": "workbuddy", ".qoder-plugin/plugin.json": "qoder", ".kimi-plugin/plugin.json": "kimi", "kimi.plugin.json": "kimi", "gemini-extension.json": "gemini", ".grok-plugin/plugin.json": "grok"}
	existing := []string{}
	for _, path := range paths {
		full := filepath.Join(root, path)
		if _, err := os.Lstat(full); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return storageFailure(err)
		}
		raw, err := readObject(full)
		if err != nil {
			return err
		}
		cli := runtimes[path]
		existing = append(existing, path)
		if !stringEquals(raw["name"], pkg.Manifest.Name) {
			return status_error.PluginManifestInvalid
		}
		if value, exists := raw["version"]; exists && !stringEquals(value, pkg.Manifest.Version) {
			return status_error.PluginManifestInvalid
		}
		allowed := map[string]bool{"name": true, "version": true, "description": true, "author": true, "license": true, "homepage": true, "repository": true, "keywords": true, "skills": true, "mcpServers": true}
		if cli == "gemini" {
			allowed["settings"] = true
		}
		for field := range raw {
			if !allowed[field] {
				return status_error.PluginManifestInvalid
			}
		}
		if cli != "gemini" && (len(raw["skills"]) > 0 || cli == "kimi") && !matchingNativeSkills(raw["skills"], pkg) {
			return status_error.PluginManifestInvalid
		}
		expected, err := nativeServers(pkg, cli, "", nil)
		if err != nil {
			return err
		}
		actual, err := readNativeServers(root, raw["mcpServers"], cli, ownedMCP)
		if err != nil {
			return err
		}
		if !sameNativeServers(actual, expected, cli) {
			return status_error.PluginManifestInvalid
		}
		if cli == "gemini" && !matchingGeminiSettings(raw["settings"], expected) {
			return status_error.PluginManifestInvalid
		}
	}
	// 先收集全部引用，再检查入口目录，避免跨目录引用依赖遍历顺序。
	for _, path := range existing {
		if !strings.HasPrefix(path, ".") {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, filepath.Dir(path)))
		if err != nil {
			return storageFailure(err)
		}
		for _, entry := range entries {
			if entry.Name() != "plugin.json" && !ownedMCP[filepath.ToSlash(filepath.Join(filepath.Dir(path), entry.Name()))] {
				return status_error.PluginManifestInvalid
			}
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "package.json")); err == nil {
		raw, err := readObject(filepath.Join(root, "package.json"))
		if err != nil {
			return err
		}
		if _, exists := raw["scripts"]; exists {
			return status_error.PluginManifestInvalid
		}
		if value, exists := raw["pi"]; exists {
			var fields map[string]json.RawMessage
			if json.Unmarshal(value, &fields) != nil || fields == nil {
				return status_error.PluginManifestInvalid
			}
			for key := range fields {
				if key != "skills" {
					return status_error.PluginManifestInvalid
				}
			}
			if !matchingNativeSkills(fields["skills"], pkg) {
				return status_error.PluginManifestInvalid
			}
		}
	} else if !os.IsNotExist(err) {
		return storageFailure(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "opencode.json")); err == nil {
		raw, err := readObject(filepath.Join(root, "opencode.json"))
		if err != nil {
			return err
		}
		for field := range raw {
			if field != "$schema" && field != "skills" && field != "mcp" {
				return status_error.PluginManifestInvalid
			}
		}
		expected, err := nativeServers(pkg, "opencode", "", nil)
		if err != nil {
			return err
		}
		actual := map[string]any{}
		if value, exists := raw["mcp"]; exists {
			var fields map[string]json.RawMessage
			if json.Unmarshal(value, &fields) != nil || len(fields) != 1 || fields["servers"] == nil {
				return status_error.PluginManifestInvalid
			}
			if json.Unmarshal(fields["servers"], &actual) != nil || actual == nil {
				return status_error.PluginManifestInvalid
			}
		}
		if !sameNativeServers(actual, expected, "opencode") {
			return status_error.PluginManifestInvalid
		}
		var skills map[string]json.RawMessage
		if value, exists := raw["skills"]; exists {
			if json.Unmarshal(value, &skills) != nil || len(skills) != 1 {
				return status_error.PluginManifestInvalid
			}
		}
		if !matchingNativeSkills(skills["paths"], pkg) {
			return status_error.PluginManifestInvalid
		}
	} else if !os.IsNotExist(err) {
		return storageFailure(err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".mcp.json")); err == nil && !ownedMCP[".mcp.json"] {
		return status_error.PluginManifestInvalid
	} else if err != nil && !os.IsNotExist(err) {
		return storageFailure(err)
	}
	return nil
}

func readNativeServers(root string, value json.RawMessage, cli string, owned map[string]bool) (map[string]any, error) {
	sidecar := ""
	if len(value) == 0 {
		switch cli {
		case "cursor":
			sidecar = "mcp.json"
		case "claude", "workbuddy", "qoder", "grok":
			sidecar = ".mcp.json"
		}
		if sidecar != "" {
			if _, err := os.Lstat(filepath.Join(root, sidecar)); os.IsNotExist(err) {
				sidecar = ""
			} else if err != nil {
				return nil, storageFailure(err)
			}
		}
	} else if json.Unmarshal(value, &sidecar) == nil && cli == "gemini" {
		return nil, status_error.PluginManifestInvalid
	}
	if sidecar != "" {
		if !safeRelative(sidecar) {
			return nil, status_error.PluginManifestInvalid
		}
		resolved, err := contained(root, sidecar)
		if err != nil {
			return nil, status_error.PluginManifestInvalid
		}
		document, err := readObject(resolved)
		if err != nil {
			return nil, err
		}
		value = document["mcpServers"]
		if len(value) == 0 {
			return nil, status_error.PluginManifestInvalid
		}
		owned[filepath.ToSlash(filepath.Clean(sidecar))] = true
	}
	result := map[string]any{}
	if len(value) > 0 {
		if json.Unmarshal(value, &result) != nil || result == nil {
			return nil, status_error.PluginManifestInvalid
		}
	}
	return result, nil
}

func sameNativeServers(actual, expected map[string]any, cli string) bool {
	if cli != "gemini" && cli != "opencode" && cli != "kimi" {
		for _, value := range actual {
			fields, ok := value.(map[string]any)
			if !ok {
				return false
			}
			if fields["type"] == "streamable-http" {
				fields["type"] = "http"
			}
			if _, exists := fields["type"]; !exists {
				if fields["command"] != nil {
					fields["type"] = "stdio"
				} else if fields["url"] != nil {
					fields["type"] = "http"
				}
			}
		}
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(expected)
	return string(a) == string(b)
}

func matchingGeminiSettings(value json.RawMessage, servers map[string]any) bool {
	expected := targetEnvironmentSettings(servers)
	if len(expected) == 0 {
		return len(value) == 0 || string(value) == "[]"
	}
	var actual []map[string]any
	if json.Unmarshal(value, &actual) != nil || len(actual) != len(expected) {
		return false
	}
	keys := map[string]bool{}
	for _, setting := range expected {
		keys[setting["envVar"].(string)] = true
	}
	for _, setting := range actual {
		key, ok := setting["envVar"].(string)
		if !ok || !keys[key] || setting["sensitive"] != true {
			return false
		}
		delete(keys, key)
	}
	return len(keys) == 0
}

// matchingNativeSkills 仅允许入口选择唯一清单内的完整技能集合。
func matchingNativeSkills(value json.RawMessage, pkg Package) bool {
	paths := []string{}
	if len(value) > 0 {
		if json.Unmarshal(value, &paths) != nil {
			var path string
			if json.Unmarshal(value, &path) != nil {
				return false
			}
			paths = []string{path}
		}
	}
	selected := map[string]bool{}
	for _, path := range paths {
		if !safeRelative(path) {
			return false
		}
		path = filepath.ToSlash(filepath.Clean(path))
		found := false
		for _, skill := range pkg.Skills {
			if path == skill.Path || path == "skills" && strings.HasPrefix(skill.Path, "skills/") {
				selected[skill.Key] = true
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return len(selected) == len(pkg.Skills)
}
