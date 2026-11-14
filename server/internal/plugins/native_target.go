package plugins

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// 目标次序与格式仅属于生成器协议，不依赖已安装 CLI。
var targetRuntimes = []string{"codex", "claude", "cursor", "workbuddy", "qoder", "kimi", "gemini", "grok", "opencode", "pi"}

func registeredTarget(runtime string) bool {
	for _, candidate := range targetRuntimes {
		if runtime == candidate {
			return true
		}
	}
	return false
}

func targetEntry(runtime string) (string, string) {
	switch runtime {
	case "codex":
		return "plugin.json", "agent-plugin"
	case "claude":
		return ".claude-plugin/plugin.json", "claude-plugin"
	case "cursor":
		return ".cursor-plugin/plugin.json", "cursor-plugin"
	case "workbuddy":
		return ".codebuddy-plugin/plugin.json", "workbuddy-plugin"
	case "qoder":
		return ".qoder-plugin/plugin.json", "qoder-plugin"
	case "kimi":
		return "kimi.plugin.json", "kimi-plugin"
	case "gemini":
		return "gemini-extension.json", "gemini-extension"
	case "grok":
		return "plugin.json", "grok-plugin"
	case "pi":
		return "package.json", "pi-package"
	default:
		return "opencode.json", "skill-mcp"
	}
}

