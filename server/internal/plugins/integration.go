package plugins

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// NativeReference 指向同一已核验来源快照中的原生包。
type NativeReference struct {
	// Path 是相对完整来源根的目录。
	Path string `json:"path"`
}

// Authentication 声明包内授权桥接，不复制供应商协议。
type Authentication struct {
	// Strategy 是宿主支持的认证策略。
	Strategy string `json:"strategy"`
	// Protocol 是桥接消息版本。
	Protocol string `json:"protocol"`
	// Command 是已安装包内的固定入口。
	Command string `json:"command,omitempty"`
	// Args 是独立传递的入口参数。
	Args []string `json:"args,omitempty"`
	// PreferredAction 是首次等待时的交互类型，不替代实际授权消息。
	PreferredAction string `json:"preferredAction,omitempty"`
	// Bindings 将授权秘密限定到明确的原生目标和远端连接。
	Bindings []AuthenticationBinding `json:"bindings,omitempty"`
	// Scopes 是插件明确需要的权限。
	Scopes []string `json:"scopes,omitempty"`
}

// Integration 只保存原生引用和授权扩展，不重复包身份和组件。
type Integration struct {
	// NativePlugins 按目标选择完整原生实现。
	NativePlugins map[string]NativeReference `json:"nativePlugins,omitempty"`
	// Authentication 是可选包内授权入口。
	Authentication *Authentication `json:"authentication,omitempty"`
}

func (p *Package) loadIntegration(root string) error {
	path := filepath.Join(root, ".neuxally/integration.json")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	}
	path, err := contained(root, ".neuxally/integration.json")
	if err != nil {
		return status_error.PluginManifestInvalid
	}
	raw, err := readObject(path)
	if err != nil {
		return status_error.PluginManifestInvalid
	}
	data, _ := json.Marshal(raw)
	if json.Unmarshal(data, &p.Integration) != nil {
		return status_error.PluginManifestInvalid
	}
	for cli, ref := range p.Integration.NativePlugins {
		if (cli != "codex" && cli != "claude") || !safeRelative(ref.Path) {
			return status_error.PluginManifestInvalid
		}
	}
	if auth := p.Integration.Authentication; auth != nil {
		if err := validateAuthenticationBindings(p.Integration); err != nil {
			return err
		}
		if auth.Strategy != constants.PluginAuthCommandStrategy || auth.Protocol != constants.PluginAuthProtocol {
			p.diagnose("authentication", "unsupported_authentication")
			return nil
		}
		if auth.Command != "node" && !safeRelative(auth.Command) {
			return status_error.PluginManifestInvalid
		}
		entry := auth.Command
		if auth.Command == "node" {
			if len(auth.Args) == 0 || !safeRelative(auth.Args[0]) {
				return status_error.PluginManifestInvalid
			}
			entry = auth.Args[0]
		}
		if _, err := contained(root, entry); err != nil {
			return status_error.PluginManifestInvalid
		}
	}
	return nil
}

