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
	// Config 是该版本支持的配置字段。
	Config []ConfigurationField `json:"config"`
	// Bindings 是客户端明确管理的共享配置绑定。
	Bindings []ConfigurationBinding `json:"bindings"`
}

// ClientExtension 只读取新扩展显式字段及绑定，不从模板推导配置事实。
func ClientExtension(pkg Package) (Extension, error) {
	if err := ValidateContract(pkg); err != nil {
		return Extension{}, err
	}
	result := Extension{Config: []ConfigurationField{}, Bindings: []ConfigurationBinding{}}
	raw, err := neuxExtensionObject(pkg)
	if err != nil {
		return result, err
	}
	if value, ok := raw["configuration"]; ok {
		var configuration struct {
			// Fields 是作者明确声明的可编辑配置。
			Fields []ConfigurationField `json:"fields"`
			// Bindings 只注入已经发现的组件。
			Bindings []ConfigurationBinding `json:"bindings"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(value)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&configuration) != nil {
			return result, status_error.PluginManifestInvalid
		}
		result.Config = configuration.Fields
		result.Bindings = configuration.Bindings
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
			if !keys[match[1]] {
				return result, status_error.PluginManifestInvalid
			}
		}
	}
	if err := validateMCPAuthenticationBindings(pkg, result); err != nil {
		return result, err
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