// PrepareTargets 从同一组件清单按固定顺序生成十种无秘密目标。
func PrepareTargets(ctx context.Context, parent, id string, pkg Package, root string) ([]NativeTarget, error) {
	digest, err := ContentDigest(root)
	if err != nil {
		return nil, err
	}
	targets := make([]NativeTarget, 0, len(targetRuntimes))
	for _, cli := range targetRuntimes {
		target, err := PrepareNativeTargetWithDigest(ctx, parent, id, pkg, root, cli, digest)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// PrepareNativeTarget 根据唯一组件清单生成并验证当前 CLI 的自包含目标。
func PrepareNativeTarget(ctx context.Context, parent, id string, pkg Package, root, cli string, authentication ...AuthenticationBinding) (NativeTarget, error) {
	return prepareNativeTarget(ctx, parent, id, pkg, root, cli, "", authentication...)
}

// PrepareNativeTargetWithDigest 使用调用方已核验的源摘要，避免重复扫描原包。
func PrepareNativeTargetWithDigest(ctx context.Context, parent, id string, pkg Package, root, cli, verifiedDigest string, authentication ...AuthenticationBinding) (NativeTarget, error) {
	return prepareNativeTarget(ctx, parent, id, pkg, root, cli, verifiedDigest, authentication...)
}

// PrepareRuntimeTarget 只为当前连接准备已选 CLI 的目标。调用方在消费者关闭后释放私有目录；
// 可直接交付的标准 Codex 包仍由调用方持有原包租约。
func PrepareRuntimeTarget(ctx context.Context, parent, id string, pkg Package, root, cli, verifiedDigest string) (NativeTarget, func(), error) {
	target, direct, err := describeNativeTarget(ctx, id, pkg, root, cli, verifiedDigest, nil)
	if err != nil {
		return NativeTarget{}, nil, err
	}
	if direct {
		return target, func() {}, nil
	}
	if err := ValidatePreparationBoundary(root, parent); err != nil {
		return NativeTarget{}, nil, err
	}
	container, err := plannedPluginDirectory(filepath.Dir(parent))
	if err != nil {
		return NativeTarget{}, nil, err
	}
	parent = filepath.Join(container, filepath.Base(parent))
	if err := codexPrivateDir(parent); err != nil {
		return NativeTarget{}, nil, err
	}
	stage, err := stageNativeTarget(parent)
	if err != nil {
		return NativeTarget{}, nil, err
	}
	release := sync.OnceFunc(func() {
		if err := os.RemoveAll(stage); err != nil {
			logx.Warnf("remove runtime plugin target failed: %v", err)
		}
	})
	target, err = generateNativeTarget(ctx, target, pkg, root, stage, nil)
	if err != nil {
		release()
		return NativeTarget{}, nil, err
	}
	return target, release, nil
}

func prepareNativeTarget(ctx context.Context, parent, id string, pkg Package, root, cli, verifiedDigest string, authentication ...AuthenticationBinding) (NativeTarget, error) {
	target, direct, err := describeNativeTarget(ctx, id, pkg, root, cli, verifiedDigest, authentication)
	if err != nil || direct {
		return target, err
	}
	if err := ValidatePreparationBoundary(root, parent); err != nil {
		return NativeTarget{}, err
	}
	prepared, unlock, err := lockTargetPreparation(ctx, parent, id, target.SourceDigest, cli, pkg, authentication)
	if err != nil {
		return NativeTarget{}, err
	}
	defer unlock()
	if prepared.root != "" {
		info, err := os.Lstat(prepared.root)
		if err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && VerifyContent(prepared.root, prepared.digest) == nil {
			target.Root, target.Digest = prepared.root, prepared.digest
			return target, nil
		}
	}
	stage, err := stageNativeTarget(parent)
	if err != nil {
		return NativeTarget{}, err
	}
	defer os.RemoveAll(stage)
	target, err = generateNativeTarget(ctx, target, pkg, root, stage, authentication)
	if err != nil {
		return NativeTarget{}, err
	}
	content, err := publishTarget(ctx, parent, stage, id, target.SourceDigest, cli, target.Digest)
	if err != nil {
		return NativeTarget{}, err
	}
	target.Root = content
	prepared.root, prepared.digest = content, target.Digest
	return target, nil
}

func describeNativeTarget(ctx context.Context, id string, pkg Package, root, cli, verifiedDigest string, authentication []AuthenticationBinding) (NativeTarget, bool, error) {
	if err := ctx.Err(); err != nil {
		return NativeTarget{}, false, err
	}
	format, err := TargetFormat(pkg, cli)
	if err != nil {
		return NativeTarget{}, false, err
	}
	digest := verifiedDigest
	if digest == "" {
		digest, err = ContentDigest(root)
		if err != nil {
			return NativeTarget{}, false, err
		}
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return NativeTarget{}, false, status_error.PluginContentInvalid
	}
	if len(digest) != 64 {
		return NativeTarget{}, false, status_error.PluginContentInvalid
	}
	actualRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return NativeTarget{}, false, storageFailure(err)
	}
	entry, _ := targetEntry(cli)
	target := NativeTarget{ID: id, Name: pkg.Manifest.Name, Version: pkg.Manifest.Version, Root: actualRoot, Digest: digest, Runtime: cli, Format: format, SourceDigest: digest, Profile: cli, ManifestPath: entry, Skills: pkg.Skills, Servers: pkg.Servers}
	if target.Version == "" {
		target.Version = digest[:12]
	}
	if target.Format == "projection" {
		target.ManifestPath = ".neuxbot/target.json"
	}
	// 标准 Codex 根已经是完整目标；可信调用方可以直接传递已冻结根。
	if cli == "codex" && len(authentication) == 0 && target.Format != "projection" {
		dependent, err := hasPositionDependentDeliveryLinks(root)
		if err != nil {
			return NativeTarget{}, false, err
		}
		return target, !dependent, ctx.Err()
	}
	return target, false, nil
}

func stageNativeTarget(parent string) (string, error) {
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", storageFailure(err)
	}
	stage, err := os.MkdirTemp(parent, "native-")
	if err != nil {
		return "", storageFailure(err)
	}
	return stage, nil
}

