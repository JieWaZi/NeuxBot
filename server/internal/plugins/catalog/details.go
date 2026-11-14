package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

const maxDetailsBytes = 4 * 1024 * 1024

// Details 是只读公开资料，不能成为安装或执行的包事实。
type Details struct {
	// SchemaVersion 限定公开资料协议版本。
	SchemaVersion int `json:"schemaVersion"`
	// SourceCommit 绑定目录来源提交。
	SourceCommit string `json:"sourceCommit"`
	// ArchiveSHA256 绑定完整安装包摘要。
	ArchiveSHA256 string `json:"archiveSHA256"`
	// PackagePath 绑定完整来源内的逻辑包入口。
	PackagePath string `json:"packagePath"`
	// Package 只包含经白名单核验的公开展示字段。
	Package plugins.Package `json:"package"`
	// Readmes 保存固定语言的完整公开说明。
	Readmes map[string]string `json:"readmes"`
	// SkillContents 保存已声明技能的完整 SKILL.md。
	SkillContents map[string]string `json:"skillContents"`
}

// ReadDetails 核验公开资料内容摘要和来源绑定，不读取或执行安装包。
func ReadDetails(ctx context.Context, entry Entry) (Details, error) {
	_, result, err := loadDetails(ctx, entry)
	return result, err
}

// ReadCurrentDetails 解析当前公开资料，分发失效时按同一可信目录恢复一次。
func ReadCurrentDetails(ctx context.Context, root, key string) (Entry, Details, error) {
	var result Details
	entry, _, err := resolveCurrent(ctx, root, key, true, func(entry Entry) (string, error) {
		var location string
		var err error
		location, result, err = loadDetails(ctx, entry)
		return location, err
	})
	return entry, result, err
}

func detailsLocation(ctx context.Context, entry Entry) (string, error) {
	location, _, err := loadDetails(ctx, entry)
	return location, err
}

func loadDetails(ctx context.Context, entry Entry) (string, Details, error) {
	if !httpAddress(entry.DetailsURL) || len(entry.DetailsSHA256) != 64 || entry.DetailsSize <= 0 || entry.DetailsSize > maxDetailsBytes {
		return "", Details{}, status_error.PluginContentInvalid
	}
	if _, err := hex.DecodeString(entry.DetailsSHA256); err != nil {
		return "", Details{}, status_error.PluginContentInvalid
	}
	directory := filepath.Join(entryStorageRoot(entry), "details")
	location := filepath.Join(directory, entry.DetailsSHA256+".json")
	state, release, err := artifactStates.acquire(ctx, location)
	if err != nil {
		return "", Details{}, err
	}
	defer release()
	if file, err := os.Open(location); err == nil {
		raw, readErr := io.ReadAll(io.LimitReader(file, entry.DetailsSize+1))
		_ = file.Close()
		if readErr == nil && validateDetailsBytes(raw, entry) == nil {
			result, err := parseDetails(raw, entry)
			return location, result, err
		}
	}
	if err := state.failed(entry.DetailsURL); err != nil {
		return "", Details{}, err
	}
	fail := func(err error) (string, Details, error) {
		if ctx.Err() == nil {
			state.fail(entry.DetailsURL, err)
		}
		return "", Details{}, err
	}
	client := http.Client{Timeout: 15 * time.Second}
	started := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.DetailsURL, nil)
	if err != nil {
		return "", Details{}, status_error.PluginContentInvalid
	}
	response, err := client.Do(request)
	if err != nil {
		logDistributionFailure("details_get", entry.Key, entry.DetailsURL, response, started, err)
		return fail(status_error.PluginDistributionUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		logDistributionFailure("details_status", entry.Key, entry.DetailsURL, response, started, nil)
		return fail(status_error.PluginDistributionUnavailable)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, entry.DetailsSize+1))
	if err != nil {
		logDistributionFailure("details_body", entry.Key, entry.DetailsURL, response, started, err)
		return fail(status_error.PluginDistributionUnavailable)
	}
	if err := validateDetailsBytes(raw, entry); err != nil {
		return fail(err)
	}
	result, err := parseDetails(raw, entry)
	if err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return "", Details{}, status_error.PluginDistributionUnavailable
	}
	if err := publishDetails(directory, location, raw); err != nil {
		return "", Details{}, err
	}
	state.failure = nil
	return location, result, nil
}

