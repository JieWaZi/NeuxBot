package plugins

import (
	"os"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
)

// TestDevelopmentPluginArtifacts 以显式本机真实构建工件验证跨仓库契约，不替换发布工件为测试夹具。
func TestDevelopmentPluginArtifacts(t *testing.T) {
	if os.Getenv("NEUX_VERIFY_LOCAL_PLUGIN_ARTIFACTS") != "1" {
		t.Skip("enable NEUX_VERIFY_LOCAL_PLUGIN_ARTIFACTS with the local catalog launcher")
	}
	source := os.Getenv("ALLY_PLUGIN_CATALOG_URL")
	if source == "" {
		t.Fatal("ALLY_PLUGIN_CATALOG_URL is required")
	}
	ctx := pluginAPIContext(t)
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", source)
	browsed, err := (&BrowsePlugins{Locale: "zh-CN"}).Output(ctx)
	if err != nil || len(browsed.(BrowsePluginsResponse).Entries) != 3 {
		t.Fatal("real catalog browse unavailable", err)
	}
	entries, err := catalog.List(source)
	if err != nil || len(entries) != 3 {
		t.Fatalf("real three-package catalog unavailable: entries=%d err=%v", len(entries), err)
	}
	for _, entry := range entries {
		t.Run(entry.Key, func(t *testing.T) {
			public, err := (&GetCatalogPlugin{Key: entry.Key, Locale: "zh-CN"}).Output(ctx)
			if err != nil {
				t.Fatal("real public details rejected", err)
			}
			pkg := public.(PluginCatalogEntryResponse).Package
			if pkg.ContractVersion != constants.PluginTargetContractVersion || pkg.Readme == "" {
				t.Fatal("real details lost contract or README")
			}
			if len(pkg.Skills) > 0 {
				if _, err := (&GetCatalogPluginSkill{Key: entry.Key, SkillKey: pkg.Skills[0].Key, SourceCommit: entry.SourceCommit, ArchiveSHA256: entry.ArchiveSHA256}).Output(ctx); err != nil {
					t.Fatal("real catalog skill rejected", err)
				}
			}
			for _, server := range pkg.Servers {
				if _, err := (&GetCatalogPluginConnector{Key: entry.Key, ConnectorKey: server.Key, SourceCommit: entry.SourceCommit, ArchiveSHA256: entry.ArchiveSHA256}).Output(ctx); err != nil {
					t.Fatal("real public connector summary rejected", err)
				}
			}
			value, err := (&PrepareCatalogPlugin{Key: entry.Key, Body: PrepareCatalogPluginRequest{TrustConfirmed: true, ExpectedArchiveSHA256: entry.ArchiveSHA256}}).Output(ctx)
			if err != nil {
				t.Fatal("real archive preparation rejected", err)
			}
			detail := value.(PluginDetailResponse)
			if detail.ResolvedVersionID == "" || len(detail.Installations) != 0 {
				t.Fatalf("preparation created installation or unsupported version: %+v", detail)
			}
			managed, err := (&GetPlugin{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID, Locale: "zh-CN"}).Output(ctx)
			if err != nil || managed.(PluginDetailResponse).ResolvedVersionID != detail.ResolvedVersionID {
				t.Fatal("prepared real version cannot be managed", err)
			}
			for _, server := range pkg.Servers {
				if _, err := (&GetPluginConnector{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID, Key: server.Key}).Output(ctx); err != nil {
					t.Fatal("fixed real connector unreadable", err)
				}
			}
			if len(pkg.Skills) > 0 {
				value, err := (&GetPluginSkill{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID, Key: pkg.Skills[0].Key}).Output(ctx)
				if err != nil || value.(PluginSkillResponse).Content == "" {
					t.Fatal("fixed real skill unreadable", err)
				}
			}
			versions, err := global.PluginControllerManager.Versions(ctx, detail.Plugin.ID)
			if err != nil || len(versions) != 1 || versions[0].Package.Manifest.Version != entry.Package.Manifest.Version {
				t.Fatal("prepared version identity changed", err)
			}
			t.Logf("validated real package %s %s: %d skills, %d MCP servers", entry.Key, pkg.Manifest.Version, len(pkg.Skills), len(pkg.Servers))
		})
	}
}