func generateNativeTarget(ctx context.Context, target NativeTarget, pkg Package, root, stage string, authentication []AuthenticationBinding) (NativeTarget, error) {
	if err := ctx.Err(); err != nil {
		return NativeTarget{}, err
	}
	if err := CopyDeliveryTreeWithDigest(root, stage, target.SourceDigest); err != nil {
		return NativeTarget{}, err
	}
	if err := validateNativeEntries(stage, pkg); err != nil {
		return NativeTarget{}, err
	}
	if target.Format == "projection" {
		descriptor := map[string]any{"schemaVersion": 1, "format": "projection", "runtime": target.Runtime, "sourceDigest": target.SourceDigest, "skills": pkg.Skills, "servers": pkg.Servers}
		if err := writeTargetJSON(stage, target.ManifestPath, descriptor); err != nil {
			return NativeTarget{}, err
		}
	} else if err := emitNativeTarget(stage, pkg, target.Runtime, authentication, target.ID); err != nil {
		return NativeTarget{}, err
	}
	derived, err := ContentDigest(stage)
	if err != nil {
		return NativeTarget{}, err
	}
	if err := ctx.Err(); err != nil {
		return NativeTarget{}, err
	}
	target.Root, target.Digest = stage, derived
	return target, nil
}

func nativeServers(pkg Package, cli, id string, bindings []AuthenticationBinding) (map[string]any, error) {
	servers := map[string]any{}
	for _, server := range pkg.Servers {
		data, _ := json.Marshal(server)
		fields := map[string]any{}
		_ = json.Unmarshal(data, &fields)
		delete(fields, "key")
		delete(fields, "authentication")
		if cli != "codex" && fields["type"] == "streamable-http" {
			fields["type"] = "http"
		}
		for _, key := range []string{"command", "cwd"} {
			if value, ok := fields[key].(string); ok {
				fields[key] = targetVariable(value, cli)
			}
		}
		for _, key := range []string{"args"} {
			if values, ok := fields[key].([]any); ok {
				for i, value := range values {
					if text, ok := value.(string); ok {
						values[i] = targetVariable(text, cli)
					}
				}
			}
		}
		for _, key := range []string{"env", "headers"} {
			if values, ok := fields[key].(map[string]any); ok {
				for name, value := range values {
					if text, ok := value.(string); ok {
						values[name] = targetVariable(text, cli)
					}
				}
			}
		}
		for _, binding := range bindings {
			if binding.Server != server.Key {
				continue
			}
			if (binding.Runtime != "" && binding.Runtime != cli) || binding.URL != server.URL || server.Type == "stdio" {
				return nil, status_error.PluginManifestInvalid
			}
			headers, ok := fields["headers"].(map[string]any)
			if !ok {
				headers = map[string]any{}
			}
			for key := range headers {
				if strings.EqualFold(key, binding.Header) {
					delete(headers, key)
				}
			}
			headers[binding.Header] = targetVariable("${"+AuthenticationEnvironmentKey(id, binding)+"}", cli)
			fields["headers"] = headers
		}
		switch cli {
		case "gemini":
			delete(fields, "type")
			if server.Type == "streamable-http" {
				fields["httpUrl"] = fields["url"]
				delete(fields, "url")
			}
		case "kimi":
			delete(fields, "type")
			if server.Type == "sse" {
				fields["transport"] = "sse"
			}
		case "opencode":
			if server.Type == "stdio" {
				command := []any{fields["command"]}
				if arguments, ok := fields["args"].([]any); ok {
					command = append(command, arguments...)
				}
				fields["type"], fields["command"] = "local", command
				if environment, exists := fields["env"]; exists {
					fields["environment"] = environment
				}
				delete(fields, "env")
				delete(fields, "args")
			} else {
				fields["type"] = "remote"
			}
			// V2 默认连接；不产生 V1 的 enabled 字段。
		}
		servers[server.Key] = fields
	}
	for _, binding := range bindings {
		if _, ok := servers[binding.Server]; !ok {
			return nil, status_error.PluginManifestInvalid
		}
	}
	return servers, nil
}

