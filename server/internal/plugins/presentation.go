package plugins

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"image"
	_ "image/gif" // 注册可验证的插件图标格式。
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"
)

const maxPresentationBytes = 256 * 1024
const maxReadmeBytes = 1024 * 1024

var brandColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// ThemeValues 分别保存作者明确声明的默认与暗色展示值。
type ThemeValues struct {
	// Default 是默认主题资源或颜色。
	Default string `json:"default,omitempty"`
	// Dark 是可选暗色主题资源或颜色。
	Dark string `json:"dark,omitempty"`
}

// SetupPresentation 保存安全的静态连接说明，不包含驱动能力。
type SetupPresentation struct {
	// Instructions 是固定语言的安全 Markdown 说明。
	Instructions map[string]string `json:"instructions,omitempty"`
	// HelpURL 是作者公开的 HTTPS 帮助地址。
	HelpURL string `json:"helpURL,omitempty"`
}

// Presentation 保存标准 interface 与显式本地化资料。
type Presentation struct {
	// DefaultPrompt 是按固定索引归一的作者提示词。
	DefaultPrompt []string `json:"defaultPrompt,omitempty"`
	// DefaultPromptLocalizations 与标准提示词按索引等长对应。
	DefaultPromptLocalizations map[string][]string `json:"defaultPromptLocalizations,omitempty"`
	// Capabilities 只描述业务能力，不授予权限。
	Capabilities []string `json:"capabilities,omitempty"`
	// SupportURL 是公开支持入口。
	SupportURL string `json:"supportURL,omitempty"`
	// BrandColor 是经过颜色格式校验的主题品牌色。
	BrandColor ThemeValues `json:"brandColor,omitempty"`
	// Icons 是分别校验后的主题内嵌图标。
	Icons ThemeValues `json:"icons,omitempty"`
	// Setup 是显式引用资料中的静态连接说明。
	Setup *SetupPresentation `json:"setup,omitempty"`

	// 本地名称
	DisplayNames map[string]string `json:"displayNames,omitempty"`
	// 本地简介
	LocalizedDescriptions map[string]string `json:"localizedDescriptions,omitempty"`
	// 展示名称
	DisplayName string `json:"displayName,omitempty"`
	// 简短介绍
	ShortDescription string `json:"shortDescription,omitempty"`
	// 完整介绍
	LongDescription string `json:"longDescription,omitempty"`
	// 开发者
	DeveloperName string `json:"developerName,omitempty"`
	// 分类
	Category string `json:"category,omitempty"`
	// 官方网站
	WebsiteURL string `json:"websiteURL,omitempty"`
	// 隐私政策
	PrivacyPolicyURL string `json:"privacyPolicyURL,omitempty"`
	// 服务条款
	TermsOfServiceURL string `json:"termsOfServiceURL,omitempty"`
}