func publishDetails(directory, location string, raw []byte) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return status_error.PluginStorageError
	}
	temporary, err := os.CreateTemp(directory, "details-*.json")
	if err != nil {
		return status_error.PluginStorageError
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return status_error.PluginStorageError
	}
	if err := temporary.Close(); err != nil {
		return status_error.PluginStorageError
	}
	if err := os.Rename(temporary.Name(), location); err != nil {
		return status_error.PluginStorageError
	}
	return nil
}

func validateDetailsBytes(raw []byte, entry Entry) error {
	if len(raw) > maxDetailsBytes {
		return status_error.PluginContentLimitExceeded
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != entry.DetailsSize || hex.EncodeToString(sum[:]) != entry.DetailsSHA256 || !utf8.Valid(raw) {
		return status_error.PluginContentInvalid
	}
	return nil
}

func parseDetails(raw []byte, entry Entry) (Details, error) {
	var result Details
	if err := validateDetailsBytes(raw, entry); err != nil {
		return result, err
	}
	document, err := publicObject(raw, "schemaVersion", "sourceCommit", "archiveSHA256", "packagePath", "package", "readmes", "skillContents")
	if err != nil {
		return result, err
	}
	if err := validatePublicPackage(document["package"]); err != nil {
		return result, err
	}
	if json.Unmarshal(raw, &result) != nil || result.SchemaVersion != constants.PluginDetailsSchemaVersion {
		return result, status_error.PluginContentInvalid
	}
	if result.SourceCommit != entry.SourceCommit || result.ArchiveSHA256 != entry.ArchiveSHA256 || result.PackagePath != entry.PackagePath {
		return result, status_error.PluginVersionConflict
	}
	if result.Package.Manifest.Name != entry.Package.Manifest.Name || result.Package.Manifest.Version != entry.Package.Manifest.Version || result.Package.Format != constants.PluginFormatPortable {
		return result, status_error.PluginContentInvalid
	}
	if err := plugins.ValidateContract(result.Package); err != nil {
		return result, err
	}
	if err := result.Package.Presentation.Validate(); err != nil {
		return result, err
	}
	for _, icon := range []string{result.Package.Presentation.Icons.Default, result.Package.Presentation.Icons.Dark} {
		if icon != "" && plugins.SanitizeIcon(icon) != icon {
			return result, status_error.PluginContentInvalid
		}
	}
	if err := result.Package.Presentation.SetLocalization(result.Package.Presentation.DisplayNames, result.Package.Presentation.LocalizedDescriptions); err != nil {
		return result, err
	}
	if result.Package.Icon != "" && plugins.SanitizeIcon(result.Package.Icon) != result.Package.Icon {
		return result, status_error.PluginContentInvalid
	}
	for _, server := range result.Package.Servers {
		if server.Key == "" {
			return result, status_error.PluginContentInvalid
		}
		if server.Type == "stdio" {
			if server.URL != "" {
				return result, status_error.PluginContentInvalid
			}
			continue
		}
		address, err := url.Parse(server.URL)
		if err != nil || (server.Type != "streamable-http" && server.Type != "sse") || (address.Scheme != "https" && address.Scheme != "http") || address.Host == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" {
			return result, status_error.PluginContentInvalid
		}
	}
	for locale, content := range result.Readmes {
		if (locale != constants.PluginLocaleEnglish && locale != constants.PluginLocaleChinese) || !publicText(content) {
			return result, status_error.PluginContentInvalid
		}
	}
	known := map[string]bool{}
	for _, skill := range result.Package.Skills {
		content, exists := result.SkillContents[skill.Key]
		if known[skill.Key] || !safeDetailsPath(skill.Key) || skill.Path != skill.Key || skill.Name == "" || !exists || len(content) > 1024*1024 || !publicText(content) {
			return result, status_error.PluginContentInvalid
		}
		known[skill.Key] = true
	}
	if len(known) != len(result.SkillContents) {
		return result, status_error.PluginContentInvalid
	}
	return result, nil
}

func publicObject(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, status_error.PluginContentInvalid
	}
	names := map[string]bool{}
	for _, name := range allowed {
		names[name] = true
	}
	for name := range object {
		if !names[name] {
			return nil, status_error.PluginContentInvalid
		}
	}
	return object, nil
}