func safeRelative(value string) bool {
	clean := filepath.Clean(filepath.FromSlash(value))
	return value != "" && !filepath.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// LoadSource 解析逻辑入口，原生引用始终相对完整来源根校验。
func LoadSource(source, entry string) (Package, error) {
	root, err := contained(source, entry)
	if err != nil {
		return Package{}, status_error.PluginContentInvalid
	}
	pkg, err := Load(root)
	if err != nil {
		return pkg, err
	}
	if filepath.Clean(entry) != "." {
		pkg.SourcePath = filepath.ToSlash(filepath.Clean(entry))
	}
	pkg.NativeImplementations = map[string]NativeImplementation{}
	// 没有独立引用的运行目标使用包自身能力，同样必须逐项验证授权绑定。
	for _, cli := range []string{"codex", "claude"} {
		if _, referenced := pkg.Integration.NativePlugins[cli]; !referenced {
			if _, err := authenticationTarget(pkg, pkg, cli); err != nil {
				return pkg, err
			}
		}
	}
	for cli, ref := range pkg.Integration.NativePlugins {
		target, err := contained(source, ref.Path)
		if err != nil {
			return pkg, status_error.PluginContentInvalid
		}
		native, err := Load(target)
		if err != nil {
			return pkg, err
		}
		configured, err := authenticationTarget(pkg, native, cli)
		if err != nil {
			return pkg, err
		}
		extension, err := ClientExtension(configured)
		if err != nil {
			return pkg, err
		}
		digest, err := pluginArchive.Digest(target)
		if err != nil {
			return pkg, err
		}
		pkg.NativeImplementations[cli] = NativeImplementation{Path: ref.Path, Name: native.Manifest.Name, Version: native.Manifest.Version, Format: native.Format, Digest: digest, Components: native.NativeComponents, Config: extension.Config, TrustRequired: native.RequiresTrust(), Skills: nativeSkillSummaries(native), Servers: nativeServerSummaries(native), SkillCount: len(native.Skills), ServerCount: len(native.Servers)}
	}
	_, err = ClientExtension(pkg)
	return pkg, err
}

// PackageRoot 返回固定来源内的逻辑包根。
func PackageRoot(source string, pkg Package) (string, error) {
	if pkg.SourcePath == "" {
		return source, nil
	}
	root, err := contained(source, pkg.SourcePath)
	if err != nil {
		return "", status_error.PluginContentInvalid
	}
	return root, nil
}

// SelectTarget 优先使用明确引用，失败不静默回退到另一份内容。
func SelectTarget(source string, pkg Package, cli string) (Package, string, error) {
	if ref, ok := pkg.Integration.NativePlugins[cli]; ok {
		root, err := contained(source, ref.Path)
		if err != nil {
			return Package{}, "", status_error.PluginContentInvalid
		}
		target, err := Load(root)
		if err == nil {
			target, err = authenticationTarget(pkg, target, cli)
		}
		return target, root, err
	}
	root, err := PackageRoot(source, pkg)
	if err != nil {
		return Package{}, "", err
	}
	if len(pkg.Skills) == 0 && len(pkg.Servers) == 0 && len(pkg.Integration.NativePlugins) > 0 {
		return Package{}, "", status_error.PluginNotReady
	}
	target, err := authenticationTarget(pkg, pkg, cli)
	return target, root, err
}

// NativeComponent 描述原生完整保留的组件；投射路径不宣称支持其执行语义。
type NativeComponent struct {
	// Kind 是原生组件类别。
	Kind string `json:"kind"`
	// Paths 是声明或默认发现的包内路径。
	Paths []string `json:"paths"`
}

// NativeImplementation 是精确目标来源摘要，不表示已经加载。
type NativeImplementation struct {
	// Path 是固定来源内的原生目录。
	Path string `json:"path"`
	// Name 是实际原生名称。
	Name string `json:"name"`
	// Version 是实际原生版本。
	Version string `json:"version"`
	// Format 是实际原生格式。
	Format string `json:"format"`
	// Digest 是实际原生完整摘要。
	Digest string `json:"digest"`
	// Components 是保留的原生组成。
	Components []NativeComponent `json:"components"`
	// Config 是实际原生包所需配置，必填约束只用于选中该目标时。
	Config []ConfigurationField `json:"config"`
	// TrustRequired 表示实际原生包包含需要执行信任的组件或授权入口。
	TrustRequired bool `json:"trustRequired"`
	// 技能摘要
	Skills []NativeSkillSummary `json:"skills"`
	// 连接摘要
	Servers []NativeServerSummary `json:"servers"`
	// SkillCount 是已发现技能数量。
	SkillCount int `json:"skillCount"`
	// ServerCount 是已发现连接器数量。
	ServerCount int `json:"serverCount"`
}

func (p *Package) loadNativeComponents(root string, raw map[string]json.RawMessage) {
	if p.Format == constants.PluginFormatPortable {
		settings, err := nativeManifest(root, *p)
		if err != nil {
			p.diagnose("com.openai", "invalid_extension")
			return
		}
		raw = settings
	} else if !p.compatibilityFormat() {
		return
	}
	// 原生机制会自行加载MCP声明，即使通用解析器不能投射，也必须先明确信任。
	_, declaredMCP := raw["mcpServers"]
	mcpPaths := []string{}
	if value, ok := raw["mcpServers"]; ok {
		var path string
		if json.Unmarshal(value, &path) == nil {
			mcpPaths = append(mcpPaths, path)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, ".mcp.json")); err == nil {
		mcpPaths = append(mcpPaths, ".mcp.json")
		declaredMCP = true
	}
	if declaredMCP {
		p.NativeComponents = append(p.NativeComponents, NativeComponent{Kind: "mcp", Paths: mcpPaths})
	}
	for _, kind := range []string{"hooks", "agents", "commands", "lspServers", "apps", "outputStyles", "settings", "dependencies", "channels", "workflows"} {
		value, declared := raw[kind]
		defaults := map[string]string{"hooks": "hooks/hooks.json", "agents": "agents", "commands": "commands", "lspServers": ".lsp.json", "apps": ".app.json", "outputStyles": "output-styles"}
		paths := []string{}
		if declared {
			var path string
			var list []string
			if json.Unmarshal(value, &path) == nil {
				paths = append(paths, path)
			} else if json.Unmarshal(value, &list) == nil {
				paths = append(paths, list...)
			}
		}
		if relative := defaults[kind]; relative != "" {
			if _, err := os.Lstat(filepath.Join(root, relative)); err == nil {
				if !slices.Contains(paths, relative) {
					paths = append(paths, relative)
				}
			}
		}
		if declared || len(paths) > 0 {
			p.NativeComponents = append(p.NativeComponents, NativeComponent{Kind: kind, Paths: paths})
		}
	}
}

// RequiresTrust 同时覆盖授权入口、逻辑内容和明确原生引用的可执行组件。
func (p Package) RequiresTrust() bool {
	if p.Integration.Authentication != nil || len(p.Servers) > 0 || len(p.NativeComponents) > 0 {
		return true
	}
	for _, target := range p.NativeImplementations {
		if target.TrustRequired || target.ServerCount > 0 || len(target.Components) > 0 {
			return true
		}
	}
	return false
}

// nativeManifest 选择完整平台对象，显式内联对象不逐字段回退到overlay。
func nativeManifest(root string, pkg Package) (map[string]json.RawMessage, error) {
	switch pkg.Format {
	case constants.PluginFormatCodex:
		return readObject(filepath.Join(root, ".codex-plugin/plugin.json"))
	case constants.PluginFormatClaude:
		return readObject(filepath.Join(root, ".claude-plugin/plugin.json"))
	default:
		if value, exists := pkg.Manifest.Extensions["com.openai"]; exists {
			var raw map[string]json.RawMessage
			if json.Unmarshal(value, &raw) != nil || raw == nil {
				return nil, status_error.PluginManifestInvalid
			}
			return raw, nil
		}
		if _, err := os.Lstat(filepath.Join(root, ".codex-plugin/plugin.json")); os.IsNotExist(err) {
			return map[string]json.RawMessage{}, nil
		}
		path, err := contained(root, ".codex-plugin/plugin.json")
		if err != nil {
			return nil, status_error.PluginContentInvalid
		}
		return readObject(path)
	}
}

// 上游技能摘要
type NativeSkillSummary struct {
	// 组件标识
	Key string `json:"key"`
	// 技能名称
	Name string `json:"name"`
	// 技能说明
	Description string `json:"description"`
}

// 上游连接摘要
type NativeServerSummary struct {
	// 组件标识
	Key string `json:"key"`
	// 连接名称
	Name string `json:"name"`
	// 传输方式
	Transport string `json:"transport"`
	// 服务源站
	Endpoint string `json:"endpoint,omitempty"`
}

func nativeSkillSummaries(pkg Package) []NativeSkillSummary {
	result := []NativeSkillSummary{}
	for _, skill := range pkg.Skills {
		result = append(result, NativeSkillSummary{Key: skill.Key, Name: skill.Name, Description: skill.Description})
	}
	return result
}

func nativeServerSummaries(pkg Package) []NativeServerSummary {
	result := []NativeServerSummary{}
	for _, server := range pkg.Servers {
		endpoint := ""
		// 仅公开服务源站，路径、查询、用户信息中可能包含凭据，不能进入能力摘要。
		if address, err := url.Parse(server.URL); err == nil && (address.Scheme == "https" || address.Scheme == "http") && address.Hostname() != "" {
			endpoint = address.Scheme + "://" + address.Host
		}
		result = append(result, NativeServerSummary{Key: server.Key, Name: server.Key, Transport: server.Type, Endpoint: endpoint})
	}
	return result
}

// SkillSource 只选择当前固定来源明确引用的目标；展示选择不参与运行时组件投射。
func SkillSource(source string, pkg Package, target string) (Package, string, error) {
	if target == "" {
		return pkg, source, nil
	}
	ref, exists := pkg.Integration.NativePlugins[target]
	if !exists {
		return Package{}, "", status_error.PluginContentInvalid
	}
	root, err := contained(source, ref.Path)
	if err != nil {
		return Package{}, "", status_error.PluginContentInvalid
	}
	selected, err := Load(root)
	return selected, root, err
}
