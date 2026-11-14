package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestPluginUpdatesCompareActualInstallationsAndExactSource(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := global.PluginCatalogSource()
	writeUpdateCatalog(t, root, "2.0.0")
	store := global.PluginControllerManager
	for _, id := range []string{"owned", "foreign"} {
		source := catalog.SourceKey(root, "tools")
		if id == "foreign" {
			source = catalog.SourceKey(t.TempDir(), "tools")
		}
		if err := store.Create(ctx, models.Plugin{ID: id, Name: "Tools", SourceKey: source}); err != nil {
			t.Fatal(err)
		}
		for i, version := range []string{"1.0.0", "2.0.0", "3.0.0"} {
			_, err := store.PutVersion(ctx, models.PluginVersion{ID: id + version, PluginID: id, Version: version, CreatedAt: int64(i + 1), Package: packageplugin.Package{Manifest: packageplugin.Manifest{Name: "tools", Version: version}}})
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, relation := range []models.WakerPlugin{{ID: id + "w1", WakerID: "w1", PluginID: id, VersionID: id + "1.0.0", Enabled: false}, {ID: id + "w2", WakerID: "w2", PluginID: id, VersionID: id + "2.0.0", Enabled: true}} {
			if err := store.Install(ctx, relation); err != nil {
				t.Fatal(err)
			}
		}
	}
	value, err := (&ListPlugins{}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, detail := range value.(ListPluginsResponse).Resources {
		if detail.ResolvedVersionID != detail.Plugin.ID+"2.0.0" {
			t.Fatalf("prepared-only version displaced installed version: %s", detail.ResolvedVersionID)
		}
		data, _ := json.Marshal(detail)
		var response struct {
			// 更新摘要
			Update PluginUpdateResponse `json:"update"`
			// 目录状态
			CatalogSource catalog.SourceState `json:"catalogSource"`
		}
		if err := json.Unmarshal(data, &response); err != nil {
			t.Fatal(err)
		}
		if detail.Plugin.ID == "foreign" {
			if response.Update.Available {
				t.Fatal("same name from another source offered update")
			}
		} else if !response.Update.Available || response.Update.CatalogKey != "tools" || response.Update.Version != "2.0.0" || response.Update.ArchiveSHA256 != strings.Repeat("b", 64) || !slices.Equal(response.Update.WakerIDs, []string{"w1"}) {
			t.Fatalf("wrong update summary: %s", data)
		}
		if response.CatalogSource.State != "local" {
			t.Fatalf("wrong catalog state: %s", data)
		}
	}
	resource, _ := store.Get(ctx, "owned")
	explicit, err := pluginDetail(ctx, resource, "owned3.0.0")
	if err != nil || explicit.ResolvedVersionID != "owned3.0.0" {
		t.Fatalf("explicit prepared version lost: %+v %v", explicit, err)
	}
}

func writeUpdateCatalog(t *testing.T, root, version string) {
	t.Helper()
	body := fmt.Sprintf(`{"schemaVersion":1,"entries":[{"key":"tools","name":"tools","version":%q,"format":"agent-plugins","detailsURL":"https://invalid.example/details.json","detailsSHA256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","detailsSize":100,"sourceCommit":%q,"archiveURL":"https://invalid.example/never-download.zip","archiveSHA256":%q,"packagePath":"entry"}]}`, version, strings.Repeat("a", 40), strings.Repeat("b", 64))
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPluginUpdateReadDoesNotContactColdRemoteCatalog(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	ctx := pluginAPIContext(t)
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer source.Close()
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", source.URL+"/catalog.json")
	store := global.PluginControllerManager
	if err := store.Create(ctx, models.Plugin{ID: "p", Name: "Tools", SourceKey: catalog.SourceKey(global.PluginCatalogSource(), "tools")}); err != nil {
		t.Fatal(err)
	}
	_, err := store.PutVersion(ctx, models.PluginVersion{ID: "old", PluginID: "p", Version: "1.0.0", Package: packageplugin.Package{}})
	if err != nil {
		t.Fatal(err)
	}
	resource, _ := store.Get(ctx, "p")
	detail, err := pluginDetail(context.Background(), resource)
	if err != nil {
		t.Fatal(err)
	}
	if detail.CatalogSource.State != "unavailable" || requests.Load() != 0 {
		t.Fatalf("cold source read must stay local: %+v requests=%d", detail.CatalogSource, requests.Load())
	}
}

func TestPrepareUpdateRejectsFinalRecoveredArchiveChange(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	ctx := pluginAPIContext(t)
	first, second := recoveryArchive(t, "1.0.0", "first"), recoveryArchive(t, "2.0.0", "second")
	sums := [][32]byte{sha256.Sum256(first), sha256.Sum256(second)}
	var mode atomic.Int32
	var source *httptest.Server
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := int(mode.Load())
		if r.URL.Path == "/catalog.json" {
			fmt.Fprintf(w, `{"schemaVersion":1,"entries":[{"key":"recovery","name":"recovery","version":"%d.0.0","format":"agent-plugins","detailsURL":"https://invalid.example/details.json","detailsSHA256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","detailsSize":100,"sourceCommit":%q,"archiveURL":%q,"archiveSHA256":"%x","packagePath":"entry"}]}`, current+1, strings.Repeat("a", 40), source.URL+"/owned.zip", sums[current])
			return
		}
		_, _ = w.Write([][]byte{first, second}[current])
	}))
	defer source.Close()
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", source.URL+"/catalog.json")
	entry, err := catalog.Find(global.PluginCatalogSource(), "recovery")
	if err != nil {
		t.Fatal(err)
	}
	mode.Store(1)
	var body PrepareCatalogPluginRequest
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"expectedArchiveSHA256":%q}`, entry.ArchiveSHA256)), &body); err != nil {
		t.Fatal(err)
	}
	if _, err := (&PrepareCatalogPlugin{Key: "recovery", Body: body}).Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("confirmed archive silently changed during recovery: %v", err)
	}
	value, err := (&ListPlugins{}).Output(ctx)
	if err != nil || value.(ListPluginsResponse).TotalSize != 0 {
		t.Fatalf("conflicting preparation registered plugin: %+v %v", value, err)
	}
}

func TestUpdateAllRequiresCompleteInstallationSetAndPreservesEnabled(t *testing.T) {
	ctx := pluginAPIContext(t)
	prepare := func(version string) PluginDetailResponse {
		root := t.TempDir()
		body := fmt.Sprintf(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"tools","version":%q,"extensions":{"com.neuxbot":{"schemaVersion":1,"configuration":{"fields":[{"key":"MODE"},{"key":"TOKEN","secret":true}]}}}}`, version)
		if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		stage, err := packageplugin.Stage(stagedRoot(), root)
		if err != nil {
			t.Fatal(err)
		}
		detail, err := prepareCatalogFixture(t, ctx, stage, "", false)
		if err != nil {
			t.Fatal(err)
		}
		return detail
	}
	old := prepare("1.0.0")
	next := prepare("2.0.0")
	store := global.PluginControllerManager
	for _, relation := range []models.WakerPlugin{{ID: "r1", WakerID: "w1", PluginID: old.Plugin.ID, VersionID: old.ResolvedVersionID, Enabled: false}, {ID: "r2", WakerID: "w2", PluginID: old.Plugin.ID, VersionID: next.ResolvedVersionID, Enabled: true}} {
		if err := store.Install(ctx, relation); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpdateConfiguration(ctx, models.PluginConfiguration{PluginID: old.Plugin.ID, Values: map[string]string{"MODE": "existing"}, Secrets: []models.MCPSecretDefinition{{Name: "TOKEN", Configured: true, Value: "private-value"}}}, 0); err != nil {
		t.Fatal(err)
	}
	first := VersionSwitch{WakerID: "w1", ExpectedVersionID: old.ResolvedVersionID, VersionID: next.ResolvedVersionID}
	second := VersionSwitch{WakerID: "w2", ExpectedVersionID: next.ResolvedVersionID, VersionID: next.ResolvedVersionID}
	for _, switches := range [][]VersionSwitch{
		{first}, {first, second, {WakerID: "removed", ExpectedVersionID: old.ResolvedVersionID, VersionID: next.ResolvedVersionID}},
		{first, second, second},
		{first, {WakerID: "w2", ExpectedVersionID: old.ResolvedVersionID, VersionID: next.ResolvedVersionID}},
		{first, {WakerID: "w2", ExpectedVersionID: next.ResolvedVersionID, VersionID: old.ResolvedVersionID}},
	} {
		body := UpdatePluginInstallationsRequest{ExpectedRevision: 1, Switches: switches}
		_, err := (&UpdatePluginInstallations{PluginID: old.Plugin.ID, Body: body}).Output(ctx)
		if !errors.Is(err, status_error.PluginVersionConflict) {
			t.Fatalf("incomplete/conflicting update accepted: %+v err=%v", switches, err)
		}
		config, _ := store.Configuration(ctx, old.Plugin.ID)
		relations, _ := store.Installations(ctx, old.Plugin.ID, "")
		if config.Revision != 1 || relations[0].VersionID != old.ResolvedVersionID || relations[0].Enabled {
			t.Fatal("failed update changed configuration or installation")
		}
	}
	body := UpdatePluginInstallationsRequest{ExpectedRevision: 1, Switches: []VersionSwitch{first, second}}
	if _, err := (&UpdatePluginInstallations{PluginID: old.Plugin.ID, WakerID: "w1", Body: body}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	config, err := store.Configuration(ctx, old.Plugin.ID)
	if err != nil || config.Revision != 2 || config.Values["MODE"] != "existing" || len(config.Secrets) != 1 || config.Secrets[0].Value != "private-value" {
		t.Fatalf("update lost shared configuration: %+v %v", config, err)
	}
	relations, _ := store.Installations(ctx, old.Plugin.ID, "")
	if len(relations) != 2 || relations[0].VersionID != next.ResolvedVersionID || relations[1].VersionID != next.ResolvedVersionID || relations[0].Enabled || !relations[1].Enabled {
		t.Fatalf("full update lost pin or enabled state: %+v", relations)
	}
}

func TestPreparedUpdateAndRollbackKeepExactLocalVersions(t *testing.T) {
	ctx := pluginAPIContext(t)
	prepare := func(version string) PluginDetailResponse {
		t.Helper()
		root := t.TempDir()
		body := fmt.Sprintf(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"fixed-tools","version":%q}`, version)
		if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		stage, err := packageplugin.Stage(stagedRoot(), root)
		if err != nil {
			t.Fatal(err)
		}
		detail, err := prepareCatalogFixture(t, ctx, stage, "", false)
		if err != nil {
			t.Fatal(err)
		}
		return detail
	}
	old := prepare("1.0.0")
	id := old.Plugin.ID
	install := &InstallPlugin{PluginID: id, Body: InstallPluginRequest{VersionID: old.ResolvedVersionID, WakerIDs: []string{"w1", "w2"}}}
	if _, err := install.Output(ctx); err != nil {
		t.Fatal(err)
	}
	store := global.PluginControllerManager
	if err := store.SetEnabled(ctx, "w2", id, false); err != nil {
		t.Fatal(err)
	}
	next := prepare("2.0.0")
	unused := prepare("3.0.0")
	if err := os.Remove(filepath.Join(global.PluginCatalogSource(), "catalog.json")); err != nil {
		t.Fatal(err)
	}
	switchTo := func(from, to string, revision int64, rollback bool) error {
		t.Helper()
		// 使用 HTTP 字段名固定回滚语义。
		var body UpdatePluginInstallationsRequest
		raw := fmt.Sprintf(`{"expectedRevision":%d,"rollback":%t,"switches":[{"wakerId":"w1","expectedVersionId":%q,"versionId":%q},{"wakerId":"w2","expectedVersionId":%q,"versionId":%q}]}`, revision, rollback, from, to, from, to)
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			t.Fatal(err)
		}
		_, err := (&UpdatePluginInstallations{PluginID: id, Body: body}).Output(ctx)
		return err
	}
	if err := switchTo(old.ResolvedVersionID, next.ResolvedVersionID, 0, false); err != nil {
		t.Fatal("prepared version required Latest again", err)
	}
	value, err := (&GetPlugin{PluginID: id, WakerID: "w2"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rollback := value.(PluginDetailResponse).Rollback; rollback == nil || !rollback.Available || rollback.VersionID != old.ResolvedVersionID {
		t.Fatalf("disabled partner lost the rollback offer: %+v", rollback)
	}
	if err := switchTo(next.ResolvedVersionID, old.ResolvedVersionID, 1, false); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("normal update accepted a downgrade: %v", err)
	}
	if err := switchTo(next.ResolvedVersionID, unused.ResolvedVersionID, 1, true); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("rollback accepted a prepared-only version: %v", err)
	}
	if err := switchTo(next.ResolvedVersionID, old.ResolvedVersionID, 1, true); err != nil {
		t.Fatal("rollback required network or failed to use the previous installed version", err)
	}
	if source, err := store.Get(ctx, id); err != nil || source.RollbackVersionID != "" {
		t.Fatalf("rollback left a reversible toggle: %+v %v", source, err)
	}
	relations, err := store.Installations(ctx, id, "")
	if err != nil || len(relations) != 2 || relations[0].VersionID != old.ResolvedVersionID || !relations[0].Enabled || relations[1].VersionID != old.ResolvedVersionID || relations[1].Enabled {
		t.Fatalf("rollback lost installation facts: %+v %v", relations, err)
	}
}

func TestPluginCollectionKeepsInstalledAndRollbackContent(t *testing.T) {
	ctx := pluginAPIContext(t)
	store := global.PluginControllerManager
	id := "retention"
	if err := store.Create(ctx, models.Plugin{ID: id, Name: "Retention", SourceKey: "catalog:retention"}); err != nil {
		t.Fatal(err)
	}
	versions := []string{"installed", "rollback", "prepared", "expired", "legacy"}
	paths := map[string]string{}
	now := time.Now()
	for index, key := range versions {
		source := t.TempDir()
		version := fmt.Sprintf("%d.0.0", index+1)
		manifest := fmt.Sprintf(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"retention","version":%q}`, version)
		if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(manifest), 0600); err != nil {
			t.Fatal(err)
		}
		stage, err := packageplugin.Stage(stagedRoot(), source)
		if err != nil {
			t.Fatal(err)
		}
		path, err := packageplugin.CopyContent(filepath.Join(global.Config.DataDirectory, "plugins", "content"), stage.Root, stage.Digest)
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(global.Config.DataDirectory, path)
		if err != nil {
			t.Fatal(err)
		}
		expiry := now.Add(-time.Minute).UnixMilli()
		if key == "prepared" {
			expiry = now.Add(time.Hour).UnixMilli()
		}
		if key == "legacy" {
			expiry = 0
		}
		record := models.PluginVersion{ID: key, PluginID: id, Version: version, Digest: stage.Digest, ContentPath: filepath.ToSlash(relative), Package: stage.Package, PreparedUntil: expiry, CreatedAt: now.Add(-time.Hour).UnixMilli()}
		if _, err := store.PutVersion(ctx, record); err != nil {
			t.Fatal(err)
		}
		paths[key] = path
	}
	if err := store.Install(ctx, models.WakerPlugin{ID: "retained", PluginID: id, WakerID: "w1", VersionID: "installed", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRollbackVersion(ctx, id, "rollback"); err != nil {
		t.Fatal(err)
	}
	contents := packageplugin.NewContentStore(global.Config.DataDirectory, func() ([]string, error) { return store.ContentReferences(ctx) })
	release, err := contents.Acquire(paths["expired"])
	if err != nil {
		t.Fatal(err)
	}
	collect := func() error {
		return contents.CollectAfter(func(active []string) error { return store.RetireUnusedVersions(ctx, active) })
	}
	if err := collect(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"installed", "rollback", "prepared", "expired"} {
		if _, err := os.Stat(paths[key]); err != nil {
			t.Fatalf("lost pinned %s: %v", key, err)
		}
	}
	release()
	if err := collect(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"expired", "legacy"} {
		if _, err := os.Stat(paths[key]); !os.IsNotExist(err) {
			t.Fatalf("unreferenced %s content was retained: %v", key, err)
		}
		record, err := store.Version(ctx, key)
		if err != nil || !record.Retired || record.ContentPath != "" || record.Digest == "" {
			t.Fatalf("lost immutable identity of %s: %+v %v", key, record, err)
		}
	}
	states, err := store.ReadInstalledStates(ctx, []string{id})
	if err != nil || len(states[id].Versions) != 1 || states[id].Versions[0].ID != "installed" {
		t.Fatalf("partner pages loaded unrelated versions: %+v %v", states[id].Versions, err)
	}
	if err := store.Install(ctx, models.WakerPlugin{ID: "second", PluginID: id, WakerID: "w2", VersionID: "installed", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Uninstall(ctx, "w2", id); err != nil {
		t.Fatal(err)
	}
	if resource, err := store.Get(ctx, id); err != nil || resource.RollbackVersionID != "rollback" {
		t.Fatalf("uninstall cleared rollback while another installation remained: %+v %v", resource, err)
	}
	if err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error { return store.DeleteByWaker(txctx, "w1") }); err != nil {
		t.Fatal(err)
	}
	if err := collect(); err != nil {
		t.Fatal(err)
	}
	if resource, err := store.Get(ctx, id); err != nil || resource.RollbackVersionID != "" {
		t.Fatalf("last removal retained an unusable rollback target: %+v %v", resource, err)
	}
	if _, err := os.Stat(paths["rollback"]); !os.IsNotExist(err) {
		t.Fatalf("expired rollback content survived the last uninstall: %v", err)
	}
}

func TestPluginUpdateSemanticOrderingAndCatalogRollback(t *testing.T) {
	for _, test := range []struct {
		// 目录目标
		Target string
		// 已安装版本
		Current string
		// 预期更新
		Available bool
	}{
		{"1.10.0", "1.9.0", true}, {"v2.0.0", "1.9.0", true},
		{"2.0.0-rc.1", "2.0.0", false}, {"2.0.0", "2.0.0-rc.1", true},
		{"2.0.0+new", "2.0.0+old", false}, {"1.0.0", "2.0.0", false},
		{"invalid", "1.0.0", false}, {"2.0.0", "invalid", false},
	} {
		t.Run(test.Target+"/"+test.Current, func(t *testing.T) {
			detail := PluginDetailResponse{Plugin: models.Plugin{SourceKey: "catalog:tools"}, Versions: []PluginVersionResponse{{ID: "v", Version: test.Current}}, Installations: []models.WakerPlugin{{WakerID: "w1", VersionID: "v"}}}
			entries := []catalog.Entry{{SourceKey: "catalog:tools", Key: "tools", ArchiveSHA256: strings.Repeat("b", 64), Package: packageplugin.Package{Manifest: packageplugin.Manifest{Version: test.Target}}}}
			applyPluginUpdate(&detail, entries, catalog.SourceState{State: "available"})
			if detail.Update.Available != test.Available {
				t.Fatalf("wrong semantic order: %+v", detail.Update)
			}
		})
	}
	detail := PluginDetailResponse{Plugin: models.Plugin{SourceKey: "catalog:tools"}, Versions: []PluginVersionResponse{{ID: "old", Version: "1.0.0"}, {ID: "ahead", Version: "3.0.0"}}, Installations: []models.WakerPlugin{{WakerID: "w1", VersionID: "old"}, {WakerID: "w2", VersionID: "ahead"}}}
	entries := []catalog.Entry{{SourceKey: "catalog:tools", Key: "tools", Package: packageplugin.Package{Manifest: packageplugin.Manifest{Version: "2.0.0"}}}}
	applyPluginUpdate(&detail, entries, catalog.SourceState{State: "available"})
	if detail.Update.Available {
		t.Fatal("mixed rollback offered a full update that would downgrade another partner")
	}
}

func TestRefreshCatalogReturnsBeforeUpstreamAndCoreOwnsCancellation(t *testing.T) {
	ctx := pluginAPIContext(t)
	entered, cancelled := make(chan struct{}, 4), make(chan struct{}, 4)
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		entered <- struct{}{}
		<-r.Context().Done()
		cancelled <- struct{}{}
	}))
	defer source.Close()
	location := source.URL + "/catalog.json"
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", location)
	stop := catalog.Start(context.Background(), location, t.TempDir(), nil)
	defer stop()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("startup did not reach upstream")
	}
	request, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx.Request = ctx.Request.WithContext(request)
	done := make(chan error, 1)
	go func() { _, err := (&RefreshPluginCatalog{}).Output(ctx); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		cancel()
		t.Fatal("refresh waited for the existing network synchronization")
	}
	cancel()
	for range 5 {
		if _, err := (&RefreshPluginCatalog{}).Output(ctx); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-cancelled:
		t.Fatal("HTTP request cancellation stopped Core background sync")
	default:
	}
	if requests.Load() != 1 {
		t.Fatal("refresh duplicated startup synchronization")
	}
	stop()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Core close did not cancel upstream")
	}
	if _, err := (&RefreshPluginCatalog{}).Output(ctx); !errors.Is(err, status_error.PluginDistributionUnavailable) {
		t.Fatal("closed Core accepted refresh", err)
	}
}
