// Package plugins 解析本地插件包，保留原始内容并生成可交付组件与诊断。
package plugins

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/skills"
)

const maxMetadataBytes = 1024 * 1024

var pluginNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,62}[a-z0-9])?$`)

// Author 保存包作者的公开署名资料。
type Author struct {
	// Name 是作者署名。
	Name string `json:"name,omitempty"`
	// Email 是公开联系地址。
	Email string `json:"email,omitempty"`
	// URL 是作者公开主页。
	URL string `json:"url,omitempty"`
}

// Manifest 保存标准元数据；客户端扩展不覆盖标准组件发现。
type Manifest struct {
	// Schema 是固定规范标识。
	Schema string `json:"$schema,omitempty"`
	// Name 是包声明名称。
	Name string `json:"name"`
	// Version 是标准清单声明的插件包版本。
	Version string `json:"version,omitempty"`
	// Description 是能力说明。
	Description string `json:"description,omitempty"`
	// Author 是当前插件包的作者资料。
	Author Author `json:"author"`
	// Homepage 是公开说明入口。
	Homepage string `json:"homepage,omitempty"`
	// Repository 是清单声明的源码或能力来源。
	Repository string `json:"repository,omitempty"`
	// License 是清单声明的插件包许可标识。
	License string `json:"license,omitempty"`
	// Keywords 是包声明标签。
	Keywords []string `json:"keywords"`
	// Extensions 是按命名空间保留的客户端扩展。
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
}

// Diagnostic 保存组件局部失败和未支持能力，不含凭据。
type Diagnostic struct {
	// Component 是包内组件或字段身份。
	Component string `json:"component"`
	// Code 是可本地化的稳定诊断标识。
	Code string `json:"code"`
}

// Skill 是插件版本派生的技能，不拥有独立安装事实。
type Skill struct {
	// Key 是版本内稳定组件键。
	Key string `json:"key"`
	// Name 是 SKILL.md 的真实声明名。
	Name string `json:"name"`
	// Description 是技能用途。
	Description string `json:"description"`
	// Path 是相对包根的交付目录。
	Path string `json:"path"`
	// Digest 是完整技能内容摘要。
	Digest string `json:"digest,omitempty"`
}

// Server 保存插件内 MCP 连接定义，不包含本机共享凭据。
type Server struct {
	// Authentication 是标准 MCP 公开认证方式，不含凭据。
	Authentication *MCPAuthentication `json:"authentication,omitempty"`
	// Key 是清单内稳定服务键。
	Key string `json:"key"`
	// Type 是标准 MCP 传输。
	Type string `json:"type"`
	// Command 是程序令牌或包内相对程序。
	Command string `json:"command,omitempty"`
	// Args 是保持原序的程序参数。
	Args []string `json:"args,omitempty"`
	// Env 是待展开的环境定义。
	Env map[string]string `json:"env,omitempty"`
	// Cwd 是规范允许的工作目录。
	Cwd string `json:"cwd,omitempty"`
	// URL 是待展开的外部服务地址。
	URL string `json:"url,omitempty"`
	// Headers 是待展开的请求头定义。
	Headers map[string]string `json:"headers,omitempty"`
}

// UpstreamSource 保留来源记录明确声明的业务上游资料。
type UpstreamSource struct {
	// 作者
	Author string `json:"author,omitempty"`
	// 版本标签
	Tag string `json:"tag,omitempty"`
	// 发布版本
	Version string `json:"version,omitempty"`
	// 源码仓库
	Repository string `json:"repository,omitempty"`
	// 许可
	License string `json:"license,omitempty"`
}

// 插件包
type Package struct {
	// ContractVersion 是宿主校验后的解析契约，缺失的历史快照不可执行。
	ContractVersion string `json:"contractVersion"`
	// 元数据
	Manifest Manifest `json:"manifest"`
	// 包格式
	Format string `json:"format"`
	// 展示资料
	Presentation Presentation `json:"presentation"`
	// 上游声明
	Upstream *UpstreamSource `json:"upstream,omitempty"`
	// 软件图标
	Icon string `json:"icon,omitempty"`
	// 插件说明
	Readme string `json:"readme,omitempty"`
	// 来源入口
	SourcePath string `json:"sourcePath,omitempty"`
	// 原生实现
	Integration Integration `json:"integration,omitempty"`
	// 正文状态
	ReadmeStatus string `json:"readmeStatus,omitempty"`
	// 正文路径
	ReadmePath string `json:"readmePath,omitempty"`
	// 包含技能
	Skills []Skill `json:"skills"`
	// 包含连接器
	Servers []Server `json:"servers"`
	// 兼容性说明
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Load 只读取标准根清单和固定组件目录，拒绝旧输入入口。
func Load(root string) (pkg Package, resultErr error) {
	defer func() {
		if resultErr != nil {
			pkg.ContractVersion = ""
		}
	}()
	pkg = Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Skills: []Skill{}, Servers: []Server{}, Diagnostics: []Diagnostic{}}
	if err := validateLinks(root); err != nil {
		return pkg, err
	}
	for _, legacy := range []string{".codex-plugin", ".neuxally", ".app.json", "hooks", "agents", "commands", ".lsp.json", ".opencode", "extensions", "prompts", "themes", "output-styles", "workflows", "GEMINI.md", "rules", "policies", "monitors", "SKILL.md"} {
		if _, err := os.Lstat(filepath.Join(root, legacy)); !os.IsNotExist(err) {
			return pkg, status_error.PluginManifestInvalid
		}
	}
	path, err := contained(root, "plugin.json")
	if err != nil {
		return pkg, status_error.PluginManifestInvalid
	}
	raw, err := readObject(path)
	if err != nil || !stringEquals(raw["$schema"], constants.PluginManifestSchema) {
		return pkg, status_error.PluginManifestInvalid
	}
	allowed := map[string]bool{"$schema": true, "name": true, "version": true, "description": true, "author": true, "homepage": true, "repository": true, "license": true, "keywords": true, "extensions": true}
	for key := range raw {
		if !allowed[key] {
			return pkg, status_error.PluginManifestInvalid
		}
	}
	if value, exists := raw["extensions"]; exists {
		var extensions map[string]json.RawMessage
		if json.Unmarshal(value, &extensions) != nil || extensions == nil {
			return pkg, status_error.PluginManifestInvalid
		}
	}
	if value, exists := raw["author"]; exists {
		var author Author
		decoder := json.NewDecoder(strings.NewReader(string(value)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&author) != nil || string(value) == "null" {
			return pkg, status_error.PluginManifestInvalid
		}
	}
	for _, key := range []string{"name", "version", "description", "homepage", "repository", "license"} {
		if value, ok := raw[key]; ok {
			var text string
			if json.Unmarshal(value, &text) != nil || string(value) == "null" {
				return pkg, status_error.PluginManifestInvalid
			}
		}
	}
	if value, ok := raw["keywords"]; ok && !validStringArray(value) {
		return pkg, status_error.PluginManifestInvalid
	}
	encoded, _ := json.Marshal(raw)
	if json.Unmarshal(encoded, &pkg.Manifest) != nil {
		return pkg, status_error.PluginManifestInvalid
	}
	if !pluginNamePattern.MatchString(pkg.Manifest.Name) || strings.Contains(pkg.Manifest.Name, "--") || strings.Contains(pkg.Manifest.Name, "..") {
		return pkg, status_error.PluginManifestInvalid
	}
	if pkg.Manifest.Keywords == nil {
		pkg.Manifest.Keywords = []string{}
	}
	if _, err := neuxExtensionObject(pkg); err != nil {
		return pkg, err
	}
	pkg.loadSkills(root, "skills", map[string]bool{})
	if err := pkg.loadMCPFile(root, "mcp.json"); err != nil {
		return pkg, err
	}
	if err := pkg.loadIntegration(root); err != nil {
		return pkg, err
	}
	if err := validateNativeEntries(root, pkg); err != nil {
		return pkg, err
	}
	if err := pkg.loadPresentation(root); err != nil {
		return pkg, err
	}
	for _, diagnostic := range pkg.Diagnostics {
		if diagnostic.Code != "invalid_documentation" {
			return pkg, status_error.PluginManifestInvalid
		}
	}
	if _, err := ClientExtension(pkg); err != nil {
		return pkg, err
	}
	sort.Slice(pkg.Skills, func(i, j int) bool { return pkg.Skills[i].Key < pkg.Skills[j].Key })
	sort.Slice(pkg.Servers, func(i, j int) bool { return pkg.Servers[i].Key < pkg.Servers[j].Key })
	sort.Slice(pkg.Diagnostics, func(i, j int) bool {
		return pkg.Diagnostics[i].Component+pkg.Diagnostics[i].Code < pkg.Diagnostics[j].Component+pkg.Diagnostics[j].Code
	})
	return pkg, nil
}

func (pkg *Package) loadMCPFile(root, path string) error {
	resolved, pathErr := contained(root, path)
	if pathErr != nil {
		if !os.IsNotExist(pathErr) {
			pkg.diagnose(path, "invalid_component")
		}
		if os.IsNotExist(pathErr) {
			return nil
		}
		return status_error.PluginManifestInvalid
	}
	document, readErr := readObject(resolved)
	if readErr != nil {
		pkg.diagnose(path, "invalid_component")
		return status_error.PluginManifestInvalid
	}
	if pkg.Format == constants.PluginFormatPortable && !stringEquals(document["$schema"], constants.PluginMCPSchema) {
		pkg.diagnose(path, "unsupported_schema")
		return status_error.PluginManifestInvalid
	}
	if pkg.Format == constants.PluginFormatPortable {
		valid := true
		for key := range document {
			if key != "$schema" && key != "mcpServers" {
				valid = false
			}
		}
		if !valid {
			pkg.diagnose(path, "invalid_component")
			return status_error.PluginManifestInvalid
		}
	}
	var servers map[string]json.RawMessage
	if value, wrapped := document["mcpServers"]; wrapped {
		if json.Unmarshal(value, &servers) != nil || servers == nil {
			pkg.diagnose(path, "invalid_component")
			return status_error.PluginManifestInvalid
		}
	} else {
		pkg.diagnose(path, "invalid_component")
		return status_error.PluginManifestInvalid
	}
	return pkg.loadServers(root, servers)
}

func (p *Package) diagnose(component, code string) {
	p.Diagnostics = append(p.Diagnostics, Diagnostic{Component: component, Code: code})
}
func readObject(path string) (map[string]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxMetadataBytes {
		return nil, status_error.PluginContentInvalid
	}
	var value map[string]json.RawMessage
	decoder := json.NewDecoder(file)
	if decoder.Decode(&value) != nil || value == nil {
		return nil, status_error.PluginContentInvalid
	}
	// 第二个 JSON 值不能被当作未读取的尾部忽略。
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return nil, status_error.PluginContentInvalid
	}
	return value, nil
}
func stringEquals(raw json.RawMessage, want string) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil && value == want
}
func decodePaths(raw json.RawMessage, result *[]string) error {
	var one string
	if validString(raw) && json.Unmarshal(raw, &one) == nil {
		*result = []string{one}
		return nil
	}
	var paths []string
	if !validStringArray(raw) || json.Unmarshal(raw, &paths) != nil || paths == nil {
		return status_error.PluginContentInvalid
	}
	*result = paths
	return nil
}
func contained(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", status_error.PluginContentInvalid
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", status_error.PluginContentInvalid
	}
	return resolved, nil
}
func (p *Package) loadSkills(root, relative string, seen map[string]bool) {
	path, err := contained(root, relative)
	if err != nil {
		if !os.IsNotExist(err) {
			p.diagnose(relative, "invalid_component")
		}
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		p.diagnose(relative, "invalid_component")
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		p.diagnose(relative, "invalid_component")
		return
	}
	for _, entry := range entries {
		p.loadSkill(root, filepath.ToSlash(filepath.Join(relative, entry.Name())), seen)
	}
}
func (p *Package) loadSkill(root, relative string, seen map[string]bool) {
	// 默认目录发现和显式单技能路径必须形成同一组件身份。
	relative = filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if seen[relative] {
		return
	}
	seen[relative] = true
	path, err := contained(root, relative)
	if err != nil {
		p.diagnose(relative, "invalid_skill")
		return
	}
	if _, err := contained(root, filepath.Join(relative, "SKILL.md")); err != nil {
		if !os.IsNotExist(err) {
			p.diagnose(relative, "invalid_skill")
		}
		return
	}
	metadata, err := skills.ReadMetadata(path)
	if err != nil {
		p.diagnose(relative, "invalid_skill")
		return
	}
	digest, err := pluginArchive.Digest(path)
	if err != nil {
		p.diagnose(relative, "invalid_skill")
		return
	}
	p.Skills = append(p.Skills, Skill{Key: filepath.ToSlash(relative), Name: metadata.Name, Description: metadata.Description, Path: filepath.ToSlash(relative), Digest: digest})
}
func (p *Package) loadServers(root string, definitions map[string]json.RawMessage) error {
	keys := make([]string, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var server Server
		raw := definitions[key]
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &server) != nil || json.Unmarshal(raw, &fields) != nil || fields == nil || key == "" {
			p.diagnose(key, "invalid_mcp")
			return status_error.PluginManifestInvalid
		}
		valid := true
		for _, field := range []string{"type", "command", "cwd", "url"} {
			if raw, ok := fields[field]; ok && !validString(raw) {
				valid = false
			}
		}
		if raw, ok := fields["args"]; ok && !validStringArray(raw) {
			valid = false
		}
		for _, field := range []string{"env", "headers"} {
			if raw, ok := fields[field]; ok && !validStringMap(raw) {
				valid = false
			}
		}
		switch server.Type {
		case "stdio":
			if p.Format == constants.PluginFormatPortable && (strings.Contains(server.Command, "${") || strings.IndexFunc(server.Command, unicode.IsSpace) >= 0) {
				valid = false
			}
			if server.Command == "" {
				valid = false
			}
			commandForValidation, cwdForValidation := server.Command, server.Cwd
			if server.Cwd != "" && !validWorkingDirectory(root, cwdForValidation) {
				valid = false
			}
			if strings.ContainsAny(commandForValidation, "/\\") && !strings.HasPrefix(commandForValidation, "${PLUGIN_ROOT}/") && !strings.HasPrefix(commandForValidation, "./") {
				valid = false
			}
			if strings.HasPrefix(commandForValidation, "./") {
				_, err := contained(root, strings.TrimPrefix(strings.TrimPrefix(commandForValidation, "${PLUGIN_ROOT}/"), "./"))
				if err != nil {
					valid = false
				}
			}
			if _, exists := server.Env["PLUGIN_ROOT"]; exists {
				valid = false
			}
			if _, exists := server.Env["PLUGIN_DATA"]; exists {
				valid = false
			}
		case "streamable-http", "sse":
			if p.Format == constants.PluginFormatPortable && !validRemote(server.URL, server.Headers) {
				valid = false
			}
			if server.URL == "" {
				valid = false
			}
		default:
			valid = false
		}
		if p.Format == constants.PluginFormatPortable {
			allowed := map[string]bool{"type": true, "url": true, "headers": true, "extensions": true}
			if server.Type == "stdio" {
				allowed = map[string]bool{"type": true, "command": true, "args": true, "env": true, "cwd": true, "extensions": true}
			}
			for field := range fields {
				if !allowed[field] {
					valid = false
				}
			}
		}
		for _, existing := range p.Servers {
			if existing.Key == key {
				valid = false
			}
		}
		if !valid {
			p.diagnose(key, "invalid_mcp")
			return status_error.PluginManifestInvalid
		}
		server.Key = key
		if err := parseMCPAuthentication(fields["extensions"], &server); err != nil {
			p.diagnose(key, "unsupported_authentication")
			return err
		}
		if server.Args == nil {
			server.Args = []string{}
		}
		if server.Env == nil {
			server.Env = map[string]string{}
		}
		if server.Headers == nil {
			server.Headers = map[string]string{}
		}
		p.Servers = append(p.Servers, server)
	}
	return nil
}
func validWorkingDirectory(root, value string) bool {
	if strings.HasPrefix(value, "${PLUGIN_DATA}") {
		suffix := strings.TrimPrefix(value, "${PLUGIN_DATA}")
		if suffix != "" && !strings.HasPrefix(suffix, "/") {
			return false
		}
		clean := filepath.Clean("." + suffix)
		return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
	}
	relative := strings.TrimPrefix(value, "${PLUGIN_ROOT}")
	if relative != value {
		relative = "." + relative
	} else if !strings.HasPrefix(value, "./") {
		return false
	}
	path, err := contained(root, relative)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func validString(raw json.RawMessage) bool {
	var value string
	return string(raw) != "null" && json.Unmarshal(raw, &value) == nil
}
func validStringArray(raw json.RawMessage) bool {
	var values []json.RawMessage
	if string(raw) == "null" || json.Unmarshal(raw, &values) != nil {
		return false
	}
	for _, value := range values {
		if !validString(value) {
			return false
		}
	}
	return true
}
func validStringMap(raw json.RawMessage) bool {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return false
	}
	for _, value := range values {
		if !validString(value) {
			return false
		}
	}
	return true
}
func validRemote(address string, headers map[string]string) bool {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return false
	}
	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return false
		}
	}
	names := map[string]bool{}
	for key, value := range headers {
		lower := strings.ToLower(key)
		if names[lower] || key == "" || strings.ContainsAny(key, "()<>@,;:\"/[]?={} \t\r\n") {
			return false
		}
		for _, r := range key {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("!#$%&' *+-.^_`|~", r) && r != ' ') {
				return false
			}
		}
		names[lower] = true
		for _, r := range value {
			if (r < 32 && r != '\t') || r == 127 {
				return false
			}
		}
		if http.CanonicalHeaderKey(key) == "" {
			return false
		}
	}
	return true
}