func validatePublicPackage(raw []byte) error {
	pkg, err := publicObject(raw, "contractVersion", "manifest", "format", "presentation", "upstream", "icon", "skills", "servers", "integration", "diagnostics")
	if err != nil {
		return err
	}
	manifest, err := publicObject(pkg["manifest"], "$schema", "name", "version", "description", "author", "homepage", "repository", "license", "keywords")
	if err != nil {
		return err
	}
	if author := manifest["author"]; len(author) != 0 {
		if _, err := publicObject(author, "name", "email", "url"); err != nil {
			return err
		}
	}
	if value := pkg["presentation"]; len(value) != 0 {
		if _, err := publicObject(value, "displayName", "shortDescription", "longDescription", "developerName", "category", "websiteURL", "privacyPolicyURL", "termsOfServiceURL", "displayNames", "localizedDescriptions", "defaultPrompt", "defaultPromptLocalizations", "capabilities", "supportURL", "brandColor", "icons", "setup"); err != nil {
			return err
		}
	}
	if value := pkg["presentation"]; len(value) > 0 {
		var presentation map[string]json.RawMessage
		_ = json.Unmarshal(value, &presentation)
		for _, name := range []string{"brandColor", "icons"} {
			if value := presentation[name]; len(value) > 0 {
				if _, err := publicObject(value, "default", "dark"); err != nil {
					return err
				}
			}
		}
		if value := presentation["setup"]; len(value) > 0 && !bytes.Equal(value, []byte("null")) {
			if _, err := publicObject(value, "instructions", "helpURL"); err != nil {
				return err
			}
		}
	}
	if value := pkg["upstream"]; len(value) != 0 && !bytes.Equal(value, []byte("null")) {
		if _, err := publicObject(value, "author", "tag", "version", "repository", "license", "commit", "distribution"); err != nil {
			return err
		}
	}
	for name, fields := range map[string][]string{"skills": {"key", "name", "description", "path"}, "servers": {"key", "type", "url", "authentication"}, "diagnostics": {"component", "code"}} {
		var items []json.RawMessage
		if json.Unmarshal(pkg[name], &items) != nil {
			return status_error.PluginContentInvalid
		}
		for _, item := range items {
			if name == "servers" {
				var server map[string]json.RawMessage
				_ = json.Unmarshal(item, &server)
				if auth := server["authentication"]; len(auth) > 0 && !bytes.Equal(auth, []byte("null")) {
					if _, err := publicObject(auth, "type", "headerScheme", "headerName"); err != nil {
						return err
					}
				}
			}
			if _, err := publicObject(item, fields...); err != nil {
				return err
			}
		}
	}
	if value := pkg["integration"]; len(value) != 0 {
		integration, err := publicObject(value, "authentication")
		if err != nil {
			return err
		}
		if auth := integration["authentication"]; len(auth) != 0 && !bytes.Equal(auth, []byte("null")) {
			if _, err := publicObject(auth, "strategy", "protocol", "preferredAction", "scopes"); err != nil {
				return err
			}
		}
	}
	return nil
}

func publicText(value string) bool { return utf8.ValidString(value) && !strings.ContainsRune(value, 0) }

func safeDetailsPath(value string) bool {
	return value != "" && !path.IsAbs(value) && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\:\x00")
}

// Readme 选择固定语言的公开原文，中文缺失时回退英文。
func (d Details) Readme(locale string) (string, string, error) {
	locale = plugins.ReadmeLocale(locale)
	if content, exists := d.Readmes[locale]; exists {
		filename := "README.md"
		if locale == constants.PluginLocaleChinese {
			filename = "README.zh-CN.md"
		}
		return content, filename, nil
	}
	if content, exists := d.Readmes[constants.PluginLocaleEnglish]; exists {
		return content, "README.md", nil
	}
	return "", "", status_error.PluginContentInvalid
}

// ViewPackage 派生说明预览，不生成执行命令、配置或安装关系。
func (d Details) ViewPackage(locale string) plugins.Package {
	pkg := d.Package
	content, filename, err := d.Readme(locale)
	pkg.ReadmeStatus = "missing"
	if err != nil {
		return pkg
	}
	pkg.ReadmePath = filename
	pkg.ReadmeStatus = "available"
	if len(content) > 1024*1024 {
		pkg.ReadmeStatus = "too-large"
		return pkg
	}
	pkg.Readme = content
	if content == "" {
		pkg.ReadmeStatus = "empty"
	}
	return pkg
}

// Skill 仅返回公开清单已发现的固定技能正文，不读取其他包内路径。
func (d Details) Skill(key string) (plugins.Skill, string, error) {
	for _, skill := range d.Package.Skills {
		if skill.Key == key {
			return skill, d.SkillContents[key], nil
		}
	}
	return plugins.Skill{}, "", status_error.PluginContentInvalid
}