func targetVariable(value, cli string) string {
	switch cli {
	case "claude", "cursor", "workbuddy", "qoder", "grok":
		return strings.NewReplacer("${PLUGIN_ROOT}", "${"+strings.ToUpper(map[string]string{"claude": "CLAUDE", "cursor": "CURSOR", "workbuddy": "CODEBUDDY", "qoder": "QODER", "grok": "GROK"}[cli])+"_PLUGIN_ROOT}", "${PLUGIN_DATA}", "${"+strings.ToUpper(map[string]string{"claude": "CLAUDE", "cursor": "CURSOR", "workbuddy": "CODEBUDDY", "qoder": "QODER", "grok": "GROK"}[cli])+"_PLUGIN_DATA}").Replace(value)
	case "gemini":
		return strings.ReplaceAll(value, "${PLUGIN_ROOT}", "${extensionPath}")
	case "opencode":
		value = strings.ReplaceAll(value, "${PLUGIN_ROOT}", "${NEUXBOT_PLUGIN_ROOT}")
		value = strings.ReplaceAll(value, "${PLUGIN_DATA}", "${NEUXBOT_PLUGIN_DATA}")
		return placeholderPattern.ReplaceAllString(value, "{env:$1}")
	case "kimi":
		return strings.ReplaceAll(value, "${PLUGIN_ROOT}/", "./")
	default:
		return value
	}
}

func emitNativeTarget(stage string, pkg Package, cli string, bindings []AuthenticationBinding, id string) error {
	servers, err := nativeServers(pkg, cli, id, bindings)
	if err != nil {
		return err
	}
	entry, _ := targetEntry(cli)
	manifest := map[string]any{"name": pkg.Manifest.Name}
	for key, value := range map[string]string{"version": pkg.Manifest.Version, "description": pkg.Manifest.Description, "license": pkg.Manifest.License, "homepage": pkg.Manifest.Homepage, "repository": pkg.Manifest.Repository} {
		if value != "" {
			manifest[key] = value
		}
	}
	if pkg.Manifest.Author.Name != "" {
		manifest["author"] = pkg.Manifest.Author
	}
	paths := []string{}
	for _, skill := range pkg.Skills {
		paths = append(paths, "./"+skill.Path)
	}
	if len(paths) > 0 {
		manifest["skills"] = paths
	}
	if len(servers) > 0 {
		manifest["mcpServers"] = servers
	}
	switch cli {
	case "gemini":
		manifest = map[string]any{"name": pkg.Manifest.Name, "version": pkg.Manifest.Version, "description": pkg.Manifest.Description, "mcpServers": servers}
		if settings := targetEnvironmentSettings(servers); len(settings) > 0 {
			manifest["settings"] = settings
		}
	case "codex":
		return writeTargetJSON(stage, "mcp.json", map[string]any{"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json", "mcpServers": servers})
	case "grok":
		if len(pkg.Manifest.Extensions) > 0 {
			manifest["extensions"] = pkg.Manifest.Extensions
		}
		delete(manifest, "mcpServers")
		if len(servers) > 0 {
			manifest["mcpServers"] = "./.mcp.json"
			if err := writeTargetJSON(stage, ".mcp.json", map[string]any{"mcpServers": servers}); err != nil {
				return err
			}
		}
	case "pi":
		declaration := map[string]any{"skills": paths}
		manifest = map[string]any{"name": pkg.Manifest.Name, "version": pkg.Manifest.Version, "pi": declaration}
		if source, err := readObject(filepath.Join(stage, "package.json")); err == nil {
			for _, key := range []string{"dependencies", "optionalDependencies", "peerDependencies", "engines", "type"} {
				if value, exists := source[key]; exists {
					manifest[key] = value
				}
			}
		}
		if len(servers) > 0 {
			declaration["extensions"] = []string{"./extensions/mcp.mjs"}
			encoded, _ := json.Marshal(servers)
			body := `import { fileURLToPath } from "node:url";
const root = fileURLToPath(new URL("../", import.meta.url));
const servers = ` + string(encoded) + `;
function resolve(value) {
  if (typeof value === "string") return value.replace(/\$\{([A-Za-z_][A-Za-z0-9_]*)\}/g, (match, key) => {
    if (key === "PLUGIN_ROOT") return root.replace(/[\\/]$/, "");
    const resolved = process.env[key];
    if (resolved === undefined) throw new Error("Missing MCP environment variable: " + key);
    return resolved;
  });
  if (Array.isArray(value)) return value.map(resolve);
  if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, resolve(item)]));
  return value;
}
export default function register(pi) {
  for (const [name, original] of Object.entries(servers)) {
    const config = resolve(original);
    for (const values of [config.env, config.headers]) {
      for (const value of Object.values(values ?? {})) {
        if (typeof value === "string" && value.trimStart().startsWith("!")) throw new Error("MCP command references are not permitted");
      }
    }
    pi.registerMcpServer(name, config);
  }
}
`
			if err := writeTargetFile(stage, "extensions/mcp.mjs", []byte(body)); err != nil {
				return err
			}
		}
	case "opencode":
		// 纯组件无须制造会被执行的 JS 插件。
		manifest = map[string]any{"skills": map[string]any{"paths": paths}, "mcp": map[string]any{"servers": servers}}
	}
	// 通过严格校验的现有入口原样复用，授权模板需要改写时重新生成。
	if cli != "grok" && cli != "pi" && len(bindings) == 0 {
		if _, err := os.Stat(filepath.Join(stage, entry)); err == nil {
			return nil
		}
	}
	return writeTargetJSON(stage, entry, manifest)
}

