package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
)

func browseFixture(t *testing.T, ctx *gin.Context, id, key string, installed bool) {
	t.Helper()
	store := global.PluginControllerManager
	if err := store.Create(ctx, models.Plugin{ID: id, Name: "Authored " + id, Description: "Resource " + id, SourceKey: catalog.SourceKey(global.PluginCatalogSource(), key), UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	version := models.PluginVersion{ID: id + "-old", PluginID: id, Version: "1.0.0", CreatedAt: 1, Digest: strings.Repeat("a", 64), Package: packageplugin.Package{Manifest: packageplugin.Manifest{Name: "Authored " + id, Description: "Installed " + id}, Presentation: packageplugin.Presentation{Icons: packageplugin.ThemeValues{Default: "default-" + id, Dark: "dark-" + id}, Category: "coding", DisplayNames: map[string]string{"en": "Authored " + id, "zh-CN": "安装" + id}, LocalizedDescriptions: map[string]string{"en": "Installed " + id, "zh-CN": "安装简介" + id}}}}
	if _, err := store.PutVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
	if installed {
		if err := store.Install(ctx, models.WakerPlugin{ID: id + "-relation", WakerID: "w1", PluginID: id, VersionID: version.ID, Enabled: false}); err != nil {
			t.Fatal(err)
		}
	}
}

func browseCatalog(t *testing.T, keys ...string) {
	t.Helper()
	entries := []map[string]any{}
	for _, key := range keys {
		entries = append(entries, map[string]any{"key": key, "name": "Catalog " + key, "description": "Catalog description " + key, "version": "2.0.0", "category": "documents", "format": constants.PluginFormatPortable, "detailsURL": "https://invalid.example/details.json", "detailsSHA256": strings.Repeat("d", 64), "detailsSize": 100, "sourceCommit": strings.Repeat("b", 40), "archiveSHA256": strings.Repeat("c", 64), "archiveURL": "https://invalid.example/never-download.zip", "packagePath": "entry", "displayNames": map[string]string{"en": "Catalog " + key, "zh-CN": "目录" + key}})
	}
	body, err := json.Marshal(map[string]any{"schemaVersion": constants.PluginCatalogSchemaVersion, "entries": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global.PluginCatalogSource(), "catalog.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
}

func readBrowse(t *testing.T, ctx *gin.Context, request BrowsePlugins) BrowsePluginsResponse {
	t.Helper()
	value, err := request.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return value.(BrowsePluginsResponse)
}

func TestBrowsePluginsMergesBeforeSearchingCountingAndPaging(t *testing.T) {
	ctx := pluginAPIContext(t)
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	browseCatalog(t, keys...)
	for _, id := range []string{"a", "b", "removed"} {
		browseFixture(t, ctx, id, id, true)
	}
	browseFixture(t, ctx, "prepared", "c", false)
	seen := map[string]bool{}
	for page := 1; page <= 5; page++ {
		result := readBrowse(t, ctx, BrowsePlugins{PageNum: page, PageSize: 3, Locale: "zh-CN"})
		if result.TotalSize == nil || *result.TotalSize != 13 || result.InstalledTotalSize != 3 {
			t.Fatalf("wrong merged totals: %+v", result)
		}
		if result.HasMore != (page < 5) {
			t.Fatalf("wrong remaining window on page %d", page)
		}
		for _, entry := range result.Entries {
			if seen[entry.SourceKey] {
				t.Fatalf("duplicate source on page %d: %s", page, entry.SourceKey)
			}
			seen[entry.SourceKey] = true
			if page == 1 && (entry.PluginID == "" || entry.InstalledCount != 1 || entry.Category != "coding" || entry.Icons != (packageplugin.ThemeValues{Default: "default-" + entry.PluginID, Dark: "dark-" + entry.PluginID}) || !strings.HasPrefix(entry.Name, "安装")) {
				t.Fatalf("installed segment lost authoritative identity: %+v", entry)
			}
			if entry.CatalogKey == "c" && (entry.Category != "documents" || entry.PluginID != "" || entry.VersionID != "" || entry.InstalledCount != 0) {
				t.Fatal("preparation was treated as installation", entry)
			}
		}
	}
	if len(seen) != 13 {
		t.Fatal("later installed or catalog rows were lost", len(seen))
	}
	// 目录中的已安装来源必须先排除；不能因安装资料不命中搜索而重新作为未安装项出现。
	result := readBrowse(t, ctx, BrowsePlugins{Query: "Catalog a"})
	if result.TotalSize == nil || *result.TotalSize != 0 || len(result.Entries) != 0 {
		t.Fatal("catalog alias resurrected an installed source", result)
	}
	result = readBrowse(t, ctx, BrowsePlugins{Query: "安装简介a", Locale: "zh-CN"})
	if result.InstalledTotalSize != 1 || len(result.Entries) != 1 || result.Entries[0].PluginID != "a" {
		t.Fatal("installed localized search was not counted before paging", result)
	}
	result = readBrowse(t, ctx, BrowsePlugins{PageNum: 1, PageSize: 4})
	if len(result.Entries) != 4 || result.Entries[2].PluginID != "removed" || result.Entries[3].CatalogKey != "c" {
		t.Fatal("cross-segment page was not filled", result)
	}
}

func TestBrowsePluginsKeepsActualVersionsAndUpdatesAcrossAllInstallations(t *testing.T) {
	ctx := pluginAPIContext(t)
	browseCatalog(t, "tools")
	browseFixture(t, ctx, "p", "tools", true)
	store := global.PluginControllerManager
	for i, version := range []string{"2.0.0", "3.0.0"} {
		if _, err := store.PutVersion(ctx, models.PluginVersion{ID: fmt.Sprint("p-new-", i), PluginID: "p", Version: version, CreatedAt: int64(i + 2), Package: packageplugin.Package{Manifest: packageplugin.Manifest{Name: "New " + version}, Presentation: packageplugin.Presentation{Category: "research"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Install(ctx, models.WakerPlugin{ID: "p-second", WakerID: "w2", PluginID: "p", VersionID: "p-new-0", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	result := readBrowse(t, ctx, BrowsePlugins{InstalledOnly: true})
	entry := result.Entries[0]
	if entry.VersionID != "p-new-0" || entry.Category != "research" || entry.InstalledCount != 2 || !entry.Update.Available || entry.Update.Version != "2.0.0" {
		t.Fatal("prepared-only version displaced a real installation", entry)
	}
	if err := store.Update(ctx, "p", "我的工具", "edited", nil); err != nil {
		t.Fatal(err)
	}
	result = readBrowse(t, ctx, BrowsePlugins{InstalledOnly: true, Locale: "en"})
	if result.Entries[0].Name != "我的工具" {
		t.Fatal("localization overwrote a user rename", result.Entries[0])
	}
	if err := store.SwitchVersion(ctx, "w2", "p", "p-new-0", "p-new-1"); err != nil {
		t.Fatal(err)
	}
	result = readBrowse(t, ctx, BrowsePlugins{InstalledOnly: true})
	if result.Entries[0].Update.Available {
		t.Fatal("catalog rollback offered an update that downgrades another partner")
	}
}

func TestBrowsePluginsSeparatesSourcesWithoutMergingNames(t *testing.T) {
	ctx := pluginAPIContext(t)
	browseCatalog(t, "tools")
	browseFixture(t, ctx, "global", "tools", true)
	store := global.PluginControllerManager
	for _, resource := range []models.Plugin{
		{ID: "foreign", Name: "Same name", SourceKey: catalog.SourceKey(t.TempDir(), "tools")},
	} {
		if err := store.Create(ctx, resource); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PutVersion(ctx, models.PluginVersion{ID: resource.ID + "-v", PluginID: resource.ID, Version: "1.0.0", Package: packageplugin.Package{Manifest: packageplugin.Manifest{Name: "Same name"}}}); err != nil {
			t.Fatal(err)
		}
		if err := store.Install(ctx, models.WakerPlugin{ID: resource.ID + "-i", WakerID: "w1", PluginID: resource.ID, VersionID: resource.ID + "-v"}); err != nil {
			t.Fatal(err)
		}
	}
	result := readBrowse(t, ctx, BrowsePlugins{})
	if result.TotalSize == nil || *result.TotalSize != 2 || result.InstalledTotalSize != 2 || len(result.Entries) != 2 {
		t.Fatal("name merged different sources", result)
	}
	for _, entry := range result.Entries {
		if entry.PluginID == "foreign" && (entry.CatalogKey != "" || entry.Update.Available) {
			t.Fatal("source boundary was guessed by name", entry)
		}
	}
}

func TestBrowsePluginsKeepsInstalledRowsWhenCatalogUnavailable(t *testing.T) {
	ctx := pluginAPIContext(t)
	browseFixture(t, ctx, "offline", "offline", true)
	result := readBrowse(t, ctx, BrowsePlugins{})
	if result.CatalogSource.State != "unavailable" || result.TotalSize != nil || result.InstalledTotalSize != 1 || len(result.Entries) != 1 {
		t.Fatal("missing catalog hid installed rows or invented a complete count", result)
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"totalSize"`) {
		t.Fatal("unknown total must be absent to match generated optional field", string(body))
	}
	for _, forbidden := range []string{"configuration", "secrets", "versions", "installations", "partnerNames", "command", "args"} {
		if strings.Contains(string(body), `"`+forbidden+`"`) {
			t.Fatalf("management field %s leaked: %s", forbidden, body)
		}
	}
	result = readBrowse(t, ctx, BrowsePlugins{InstalledOnly: true})
	if result.TotalSize == nil || *result.TotalSize != 1 {
		t.Fatal("installed-only count depends on catalog availability", result)
	}
	browseCatalog(t)
	if err := global.PluginControllerManager.Uninstall(ctx, "w1", "offline"); err != nil {
		t.Fatal(err)
	}
	result = readBrowse(t, ctx, BrowsePlugins{})
	if result.TotalSize == nil || *result.TotalSize != 0 || len(result.Entries) != 0 || result.HasMore {
		t.Fatal("empty known catalog did not report a real zero", result)
	}
	body, err = json.Marshal(result)
	if err != nil || !strings.Contains(string(body), `"totalSize":0`) {
		t.Fatal("known zero must remain present in the generated contract", string(body), err)
	}
}

func TestBrowsePluginsRejectsInvalidWindowsAndLocales(t *testing.T) {
	ctx := pluginAPIContext(t)
	for _, request := range []BrowsePlugins{{PageNum: -1}, {PageSize: -1}, {PageSize: 101}, {Locale: "fr"}, {PageNum: int(^uint(0) >> 1), PageSize: 100}} {
		if _, err := request.Output(ctx); err == nil {
			t.Fatalf("invalid browse request accepted: %+v", request)
		}
	}
}

func TestBrowsePluginsUsesStaleSnapshotWithoutMakingNetworkRequests(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	ctx := pluginAPIContext(t)
	transport := &browseCatalogTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", fmt.Sprintf("https://browse.invalid/%p/catalog.json", t))
	browseFixture(t, ctx, "local", "local", true)
	result := readBrowse(t, ctx, BrowsePlugins{})
	if result.TotalSize != nil || len(result.Entries) != 1 || transport.requests.Load() != 0 {
		t.Fatal("cold browse contacted distribution or hid installation", result, transport.requests.Load())
	}
	if err := catalog.Refresh(ctx, global.PluginCatalogSource()); err != nil {
		t.Fatal(err)
	}
	transport.failed.Store(true)
	if err := catalog.Refresh(ctx, global.PluginCatalogSource()); err == nil {
		t.Fatal("failed refresh was not reported")
	}
	reads := transport.requests.Load()
	result = readBrowse(t, ctx, BrowsePlugins{})
	if result.CatalogSource.State != "stale" || result.CatalogSource.ErrorCode == "" || result.CatalogSource.FetchedAt == 0 || result.TotalSize == nil || *result.TotalSize != 2 || len(result.Entries) != 2 || transport.requests.Load() != reads {
		t.Fatal("stale snapshot lost content or browse retried network", result, transport.requests.Load())
	}
}

// browseCatalogTransport 仅替代外部 HTTP，缓存、快照、错误与浏览查询仍使用真实实现。
type browseCatalogTransport struct {
	// requests 统计显式刷新与错误自动重试行为。
	requests atomic.Int32
	// failed 控制后续分发响应不可用。
	failed atomic.Bool
}

// RoundTrip 返回固定目录或真实 HTTP 失败状态，不创建本机监听器。
func (t *browseCatalogTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.requests.Add(1)
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Request: request}
	body := fmt.Sprintf(`{"schemaVersion":1,"entries":[{"key":"remote","name":"Remote","version":"2.0.0","format":"agent-plugins","detailsURL":"https://invalid.example/details.json","detailsSHA256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","detailsSize":100,"sourceCommit":%q,"archiveSHA256":%q,"archiveURL":"https://invalid.example/not-downloaded.zip","packagePath":"entry"}]}`, strings.Repeat("a", 40), strings.Repeat("b", 64))
	if t.failed.Load() {
		response.StatusCode = http.StatusServiceUnavailable
		body = ""
	}
	response.Body = io.NopCloser(strings.NewReader(body))
	return response, nil
}

// browseAccountDriver 观测浏览是否错误触发真实身份核验。
type browseAccountDriver struct {
	// calls 是显式身份核验次数。
	calls int
	// available 是下一次显式核验的身份可用性。
	available bool
}

func (d *browseAccountDriver) Status(context.Context, bool) (authorization.Account, error) {
	d.calls++
	return authorization.Account{Configured: true, Available: d.available}, nil
}

func (*browseAccountDriver) Login(context.Context, []string, func(authorization.Device) error) error {
	return nil
}

func (*browseAccountDriver) Logout(context.Context) error { return nil }

func TestBrowseAuthorizationUsesOnlyKnownMatchingCachedIdentity(t *testing.T) {
	previous := global.PluginBridges
	global.PluginBridges = commandbridge.NewRegistry(context.Background(), nil)
	t.Cleanup(func() { global.PluginBridges.Close(); global.PluginBridges = previous })
	resource := models.Plugin{ID: "p", SourceKey: "source", Name: "Plugin"}
	state := models.PluginBrowseState{InstalledCount: 1, Versions: []models.PluginBrowseVersion{{ID: "v", Digest: "digest", SourcePath: "entry", AuthenticationStrategy: "plugin-command", AuthenticationProtocol: constants.PluginAuthProtocol}}}
	read := func() PluginBrowseEntry { return installedBrowseEntry(resource, state, nil, "en") }
	if read().RequiresAuthorization {
		t.Fatal("unknown account was treated as invalid")
	}
	auth, _, err := global.PluginBridges.Get("p", "digest:entry", commandbridge.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	driver := &browseAccountDriver{}
	if err := auth.ReplaceDriver(driver); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Status(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !read().RequiresAuthorization || driver.calls != 1 {
		t.Fatal("known unavailable identity was not displayed from cache", driver.calls)
	}
	state.Versions[0].Digest = "new-digest"
	if read().RequiresAuthorization || driver.calls != 1 {
		t.Fatal("different version inherited or rechecked old authorization")
	}
	state.Versions[0].Digest = "digest"
	state.InstalledCount = 0
	if read().RequiresAuthorization {
		t.Fatal("preparation alone required reauthorization")
	}
	state.InstalledCount = 1
	driver.available = true
	if _, err := auth.Status(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if read().RequiresAuthorization || driver.calls != 2 {
		t.Fatal("available shared identity did not clear reauthorization")
	}
}
