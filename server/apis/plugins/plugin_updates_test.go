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

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
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
	value, err := (&ListPlugins{InstalledOnly: true}).Output(ctx)
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
	body := fmt.Sprintf(`{"schemaVersion":2,"entries":[{"key":"tools","name":"tools","version":%q,"format":"codex","sourceCommit":%q,"archiveURL":"https://invalid.example/never-download.zip","archiveSHA256":%q,"packagePath":"entry"}]}`, version, strings.Repeat("a", 40), strings.Repeat("b", 64))
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
			fmt.Fprintf(w, `{"schemaVersion":2,"entries":[{"key":"recovery","name":"recovery","version":"%d.0.0","format":"codex","sourceCommit":%q,"archiveURL":%q,"archiveSHA256":"%x","packagePath":"entry"}]}`, current+1, strings.Repeat("a", 40), source.URL+"/owned.zip", sums[current])
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
		body := fmt.Sprintf(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"tools","version":%q,"extensions":{"com.ally":{"config":[{"key":"MODE"},{"key":"TOKEN","secret":true}]}}}`, version)
		if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		stage, err := packageplugin.Stage(stagedRoot(), root)
		if err != nil {
			t.Fatal(err)
		}
		detail, err := prepareCatalogFixture(t, ctx, stage, "", "", false)
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
		body := UpdateConfigurationRequest{ExpectedRevision: 1, Switches: switches}
		_, err := (&UpdatePluginConfiguration{PluginID: old.Plugin.ID, Body: body}).Output(ctx)
		if !errors.Is(err, status_error.PluginVersionConflict) {
			t.Fatalf("incomplete/conflicting update accepted: %+v err=%v", switches, err)
		}
		config, _ := store.Configuration(ctx, old.Plugin.ID)
		relations, _ := store.Installations(ctx, old.Plugin.ID, "")
		if config.Revision != 1 || relations[0].VersionID != old.ResolvedVersionID || relations[0].Enabled {
			t.Fatal("failed update changed configuration or installation")
		}
	}
	body := UpdateConfigurationRequest{ExpectedRevision: 1, Switches: []VersionSwitch{first, second}}
	if _, err := (&UpdatePluginConfiguration{PluginID: old.Plugin.ID, WakerID: "w1", Body: body}).Output(ctx); err != nil {
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
			detail := PluginDetailResponse{Plugin: models.Plugin{SourceKey: "catalog:tools"}, Versions: []models.PluginVersion{{ID: "v", Version: test.Current}}, Installations: []models.WakerPlugin{{WakerID: "w1", VersionID: "v"}}}
			entries := []catalog.Entry{{SourceKey: "catalog:tools", Key: "tools", ArchiveSHA256: strings.Repeat("b", 64), Package: packageplugin.Package{Manifest: packageplugin.Manifest{Version: test.Target}}}}
			applyPluginUpdate(&detail, entries, catalog.SourceState{State: "available"})
			if detail.Update.Available != test.Available {
				t.Fatalf("wrong semantic order: %+v", detail.Update)
			}
		})
	}
	detail := PluginDetailResponse{Plugin: models.Plugin{SourceKey: "catalog:tools"}, Versions: []models.PluginVersion{{ID: "old", Version: "1.0.0"}, {ID: "ahead", Version: "3.0.0"}}, Installations: []models.WakerPlugin{{WakerID: "w1", VersionID: "old"}, {WakerID: "w2", VersionID: "ahead"}}}
	entries := []catalog.Entry{{SourceKey: "catalog:tools", Key: "tools", Package: packageplugin.Package{Manifest: packageplugin.Manifest{Version: "2.0.0"}}}}
	applyPluginUpdate(&detail, entries, catalog.SourceState{State: "available"})
	if detail.Update.Available {
		t.Fatal("mixed rollback offered a full update that would downgrade another partner")
	}
}

func TestRefreshCatalogPublishesLocalSuccessAndFailure(t *testing.T) {
	ctx := pluginAPIContext(t)
	previous := global.EventHub
	global.EventHub = events.New()
	t.Cleanup(func() { global.EventHub.Close(); global.EventHub = previous })
	subscription := global.EventHub.Subscribe(constants.PluginEventTopic, events.Resource{Kind: "global"})
	defer subscription.Close()
	writeUpdateCatalog(t, global.PluginCatalogSource(), "2.0.0")
	if _, err := (&RefreshPluginCatalog{}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-subscription.Events:
		if event.Type != "plugins.changed" {
			t.Fatalf("wrong refresh event: %+v", event)
		}
	default:
		t.Fatal("successful catalog check did not wake plugin queries")
	}
	if err := os.Remove(filepath.Join(global.PluginCatalogSource(), "catalog.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&RefreshPluginCatalog{}).Output(ctx); !errors.Is(err, status_error.PluginStorageError) {
		t.Fatalf("failed catalog check hid source error: %v", err)
	}
	select {
	case <-subscription.Events:
	default:
		t.Fatal("failed catalog check did not wake plugin queries")
	}
	value, err := (&ListPlugins{}).Output(ctx)
	if err != nil || value.(ListPluginsResponse).CatalogSource.State != "unavailable" {
		t.Fatalf("empty local library lost source error: %+v %v", value, err)
	}
}

func TestRefreshCatalogUsesHTTPRequestCancellation(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	ctx := pluginAPIContext(t)
	entered := make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer source.Close()
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", source.URL+"/catalog.json")
	request, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx.Request = ctx.Request.WithContext(request)
	done := make(chan error, 1)
	go func() { _, err := (&RefreshPluginCatalog{}).Output(ctx); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh request did not reach controlled upstream")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, status_error.PluginDistributionUnavailable) {
			t.Fatalf("cancelled refresh hid source error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP cancellation did not end explicit catalog refresh")
	}
}
