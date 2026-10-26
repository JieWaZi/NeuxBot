package plugins

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

var placeholderPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
var configurationKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ConfigurationField 描述一个真实可编辑配置，不保存配置值。
type ConfigurationField struct {
	// Key 是稳定配置键。
	Key string `json:"key"`
	// Label 是显示标签。
	Label string `json:"label"`
	// Description 是字段用途和格式说明。
	Description string `json:"description,omitempty"`
	// Secret 表示值必须加密且不能回显。
	Secret bool `json:"secret"`
	// Required 表示安装前必须配置。
	Required bool `json:"required"`
	// Default 是非敏感默认值。
	Default string `json:"default,omitempty"`
}

// ConfigurationBinding 在客户端命名空间内把共享设置绑定到明确服务。
type ConfigurationBinding struct {
	// Server 是包内稳定服务键。
	Server string `json:"server"`
	// Target 是客户端允许的注入位置。
	Target constants.PluginBindingTarget `json:"target"`
	// Name 是环境变量名或授权请求头名。
	Name string `json:"name"`
	// Value 是仅在客户端扩展内解释的配置模板。
	Value string `json:"value"`
}

// Extension 定义客户端共享配置，不改变标准发现规则。
type Extension struct {
	// DisplayName 是初始产品展示名。
	DisplayName string `json:"displayName,omitempty"`
	// Config 是该版本支持的配置字段。
	Config []ConfigurationField `json:"config"`
	// Bindings 是客户端明确管理的共享配置绑定。
	Bindings []ConfigurationBinding `json:"bindings"`
	// RequiredComponents 是必须可用的组件键。
	RequiredComponents []string `json:"requiredComponents"`
}

// ClientExtension 读取本机配置和 MCP 占位变量，并汇总原生目标的可选共享配置。
func ClientExtension(pkg Package) (Extension, error) {
	result := Extension{Config: []ConfigurationField{}, Bindings: []ConfigurationBinding{}, RequiredComponents: []string{}}
	if value, ok := pkg.Manifest.Extensions[constants.PluginExtensionNamespace]; ok {
		if !validClientExtension(value) || json.Unmarshal(value, &result) != nil {
			return result, status_error.PluginManifestInvalid
		}
	}
	keys := map[string]bool{}
	for index := range result.Config {
		field := &result.Config[index]
		if !configurationKeyPattern.MatchString(field.Key) || keys[field.Key] || field.Key == "PLUGIN_ROOT" || field.Key == "PLUGIN_DATA" || (field.Secret && field.Default != "") {
			return result, status_error.PluginManifestInvalid
		}
		keys[field.Key] = true
		if field.Label == "" {
			field.Label = field.Key
		}
	}
	servers := map[string]Server{}
	for _, server := range pkg.Servers {
		servers[server.Key] = server
	}
	bindingKeys := map[string]bool{}
	for _, binding := range result.Bindings {
		server, exists := servers[binding.Server]
		identity := binding.Server + "\x00" + string(binding.Target) + "\x00" + strings.ToLower(binding.Name)
		if !exists || binding.Name == "" || bindingKeys[identity] {
			return result, status_error.PluginManifestInvalid
		}
		bindingKeys[identity] = true
		switch binding.Target {
		case constants.PluginBindingTargetEnvironment:
			if server.Type != "stdio" || !configurationKeyPattern.MatchString(binding.Name) || strings.EqualFold(binding.Name, "PLUGIN_ROOT") || strings.EqualFold(binding.Name, "PLUGIN_DATA") {
				return result, status_error.PluginManifestInvalid
			}
		case constants.PluginBindingTargetHeader:
			if server.Type == "stdio" || !validRemote("https://example.invalid", map[string]string{binding.Name: ""}) {
				return result, status_error.PluginManifestInvalid
			}
		default:
			return result, status_error.PluginManifestInvalid
		}
		for _, match := range placeholderPattern.FindAllStringSubmatch(binding.Value, -1) {
			key := match[1]
			if key == "PLUGIN_ROOT" || key == "PLUGIN_DATA" || keys[key] {
				continue
			}
			keys[key] = true
			result.Config = append(result.Config, ConfigurationField{Key: key, Label: key, Secret: true, Required: true})
		}
	}
	if pkg.compatibilityFormat() {
		for _, server := range pkg.Servers {
			values := []string{server.Command, server.Cwd, server.URL}
			values = append(values, server.Args...)
			for _, value := range server.Env {
				values = append(values, value)
			}
			for name, value := range server.Headers {
				if !selfHeaderAuthenticationCovered(pkg, server, name) {
					values = append(values, value)
				}
			}
			for _, value := range values {
				for _, match := range placeholderPattern.FindAllStringSubmatch(value, -1) {
					key := match[1]
					if key == "PLUGIN_ROOT" || key == "PLUGIN_DATA" || key == "CLAUDE_PLUGIN_ROOT" || key == "CLAUDE_PLUGIN_DATA" || keys[key] {
						continue
					}
					keys[key] = true
					result.Config = append(result.Config, ConfigurationField{Key: key, Label: key, Secret: true, Required: true})
				}
			}
		}
	}
	fields := map[string]ConfigurationField{}
	for _, field := range result.Config {
		fields[field.Key] = field
	}
	targets := make([]string, 0, len(pkg.NativeImplementations))
	for target := range pkg.NativeImplementations {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		for _, field := range pkg.NativeImplementations[target].Config {
			if !configurationKeyPattern.MatchString(field.Key) || field.Key == "PLUGIN_ROOT" || field.Key == "PLUGIN_DATA" || (field.Secret && field.Default != "") {
				return result, status_error.PluginManifestInvalid
			}
			if previous, exists := fields[field.Key]; exists {
				if previous.Secret != field.Secret || previous.Default != field.Default {
					return result, status_error.PluginManifestInvalid
				}
				continue
			}
			// 不同目标各自校验必填项，逻辑资源只负责识别并受保护地保存共享值。
			field.Required = false
			if field.Label == "" {
				field.Label = field.Key
			}
			fields[field.Key] = field
			result.Config = append(result.Config, field)
		}
	}
	sort.Slice(result.Config, func(i, j int) bool { return result.Config[i].Key < result.Config[j].Key })
	return result, nil
}