func (p *Package) loadPresentation(root string) error {
	p.Presentation = Presentation{DisplayName: p.Manifest.Name, ShortDescription: p.Manifest.Description, DeveloperName: p.Manifest.Author.Name, WebsiteURL: publicURL(p.Manifest.Homepage)}
	settings, err := nativeManifest(*p)
	if err != nil {
		return err
	}
	for key := range settings {
		if key != "interface" {
			return status_error.PluginCapabilityUnsupported
		}
	}
	presentation := map[string]json.RawMessage{}
	if value, exists := settings["interface"]; exists {
		if json.Unmarshal(value, &presentation) != nil || presentation == nil {
			return status_error.PluginManifestInvalid
		}
	}
	for key, target := range map[string]*string{"displayName": &p.Presentation.DisplayName, "shortDescription": &p.Presentation.ShortDescription, "longDescription": &p.Presentation.LongDescription, "developerName": &p.Presentation.DeveloperName, "category": &p.Presentation.Category} {
		if value, exists := presentation[key]; exists {
			if !validString(value) {
				return status_error.PluginManifestInvalid
			}
			*target = metadataString(presentation, key)
		}
	}
	for key, target := range map[string]*string{"websiteURL": &p.Presentation.WebsiteURL, "privacyPolicyURL": &p.Presentation.PrivacyPolicyURL, "termsOfServiceURL": &p.Presentation.TermsOfServiceURL, "supportURL": &p.Presentation.SupportURL} {
		if value, exists := presentation[key]; exists {
			if !validString(value) {
				return status_error.PluginManifestInvalid
			}
			text := metadataString(presentation, key)
			if text != "" && publicURL(text) == "" {
				return status_error.PluginManifestInvalid
			}
			*target = text
		}
	}
	if value, exists := presentation["defaultPrompt"]; exists {
		if err := decodePaths(value, &p.Presentation.DefaultPrompt); err != nil {
			return status_error.PluginManifestInvalid
		}
	}
	if value, exists := presentation["capabilities"]; exists {
		if !validStringArray(value) || json.Unmarshal(value, &p.Presentation.Capabilities) != nil {
			return status_error.PluginManifestInvalid
		}
	}
	for key, target := range map[string]*string{"brandColor": &p.Presentation.BrandColor.Default, "brandColorDark": &p.Presentation.BrandColor.Dark} {
		if value, exists := presentation[key]; exists {
			if !validString(value) {
				return status_error.PluginManifestInvalid
			}
			color := metadataString(presentation, key)
			if !brandColorPattern.MatchString(color) {
				return status_error.PluginManifestInvalid
			}
			*target = color
		}
	}
	icons := map[string]string{}
	for _, key := range []string{"logo", "logoDark", "composerIcon", "composerIconDark"} {
		if value, exists := presentation[key]; exists {
			if !validString(value) || metadataString(presentation, key) == "" {
				return status_error.PluginManifestInvalid
			}
			icon, err := readIcon(root, metadataString(presentation, key))
			if err != nil {
				return err
			}
			icons[key] = icon
		}
	}
	p.Presentation.Icons = ThemeValues{Default: icons["logo"], Dark: icons["logoDark"]}
	if p.Presentation.Icons.Default == "" {
		p.Presentation.Icons.Default = icons["composerIcon"]
	}
	if p.Presentation.Icons.Dark == "" {
		p.Presentation.Icons.Dark = icons["composerIconDark"]
	}
	p.Icon = p.Presentation.Icons.Default
	extension, err := neuxExtensionObject(*p)
	if err != nil {
		return err
	}
	if value, exists := extension["presentation"]; exists {
		if !validString(value) {
			return status_error.PluginManifestInvalid
		}
		relative := metadataString(extension, "presentation")
		if !strings.HasPrefix(filepath.ToSlash(filepath.Clean(relative)), ".neuxbot/") {
			return status_error.PluginManifestInvalid
		}
		if _, err := contained(filepath.Join(root, ".neuxbot"), strings.TrimPrefix(filepath.ToSlash(filepath.Clean(relative)), ".neuxbot/")); err != nil {
			return status_error.PluginManifestInvalid
		}
		content, err := readBounded(root, relative, 32*1024)
		if err != nil {
			return err
		}
		var localized struct {
			// DisplayNames 是双语名称映射。
			DisplayNames map[string]string `json:"displayNames"`
			// LocalizedDescriptions 是双语简介。
			LocalizedDescriptions map[string]string `json:"localizedDescriptions"`
			// DefaultPromptLocalizations 与标准提示词等长对应。
			DefaultPromptLocalizations map[string][]string `json:"defaultPromptLocalizations"`
			// Setup 是静态安装帮助。
			Setup *SetupPresentation `json:"setup"`
		}
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&localized) != nil {
			return status_error.PluginManifestInvalid
		}
		if err := p.Presentation.SetLocalization(localized.DisplayNames, localized.LocalizedDescriptions); err != nil {
			return err
		}
		p.Presentation.DefaultPromptLocalizations = localized.DefaultPromptLocalizations
		p.Presentation.Setup = localized.Setup
	}
	if err := p.Presentation.Validate(); err != nil {
		return err
	}
	if content, err := readBounded(root, "provenance.json", maxMetadataBytes); err == nil {
		var provenance map[string]json.RawMessage
		var upstream UpstreamSource
		if json.Unmarshal(content, &provenance) == nil && json.Unmarshal(provenance["upstream"], &upstream) == nil {
			upstream.Repository = publicURL(upstream.Repository)
			p.Upstream = &upstream
		}
	}
	p.loadReadme(root, constants.PluginLocaleEnglish)
	return nil
}

