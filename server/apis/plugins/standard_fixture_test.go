package plugins

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
)

// standardFixtureTransport 提供真实摘要绑定的发布工件，外部 HTTP 使用可控替身。
type standardFixtureTransport struct {
	// previous 交回本夹具不拥有的其他请求。
	previous http.RoundTripper
	// artifacts 是本次发布的完整资料和 ZIP 字节。
	artifacts map[string][]byte
}

// RoundTrip 只返回明确登记的测试发布资料。
func (t *standardFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if body, ok := t.artifacts[request.URL.String()]; ok {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body))), Request: request}, nil
	}
	return t.previous.RoundTrip(request)
}

func writeStandardCatalogFixture(t *testing.T, root, key string, archive []byte, stage packageplugin.StagedPackage) {
	t.Helper()
	archiveDigest := fmt.Sprintf("%x", sha256.Sum256(archive))
	pkg := packageplugin.Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: stage.Package.Manifest, Presentation: stage.Package.Presentation, Icon: stage.Package.Icon, Upstream: stage.Package.Upstream, Skills: []packageplugin.Skill{}, Servers: []packageplugin.Server{}, Diagnostics: []packageplugin.Diagnostic{}}
	pkg.Manifest.Extensions = nil
	if auth := stage.Package.Integration.Authentication; auth != nil {
		pkg.Integration.Authentication = &packageplugin.Authentication{Strategy: auth.Strategy, Protocol: auth.Protocol, Scopes: auth.Scopes, PreferredAction: auth.PreferredAction}
	}
	skills := map[string]string{}
	for _, skill := range stage.Package.Skills {
		_, body, err := packageplugin.ReadSkill(stage.Root, stage.Package, skill.Key)
		if err != nil {
			t.Fatal(err)
		}
		skills[skill.Key] = body
		pkg.Skills = append(pkg.Skills, packageplugin.Skill{Key: skill.Key, Path: skill.Key, Name: skill.Name, Description: skill.Description})
	}
	for _, server := range stage.Package.Servers {
		pkg.Servers = append(pkg.Servers, packageplugin.Server{Key: server.Key, Type: server.Type, URL: server.URL})
	}
	readmes := map[string]string{}
	for _, locale := range []string{constants.PluginLocaleEnglish, constants.PluginLocaleChinese} {
		if body, err := packageplugin.ReadRawReadme(stage.Root, stage.Package, locale); err == nil {
			readmes[locale] = body
		}
	}
	if len(readmes) == 0 {
		readmes[constants.PluginLocaleEnglish] = ""
	}
	packagePath := "."
	if stage.Package.SourcePath != "" {
		packagePath = stage.Package.SourcePath
	}
	details, err := json.Marshal(catalog.Details{SchemaVersion: constants.PluginDetailsSchemaVersion, SourceCommit: strings.Repeat("a", 40), ArchiveSHA256: archiveDigest, PackagePath: packagePath, Package: pkg, Readmes: readmes, SkillContents: skills})
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("https://fixtures.invalid/%x/%s", sha256.Sum256([]byte(root)), key)
	archiveURL, detailsURL := base+".zip", base+".json"
	previous := http.DefaultTransport
	http.DefaultTransport = &standardFixtureTransport{previous: previous, artifacts: map[string][]byte{archiveURL: archive, detailsURL: details}}
	t.Cleanup(func() { http.DefaultTransport = previous })
	entries := []any{}
	if previous, err := os.ReadFile(filepath.Join(root, "catalog.json")); err == nil {
		var document map[string]any
		if json.Unmarshal(previous, &document) != nil {
			t.Fatal("invalid catalog fixture")
		}
		items, _ := document["entries"].([]any)
		for _, item := range items {
			entry, _ := item.(map[string]any)
			if entry["key"] != key {
				entries = append(entries, item)
			}
		}
	}
	entries = append(entries, map[string]any{"key": key, "name": pkg.Manifest.Name, "version": pkg.Manifest.Version, "description": pkg.Manifest.Description, "format": constants.PluginFormatPortable, "category": pkg.Presentation.Category, "sourceCommit": strings.Repeat("a", 40), "archiveSHA256": archiveDigest, "archiveURL": archiveURL, "packagePath": packagePath, "detailsURL": detailsURL, "detailsSHA256": fmt.Sprintf("%x", sha256.Sum256(details)), "detailsSize": len(details), "icon": pkg.Icon, "displayNames": pkg.Presentation.DisplayNames, "localizedDescriptions": pkg.Presentation.LocalizedDescriptions})
	body, err := json.Marshal(map[string]any{"schemaVersion": constants.PluginCatalogSchemaVersion, "entries": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
}