// ValidateContract 拒绝未经过当前唯一标准输入解析的持久快照。
func ValidateContract(pkg Package) error {
	if pkg.ContractVersion != constants.PluginTargetContractVersion {
		return status_error.PluginPackageUnsupported
	}
	if pkg.Format != constants.PluginFormatPortable || pkg.Manifest.Schema != constants.PluginManifestSchema {
		return status_error.PluginManifestInvalid
	}
	if _, err := neuxExtensionObject(pkg); err != nil {
		return err
	}
	settings, err := nativeManifest(pkg)
	if err != nil {
		return err
	}
	for key := range settings {
		if key != "interface" {
			return status_error.PluginManifestInvalid
		}
	}

	if auth := pkg.Integration.Authentication; auth != nil {
		if auth.Strategy != constants.PluginAuthCommandStrategy || auth.Protocol != constants.PluginAuthProtocol {
			return status_error.PluginManifestInvalid
		}
		if err := validateAuthenticationBindings(pkg.Integration); err != nil {
			return err
		}
		if err := validateSkillAuthenticationBindings(pkg); err != nil {
			return err
		}
	}
	for _, server := range pkg.Servers {
		if server.Authentication != nil && server.Authentication.Type != "none" && server.Authentication.Type != "api_key" {
			return status_error.PluginManifestInvalid
		}
	}
	return nil
}