// Validate 校验公开展示字段、提示词索引和静态连接帮助。
func (p Presentation) Validate() error {
	for _, text := range []string{p.DisplayName, p.ShortDescription, p.LongDescription, p.DeveloperName, p.Category} {
		if len(text) > maxMetadataBytes || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return status_error.PluginManifestInvalid
		}
	}
	for _, url := range []string{p.SupportURL, p.WebsiteURL, p.PrivacyPolicyURL, p.TermsOfServiceURL} {
		if url != "" && publicURL(url) == "" {
			return status_error.PluginManifestInvalid
		}
	}
	for _, color := range []string{p.BrandColor.Default, p.BrandColor.Dark} {
		if color != "" && !brandColorPattern.MatchString(color) {
			return status_error.PluginManifestInvalid
		}
	}
	if len(p.DefaultPrompt) > 20 || len(p.Capabilities) > 64 {
		return status_error.PluginContentLimitExceeded
	}
	for _, values := range [][]string{p.DefaultPrompt, p.Capabilities} {
		for _, text := range values {
			if len(text) > 4096 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) || strings.TrimSpace(text) == "" {
				return status_error.PluginManifestInvalid
			}
		}
	}
	for locale, values := range p.DefaultPromptLocalizations {
		if (locale != constants.PluginLocaleEnglish && locale != constants.PluginLocaleChinese) || len(values) != len(p.DefaultPrompt) {
			return status_error.PluginManifestInvalid
		}
		for _, text := range values {
			if text == "" || len(text) > 4096 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
				return status_error.PluginManifestInvalid
			}
		}
	}
	if p.Setup != nil {
		if url := p.Setup.HelpURL; url != "" && (!strings.HasPrefix(url, "https://") || publicURL(url) == "") {
			return status_error.PluginManifestInvalid
		}
		for locale, text := range p.Setup.Instructions {
			if (locale != constants.PluginLocaleEnglish && locale != constants.PluginLocaleChinese) || len(text) > 16*1024 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
				return status_error.PluginManifestInvalid
			}
		}
	}
	return nil
}

func metadataString(fields map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(fields[key], &value)
	return strings.TrimSpace(value)
}

func publicURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	return value
}

