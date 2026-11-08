package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// Authentication 声明包内授权桥接，不复制供应商协议。
type Authentication struct {
	// SkillBindings 精确限定技能可取得的私有环境键和路径键。
	SkillBindings []SkillAuthenticationBinding `json:"skillBindings,omitempty"`
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

// Integration 保存唯一标准包归一后的授权声明。
type Integration struct {
	// Authentication 是可选包内授权入口。
	Authentication *Authentication `json:"authentication,omitempty"`
}

// neuxExtensionObject 验证唯一宿主扩展入口；阶段一不接纳管理驱动。
func neuxExtensionObject(pkg Package) (map[string]json.RawMessage, error) {
	for _, legacy := range []string{"com.ally", "com.neuxally"} {
		if _, exists := pkg.Manifest.Extensions[legacy]; exists {
			return nil, status_error.PluginManifestInvalid
		}
	}
	value, exists := pkg.Manifest.Extensions[constants.PluginExtensionNamespace]
	if !exists {
		return map[string]json.RawMessage{}, nil
	}
	var fields map[string]json.RawMessage
	if !nonNullJSON(value) || json.Unmarshal(value, &fields) != nil || fields == nil {
		return nil, status_error.PluginManifestInvalid
	}
	var version int
	if json.Unmarshal(fields["schemaVersion"], &version) != nil {
		return nil, status_error.PluginManifestInvalid
	}
	if version != constants.PluginExtensionSchemaVersion {
		return nil, status_error.PluginCapabilityUnsupported
	}
	for key, value := range fields {
		if key == "management" {
			return nil, status_error.PluginCapabilityUnsupported
		}
		if string(value) == "null" || (key != "schemaVersion" && key != "presentation" && key != "configuration" && key != "authentication") {
			return nil, status_error.PluginManifestInvalid
		}
	}
	return fields, nil
}

func (p *Package) loadIntegration(root string) error {
	raw, err := neuxExtensionObject(*p)
	if err != nil {
		return err
	}
	value, exists := raw["authentication"]
	if !exists {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(value, &fields) != nil || fields == nil {
		return status_error.PluginManifestInvalid
	}
	for key := range fields {
		if key != "strategy" && key != "protocol" && key != "command" && key != "args" && key != "preferredAction" && key != "bindings" && key != "skillBindings" && key != "scopes" {
			return status_error.PluginManifestInvalid
		}
	}
	var auth Authentication
	if json.Unmarshal(value, &auth) != nil || auth.Strategy != constants.PluginAuthCommandStrategy || auth.Protocol != constants.PluginAuthProtocol {
		return status_error.PluginManifestInvalid
	}
	if len(auth.Scopes) > 128 || len(auth.Args) > 128 {
		return status_error.PluginManifestInvalid
	}
	for _, scope := range auth.Scopes {
		if strings.TrimSpace(scope) == "" || len(scope) > 4096 || strings.ContainsRune(scope, 0) {
			return status_error.PluginManifestInvalid
		}
	}
	for _, arg := range auth.Args {
		if strings.ContainsRune(arg, 0) {
			return status_error.PluginManifestInvalid
		}
	}
	p.Integration.Authentication = &auth
	if err := validateAuthenticationBindings(p.Integration); err != nil {
		return err
	}
	entry := auth.Command
	if auth.Command == "node" {
		if len(auth.Args) == 0 {
			return status_error.PluginManifestInvalid
		}
		entry = auth.Args[0]
	}
	if !safeRelative(entry) || (auth.Command == "node" && strings.HasPrefix(entry, "-")) {
		return status_error.PluginManifestInvalid
	}
	path, err := contained(root, entry)
	if err != nil {
		return status_error.PluginManifestInvalid
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return status_error.PluginManifestInvalid
	}
	if err := validateSkillAuthenticationBindings(*p); err != nil {
		return err
	}
	for _, cli := range []string{"codex", "claude"} {
		if _, err := authenticationTarget(*p, *p, cli); err != nil {
			return err
		}
	}
	return nil
}

func safeRelative(value string) bool {
	clean := filepath.Clean(filepath.FromSlash(value))
	return value != "" && !filepath.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// LoadSource 解析可信来源中的唯一逻辑根，不导入其他原生来源包。
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
	return pkg, nil
}

// PackageRoot 返回固定来源内的逻辑包根。
func PackageRoot(source string, pkg Package) (string, error) {
	if err := ValidateContract(pkg); err != nil {
		return "", err
	}
	if pkg.SourcePath == "" {
		return source, nil
	}
	root, err := contained(source, pkg.SourcePath)
	if err != nil {
		return "", status_error.PluginContentInvalid
	}
	return root, nil
}

// SelectTarget 仅从固定标准包派生当前 CLI，不接受其他输入来源。
func SelectTarget(source string, pkg Package, cli string) (Package, string, error) {
	if err := ValidateContract(pkg); err != nil {
		return Package{}, "", err
	}
	root, err := PackageRoot(source, pkg)
	if err != nil {
		return Package{}, "", err
	}
	target, err := authenticationTarget(pkg, pkg, cli)
	return target, root, err
}

// RequiresTrust 覆盖包内授权入口和 MCP 执行组件。
func (p Package) RequiresTrust() bool {
	return p.Integration.Authentication != nil || len(p.Servers) > 0
}

// nativeManifest 仅返回根 OpenAI 声明，派生输出不回读原生入口。
func nativeManifest(pkg Package) (map[string]json.RawMessage, error) {
	value, exists := pkg.Manifest.Extensions["com.openai"]
	if !exists {
		return map[string]json.RawMessage{}, nil
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(value, &raw) != nil || raw == nil {
		return nil, status_error.PluginManifestInvalid
	}
	return raw, nil
}

// nonNullJSON 避免显式 null 被结构体零值吞掉后扩大宿主声明含义。
func nonNullJSON(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var valid func(any) bool
	valid = func(value any) bool {
		switch item := value.(type) {
		case nil:
			return false
		case map[string]any:
			for _, value := range item {
				if !valid(value) {
					return false
				}
			}
		case []any:
			for _, value := range item {
				if !valid(value) {
					return false
				}
			}
		}
		return true
	}
	return valid(value)
}