// ValidateValues 检查一份共享设置能否用于指定版本，包含未选中或停用的安装版本。
func ValidateValues(pkg Package, values map[string]string, secrets map[string]string) error {
	extension, err := ClientExtension(pkg)
	if err != nil {
		return err
	}
	supported := map[string]ConfigurationField{}
	for _, field := range extension.Config {
		supported[field.Key] = field
		value := values[field.Key]
		if field.Secret {
			value = secrets[field.Key]
			if _, exists := values[field.Key]; exists {
				return status_error.PluginConfigurationConflict
			}
		} else if value == "" {
			value = field.Default
		}
		if field.Required && strings.TrimSpace(value) == "" {
			return status_error.PluginNotReady
		}
	}
	for key := range values {
		field, ok := supported[key]
		if !ok || field.Secret {
			return status_error.PluginConfigurationConflict
		}
	}
	for key := range secrets {
		field, ok := supported[key]
		if !ok || !field.Secret {
			return status_error.PluginConfigurationConflict
		}
	}
	return nil
}

// Expand 替换明确受管变量，未知变量保持失败而非交付含糊配置。
func Expand(value string, variables map[string]string) (string, error) {
	missing := false
	result := placeholderPattern.ReplaceAllStringFunc(value, func(token string) string {
		key := token[2 : len(token)-1]
		replacement, exists := variables[key]
		if !exists {
			missing = true
		}
		return replacement
	})
	if missing {
		return "", status_error.PluginNotReady
	}
	return result, nil
}

// ExpandPortable 只替换标准保留变量，未知占位文本保持字面量且不递归展开。
func ExpandPortable(value, root, data string) string {
	return strings.NewReplacer("${PLUGIN_ROOT}", root, "${PLUGIN_DATA}", data).Replace(value)
}

// validClientExtension 拒绝把显式 null 当作未配置，其他命名空间仍由上游保留。
func validClientExtension(value json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(value, &fields) != nil || fields == nil {
		return false
	}
	// 旧宿主供应商适配已经移至独立插件包，拒绝声明以免静默丢失认证。
	if raw, exists := fields["provider"]; exists {
		var provider string
		if !validString(raw) || json.Unmarshal(raw, &provider) != nil || provider != "" {
			return false
		}
	}
	for _, key := range []string{"displayName"} {
		if raw, ok := fields[key]; ok && !validString(raw) {
			return false
		}
	}
	for _, key := range []string{"scopes", "requiredComponents"} {
		if raw, ok := fields[key]; ok && !validStringArray(raw) {
			return false
		}
	}
	for _, key := range []string{"config", "bindings"} {
		raw, exists := fields[key]
		if !exists {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil || items == nil {
			return false
		}
		for _, item := range items {
			var object map[string]json.RawMessage
			if json.Unmarshal(item, &object) != nil || object == nil {
				return false
			}
			for _, raw := range object {
				if string(raw) == "null" {
					return false
				}
			}
		}
	}
	return true
}

// selfHeaderAuthenticationCovered 只豁免所有自身运行目标都由桥接接管的同一位置。
// 引用其他包的目标不使用此处内容；同名变量在其他位置仍由正常配置扫描校验。
func selfHeaderAuthenticationCovered(pkg Package, server Server, name string) bool {
	covered := false
	for _, cli := range []string{"codex", "claude"} {
		if _, referenced := pkg.Integration.NativePlugins[cli]; referenced {
			continue
		}
		found := false
		for _, binding := range TargetAuthenticationBindings(pkg, cli) {
			if binding.Server == server.Key && binding.URL == server.URL && server.Type != "stdio" && strings.EqualFold(binding.Header, name) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
		covered = true
	}
	return covered
}