func readBounded(root, relative string, limit int64) ([]byte, error) {
	path, err := contained(root, relative)
	if err != nil {
		return nil, status_error.PluginContentInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, storageFailure(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, status_error.PluginContentInvalid
	}
	if info.Size() > limit {
		return nil, status_error.PluginContentLimitExceeded
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, storageFailure(err)
	}
	if int64(len(content)) > limit {
		return nil, status_error.PluginContentLimitExceeded
	}
	return content, nil
}

func readIcon(root, relative string) (string, error) {
	content, err := readBounded(root, relative, maxPresentationBytes)
	if err != nil {
		return "", err
	}
	return sanitizeIcon(content, strings.EqualFold(filepath.Ext(relative), ".svg"))
}

// SanitizeIcon 只接受有界内嵌图标，目录展示与包内资产复用同一内容校验。
func SanitizeIcon(value string) string {
	if len(value) > maxPresentationBytes*2 {
		return ""
	}
	prefix, encoded, ok := strings.Cut(value, ",")
	if !ok || (prefix != "data:image/svg+xml;base64" && prefix != "data:image/png;base64" && prefix != "data:image/jpeg;base64" && prefix != "data:image/gif;base64") {
		return ""
	}
	content, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(content) > maxPresentationBytes {
		return ""
	}
	result, err := sanitizeIcon(content, prefix == "data:image/svg+xml;base64")
	if err != nil || !strings.HasPrefix(result, prefix+",") {
		return ""
	}
	return result
}

func sanitizeIcon(content []byte, svg bool) (string, error) {
	mime := ""
	if svg {
		if !staticSVG(content) {
			return "", status_error.PluginContentInvalid
		}
		mime = "image/svg+xml"
	} else {
		config, format, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 {
			return "", status_error.PluginContentInvalid
		}
		switch format {
		case "png", "jpeg", "gif":
			mime = "image/" + format
		default:
			return "", status_error.PluginContentInvalid
		}
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(content), nil
}

// staticSVG 仅接受静态矢量图形，拒绝脚本、事件、样式与所有外部资源引用。
func staticSVG(content []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	depth, elements := 0, 0
	allowed := map[string]bool{"svg": true, "g": true, "path": true, "rect": true, "circle": true, "ellipse": true, "line": true, "polyline": true, "polygon": true, "defs": true, "linearGradient": true, "radialGradient": true, "stop": true, "clipPath": true, "mask": true, "title": true, "desc": true}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return elements > 0 && depth == 0
		}
		if err != nil {
			return false
		}
		switch value := token.(type) {
		case xml.StartElement:
			if !allowed[value.Name.Local] || (value.Name.Space != "" && value.Name.Space != "http://www.w3.org/2000/svg") || (depth == 0 && (value.Name.Local != "svg" || elements > 0)) {
				return false
			}
			elements++
			depth++
			if depth > 32 || elements > 4096 {
				return false
			}
			for _, attribute := range value.Attr {
				name, text := strings.ToLower(attribute.Name.Local), strings.ToLower(attribute.Value)
				if strings.HasPrefix(name, "on") || name == "style" || name == "href" || name == "src" || strings.Contains(text, "\\") || (attribute.Name.Space != "" && attribute.Name.Space != "xmlns") {
					return false
				}
				if strings.Contains(text, "url(") && !(strings.HasPrefix(text, "url(#") && strings.HasSuffix(text, ")") && strings.Count(text, "url(") == 1 && !strings.ContainsAny(text, "\\\"'\n\r")) {
					return false
				}
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			return false
		case xml.ProcInst:
			// XML 编码声明不加载资源；其他处理指令（包括样式表）不能进入图标。
			if value.Target != "xml" || depth != 0 || elements != 0 {
				return false
			}
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(value)) != 0 {
				return false
			}
		}
	}
}

// ReadSkill 返回版本已发现技能的正文，不接受任意文件路径。
func ReadSkill(root string, pkg Package, key string) (Skill, string, error) {
	var err error
	root, err = PackageRoot(root, pkg)
	if err != nil {
		return Skill{}, "", err
	}
	for _, skill := range pkg.Skills {
		if skill.Key != key {
			continue
		}
		content, err := readBounded(root, filepath.Join(skill.Path, "SKILL.md"), maxMetadataBytes)
		if err != nil {
			return Skill{}, "", err
		}
		if !utf8.Valid(content) || bytes.ContainsRune(content, 0) {
			return Skill{}, "", status_error.PluginContentInvalid
		}
		return skill, string(content), nil
	}
	return Skill{}, "", status_error.PluginContentInvalid
}