func writeTargetJSON(root, path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return status_error.PluginManifestInvalid
	}
	return writeTargetFile(root, path, append(body, '\n'))
}

// TargetFormat 在不读文件或生成产物的前提下选择确定可表达的目标格式。
func TargetFormat(pkg Package, cli string) (string, error) {
	if err := ValidateContract(pkg); err != nil {
		return "", err
	}
	if !registeredTarget(cli) {
		return "", status_error.PluginCapabilityUnsupported
	}
	extension, err := ClientExtension(pkg)
	if err != nil {
		return "", err
	}
	if len(extension.Bindings) > 0 {
		return "projection", nil
	}
	if cli == "grok" && (strings.ContainsAny(pkg.Manifest.Name, ".") || len(pkg.Manifest.Name) > 64) {
		return "projection", nil
	}
	if cli == "opencode" {
		for _, server := range pkg.Servers {
			if server.Type == "sse" {
				return "projection", nil
			}
		}
	}
	if cli == "pi" {
		identities := map[string]bool{}
		for _, server := range pkg.Servers {
			identity := strings.ReplaceAll(server.Key, "-", "_")
			if server.Type == "sse" || identities[identity] {
				return "projection", nil
			}
			identities[identity] = true
			for _, character := range server.Key {
				if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
					return "projection", nil
				}
			}
			for _, values := range []map[string]string{server.Env, server.Headers} {
				for _, value := range values {
					if strings.HasPrefix(strings.TrimSpace(value), "!") {
						return "projection", nil
					}
				}
			}
		}
	}
	_, format := targetEntry(cli)
	return format, nil
}

func targetEnvironmentSettings(servers map[string]any) []map[string]any {
	encoded, _ := json.Marshal(servers)
	known := map[string]bool{}
	for _, match := range placeholderPattern.FindAllStringSubmatch(string(encoded), -1) {
		key := match[1]
		if key != "extensionPath" && key != "workspacePath" {
			known[key] = true
		}
	}
	keys := []string{}
	for key := range known {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	settings := []map[string]any{}
	for _, key := range keys {
		settings = append(settings, map[string]any{"name": key, "description": "MCP environment variable", "envVar": key, "sensitive": true})
	}
	return settings
}