// MCPAuthentication 保存标准服务认证声明，凭据仍由配置或账号材料拥有。
type MCPAuthentication struct {
	// Type 是作者声明的认证模式。
	Type string `json:"type"`
	// HeaderScheme 是 API key 的请求头编码方式。
	HeaderScheme string `json:"headerScheme,omitempty"`
	// HeaderName 仅用于 custom_header 模式。
	HeaderName string `json:"headerName,omitempty"`
}

func parseMCPAuthentication(raw json.RawMessage, server *Server) error {
	if len(raw) == 0 {
		return nil
	}
	var extensions map[string]json.RawMessage
	if json.Unmarshal(raw, &extensions) != nil || extensions == nil {
		return status_error.PluginManifestInvalid
	}
	value, exists := extensions["com.openai"]
	if !exists {
		return nil
	}
	var openai map[string]json.RawMessage
	if json.Unmarshal(value, &openai) != nil || openai == nil {
		return status_error.PluginManifestInvalid
	}
	for key := range openai {
		if key != "auth" {
			return status_error.PluginManifestInvalid
		}
	}
	value, exists = openai["auth"]
	if !exists {
		return nil
	}
	var auth MCPAuthentication
	decoder := json.NewDecoder(strings.NewReader(string(value)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&auth) != nil {
		return status_error.PluginManifestInvalid
	}
	switch auth.Type {
	case "none":
		if auth.HeaderScheme != "" || auth.HeaderName != "" {
			return status_error.PluginManifestInvalid
		}
	case "oauth", "mixed":
		return status_error.PluginCapabilityUnsupported
	case "api_key":
		if server.Type == "stdio" {
			return status_error.PluginCapabilityUnsupported
		}
		switch auth.HeaderScheme {
		case "bearer", "basic":
			if auth.HeaderName != "" {
				return status_error.PluginManifestInvalid
			}
		case "custom_header":
			if strings.EqualFold(auth.HeaderName, "Host") || auth.HeaderName == "" || !validRemote("https://example.invalid", map[string]string{auth.HeaderName: ""}) {
				return status_error.PluginManifestInvalid
			}
		default:
			return status_error.PluginManifestInvalid
		}
	default:
		return status_error.PluginManifestInvalid
	}
	server.Authentication = &auth
	return nil
}

func validateMCPAuthenticationBindings(pkg Package, extension Extension) error {
	fields := map[string]ConfigurationField{}
	for _, field := range extension.Config {
		fields[field.Key] = field
	}
	for _, server := range pkg.Servers {
		auth := server.Authentication
		if auth == nil || auth.Type == "none" {
			continue
		}
		if auth.Type != "api_key" {
			return status_error.PluginManifestInvalid
		}
		header := "Authorization"
		prefix := "Bearer "
		if auth.HeaderScheme == "basic" {
			prefix = "Basic "
		}
		if auth.HeaderScheme == "custom_header" {
			header = auth.HeaderName
			prefix = ""
		}
		covered := false
		for _, binding := range extension.Bindings {
			if binding.Server != server.Key || binding.Target != constants.PluginBindingTargetHeader || !strings.EqualFold(binding.Name, header) {
				continue
			}
			matches := placeholderPattern.FindAllStringSubmatch(binding.Value, -1)
			if len(matches) != 1 || binding.Value != prefix+matches[0][0] || !fields[matches[0][1]].Secret || !fields[matches[0][1]].Required {
				return status_error.PluginManifestInvalid
			}
			covered = true
		}
		if !covered && pkg.Integration.Authentication != nil {
			for _, binding := range pkg.Integration.Authentication.Bindings {
				if binding.Server == server.Key && binding.URL == server.URL && strings.EqualFold(binding.Header, header) {
					covered = true
				}
			}
		}
		if !covered {
			return status_error.PluginManifestInvalid
		}
	}
	return nil
}