// ReadSkillResources 只暴露已发现技能目录的文件，根技能过滤插件清单与其他组件。
func ReadSkillResources(root string, pkg Package, key, requested string) ([]packagefiles.ResourceEntry, *packagefiles.ResourceFile, error) {
	var err error
	root, err = PackageRoot(root, pkg)
	if err != nil {
		return nil, nil, err
	}
	for _, skill := range pkg.Skills {
		if skill.Key != key {
			continue
		}
		directory, err := contained(root, skill.Path)
		if err != nil {
			return nil, nil, status_error.PluginContentInvalid
		}
		excluded := []string{".git"}
		if filepath.Clean(skill.Path) == "." {
			excluded = append(excluded, ".codex-plugin", ".claude-plugin", "plugin.json", "mcp.json", ".mcp.json", ".app.json", "provenance.json", "README.md", "readme.md", "skills", "hooks", "agents", "commands")
			for _, other := range pkg.Skills {
				if other.Key != skill.Key && filepath.Clean(other.Path) != "." {
					excluded = append(excluded, filepath.Clean(other.Path))
				}
			}
		}
		resources, err := pluginArchive.Resources(directory, excluded)
		if err != nil {
			return nil, nil, err
		}
		if requested == "" {
			return resources, nil, nil
		}
		file, err := pluginArchive.ReadResource(directory, requested, resources)
		if err != nil {
			return nil, nil, err
		}
		return resources, &file, nil
	}
	return nil, nil, status_error.PluginContentInvalid
}

// SetLocalization 校验外部本地化展示；身份与组件始终取正式清单。
func (p *Presentation) SetLocalization(names, descriptions map[string]string) error {
	for _, fields := range []map[string]string{names, descriptions} {
		if len(fields) > 2 {
			return status_error.PluginContentInvalid
		}
		for locale, value := range fields {
			if (locale != constants.PluginLocaleEnglish && locale != constants.PluginLocaleChinese) || len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
				return status_error.PluginContentInvalid
			}
		}
	}
	p.DisplayNames = names
	p.LocalizedDescriptions = descriptions
	return nil
}

// ReadmeLocale 将展示语言收敛到受支持的两种固定文档。
func ReadmeLocale(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return constants.PluginLocaleChinese
	}
	return constants.PluginLocaleEnglish
}

func readmeFilename(root, locale string) string {
	filenames := []string{"README.md", "readme.md"}
	if ReadmeLocale(locale) == constants.PluginLocaleChinese {
		filenames = append([]string{"README.zh-CN.md"}, filenames...)
	}
	for _, filename := range filenames {
		if _, err := os.Lstat(filepath.Join(root, filename)); !os.IsNotExist(err) {
			return filename
		}
	}
	return ""
}

func (p *Package) loadReadme(root, locale string) {
	p.Readme = ""
	p.ReadmePath = readmeFilename(root, locale)
	p.ReadmeStatus = "missing"
	if p.ReadmePath == "" {
		return
	}
	content, err := readBounded(root, p.ReadmePath, maxReadmeBytes)
	if err == nil && utf8.Valid(content) && !bytes.ContainsRune(content, 0) {
		p.Readme = string(content)
		p.ReadmeStatus = "available"
		if len(content) == 0 {
			p.ReadmeStatus = "empty"
		}
		return
	}
	p.ReadmeStatus = "invalid"
	if err == status_error.PluginContentLimitExceeded {
		p.ReadmeStatus = "too-large"
	}
	p.diagnose(p.ReadmePath, "invalid_documentation")
}

// LocalizeReadme 只从当前完整快照内的固定文件名读取所选语言，缺失时回退英文。
func LocalizeReadme(source string, pkg Package, locale string) (Package, error) {
	root, err := PackageRoot(source, pkg)
	if err != nil {
		return pkg, err
	}
	pkg.loadReadme(root, locale)
	return pkg, nil
}

// ReadRawReadme 用同一语言选择返回有界完整原文，不接受请求提供文件路径。
func ReadRawReadme(source string, pkg Package, locale string) (string, error) {
	root, err := PackageRoot(source, pkg)
	if err != nil {
		return "", err
	}
	filename := readmeFilename(root, locale)
	if filename == "" {
		return "", status_error.PluginContentInvalid
	}
	content, err := readBounded(root, filename, 100*1024*1024)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(content) || bytes.ContainsRune(content, 0) {
		return "", status_error.PluginContentInvalid
	}
	return string(content), nil
}
