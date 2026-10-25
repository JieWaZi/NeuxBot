package controllers

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestPluginControllerSeparatesLibraryFromInstalledPartnerAndPinsVersion(t *testing.T) {
	ctx := context.Background()
	c := NewPluginController(newTestDB(t))
	for _, p := range []models.Plugin{{ID: "plugin-a", Name: "办公", SourceKey: "source-a"}, {ID: "plugin-b", Name: "办公", SourceKey: "source-b"}, {ID: "private", Name: "私有", SourceKey: "source-private", OwnerWakerID: "w1"}} {
		if err := c.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	version, err := c.PutVersion(ctx, models.PluginVersion{ID: "v1", PluginID: "plugin-a", Version: "1.0.0", Digest: strings.Repeat("a", 64), Package: plugins.Package{}})
	if err != nil {
		t.Fatal(err)
	}
	same, err := c.PutVersion(ctx, models.PluginVersion{ID: "duplicate", PluginID: "plugin-a", Version: "1.0.0", Digest: strings.Repeat("a", 64)})
	if err != nil || same.ID != version.ID {
		t.Fatalf("version not idempotent: %+v %v", same, err)
	}
	if _, err := c.PutVersion(ctx, models.PluginVersion{ID: "conflict", PluginID: "plugin-a", Version: "1.0.0", Digest: strings.Repeat("b", 64)}); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("expected digest conflict: %v", err)
	}
	_, total, err := c.List(ctx, "", "", "", 0, 1, false)
	if err != nil || total != 2 {
		t.Fatalf("global total: %d %v", total, err)
	}
	matched, count, err := c.List(ctx, "", "", "source-a", 0, 50, false)
	if err != nil || count != 1 || len(matched) != 1 || matched[0].ID != "plugin-a" {
		t.Fatalf("renamed source lookup: %+v %d %v", matched, count, err)
	}

	items, total, err := c.List(ctx, "", "w1", "", 0, 50, false)
	if err != nil || total != 1 || items[0].ID != "private" {
		t.Fatalf("uninstalled resources leaked: %+v %d %v", items, total, err)
	}
	if err := c.Install(ctx, models.WakerPlugin{ID: "r1", PluginID: "plugin-a", VersionID: "v1", WakerID: "w1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Install(ctx, models.WakerPlugin{ID: "r2", PluginID: "private", VersionID: "v1", WakerID: "w2", Enabled: true}); !errors.Is(err, status_error.PluginForbidden) {
		t.Fatalf("private ownership bypassed: %v", err)
	}
	if err := c.Install(ctx, models.WakerPlugin{ID: "r3", PluginID: "plugin-b", VersionID: "v1", WakerID: "w1", Enabled: true}); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("foreign version accepted: %v", err)
	}
	items, total, err = c.List(ctx, "", "w1", "办公", 0, 50, false)
	if err != nil || total != 1 || items[0].ID != "plugin-a" {
		t.Fatalf("installed library incorrect: %+v %d %v", items, total, err)
	}
	if err := c.Uninstall(ctx, "w1", "plugin-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, "plugin-a"); err != nil {
		t.Fatal("uninstall deleted global resource", err)
	}
}

func TestPluginConfigurationEncryptsSecretsAndRejectsStaleRevision(t *testing.T) {
	db := newTestDB(t)
	c := NewPluginController(db)
	ctx := context.Background()
	if err := c.Create(ctx, models.Plugin{ID: "plugin-a", Name: "共享", SourceKey: "source"}); err != nil {
		t.Fatal(err)
	}
	config := models.PluginConfiguration{PluginID: "plugin-a", Values: map[string]string{"region": "cn"}, Secrets: []models.MCPSecretDefinition{{ID: "api-key", Name: "API_KEY", Configured: true, Value: "secret-value"}}}
	if err := c.UpdateConfiguration(ctx, config, 0); err != nil {
		t.Fatal(err)
	}
	var raw models.PluginConfiguration
	if err := db.Where("plugin_id = ?", "plugin-a").First(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if raw.Revision != 1 || len(raw.Secrets) != 1 || raw.Secrets[0].Ciphertext == "" || strings.Contains(raw.Secrets[0].Ciphertext, "secret-value") {
		t.Fatalf("credentials not encrypted: %+v", raw)
	}
	got, err := c.Configuration(ctx, "plugin-a")
	if err != nil || got.Secrets[0].Value != "secret-value" || got.Secrets[0].Ciphertext != "" {
		t.Fatalf("cannot use decrypted shared config: %+v %v", got, err)
	}
	if err := c.UpdateConfiguration(ctx, config, 0); !errors.Is(err, status_error.PluginConfigurationConflict) {
		t.Fatalf("stale write accepted: %v", err)
	}
}

func TestPluginVersionCASRejectsConcurrentChanges(t *testing.T) {
	ctx := context.Background()
	c := NewPluginController(newTestDB(t))
	if err := c.Create(ctx, models.Plugin{ID: "plugin-a", Name: "版本", SourceKey: "source"}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []models.PluginVersion{{ID: "v1", PluginID: "plugin-a", Version: "1", Digest: "first"}, {ID: "v2", PluginID: "plugin-a", Version: "2", Digest: "second"}} {
		if _, err := c.PutVersion(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Install(ctx, models.WakerPlugin{ID: "r", PluginID: "plugin-a", VersionID: "v1", WakerID: "w1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.SwitchVersion(ctx, "w1", "plugin-a", "v1", "v2"); err != nil {
		t.Fatal(err)
	}
	if err := c.SwitchVersion(ctx, "w1", "plugin-a", "v1", "v2"); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("stale version switch accepted: %v", err)
	}
}

func TestPluginConfigurationAndVersionChangesRollbackTogether(t *testing.T) {
	db := newTestDB(t)
	c := NewPluginController(db)
	ctx := context.Background()
	if err := c.Create(ctx, models.Plugin{ID: "atomic", Name: "原子更新", SourceKey: "atomic"}); err != nil {
		t.Fatal(err)
	}
	initial := models.PluginConfiguration{PluginID: "atomic", Values: map[string]string{"region": "old"}}
	if err := c.UpdateConfiguration(ctx, initial, 0); err != nil {
		t.Fatal(err)
	}
	tx := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	err := tx.Transaction(ctx, func(txctx context.Context) error {
		if err := c.UpdateConfiguration(txctx, models.PluginConfiguration{PluginID: "atomic", Values: map[string]string{"region": "new"}}, 1); err != nil {
			return err
		}
		return c.SwitchVersion(txctx, "w1", "atomic", "missing", "missing")
	})
	if err == nil {
		t.Fatal("accepted failing version switch")
	}
	got, err := c.Configuration(ctx, "atomic")
	if err != nil || got.Revision != 1 || got.Values["region"] != "old" {
		t.Fatalf("configuration partially committed: %+v %v", got, err)
	}
	if err := c.UpdateConfiguration(ctx, models.PluginConfiguration{PluginID: "atomic", Values: map[string]string{"region": "changed"}}, 1); err != nil {
		t.Fatal(err)
	}
	got, err = c.Configuration(ctx, "atomic")
	if err != nil || got.Revision != 2 || got.Values["region"] != "changed" {
		t.Fatalf("configuration did not advance: %+v %v", got, err)
	}
}

func TestIndependentResourcesAllowDuplicateDisplayNamesAndRequireInstallation(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	skills := NewSkillController(db)
	for _, id := range []string{"s1", "s2"} {
		if err := skills.CreatePackage(ctx, models.SkillPackage{ID: id, Name: "same", Scope: constants.SkillScopePublic, Source: constants.SkillSourceZip}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := skills.ListVisible(ctx, "w1", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("global skills implicitly installed: %+v %v", items, err)
	}
	if err := skills.UpsertWakerSkill(ctx, models.WakerSkill{ID: "sr1", WakerID: "w1", SkillPackageID: "s1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	items, err = skills.ListVisible(ctx, "w1", "")
	if err != nil || len(items) != 1 || items[0].SkillPackage.ID != "s1" {
		t.Fatalf("installed skill missing: %+v %v", items, err)
	}
	connectors := NewMCPConnectorController(db)
	for _, id := range []string{"c1", "c2"} {
		if err := connectors.Create(ctx, models.MCPConnector{ID: id, RuntimeKey: id, Name: "same", Scope: constants.MCPConnectorScopePublic}); err != nil {
			t.Fatal(err)
		}
	}
	if err := connectors.Create(ctx, models.MCPConnector{ID: "private-c", RuntimeKey: "private-c", Name: "owned", Scope: constants.MCPConnectorScopePrivate, OwnerWakerID: "w1"}); err != nil {
		t.Fatal(err)
	}
	if err := connectors.UpsertWakerConnector(ctx, models.WakerMCPConnector{ID: "forbidden", WakerID: "w2", MCPConnectorID: "private-c", Enabled: true}); err == nil {
		t.Fatal("accepted foreign owned connector installation")
	}
	conns, err := connectors.ListForWaker(ctx, "w2", "")
	if err != nil || len(conns) != 0 {
		t.Fatalf("uninstalled or private connectors leaked: %+v %v", conns, err)
	}
	if err := connectors.UpsertWakerConnector(ctx, models.WakerMCPConnector{ID: "cr1", WakerID: "w1", MCPConnectorID: "c1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := connectors.DeleteWakerConnector(ctx, "w1", "c1"); err != nil {
		t.Fatal(err)
	}
	if _, err := connectors.Get(ctx, "c1"); err != nil {
		t.Fatalf("uninstall removed shared connector: %v", err)
	}
	if err := connectors.DeleteByWaker(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := connectors.Get(ctx, "private-c"); err == nil {
		t.Fatal("owned connector survived partner deletion")
	}
}

func TestResourceLibrariesPageGlobalItemsAndCountOnlyInstalledPartners(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	skills := NewSkillController(db)
	connectors := NewMCPConnectorController(db)
	for _, id := range []string{"a", "b"} {
		if err := skills.CreatePackage(ctx, models.SkillPackage{ID: "s" + id, Name: "技能" + id, Scope: constants.SkillScopePublic, Source: constants.SkillSourceZip}); err != nil {
			t.Fatal(err)
		}
		if err := connectors.Create(ctx, models.MCPConnector{ID: "c" + id, RuntimeKey: "c" + id, Name: "连接" + id, Scope: constants.MCPConnectorScopePublic}); err != nil {
			t.Fatal(err)
		}
	}
	if err := skills.UpsertWakerSkill(ctx, models.WakerSkill{ID: "sr", WakerID: "w1", SkillPackageID: "sa", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := connectors.UpsertWakerConnector(ctx, models.WakerMCPConnector{ID: "cr", WakerID: "w1", MCPConnectorID: "ca", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s, total, err := skills.ListLibrary(ctx, "", 0, 1)
	if err != nil || len(s) != 1 || total != 2 {
		t.Fatalf("skill pagination: %+v %d %v", s, total, err)
	}
	c, total, err := connectors.ListLibrary(ctx, "", 0, 1)
	if err != nil || len(c) != 1 || total != 2 {
		t.Fatalf("connector pagination: %+v %d %v", c, total, err)
	}
	sc, err := skills.InstalledCounts(ctx, []string{"sa", "sb"})
	if err != nil || sc["sa"] != 1 || sc["sb"] != 0 {
		t.Fatalf("skill counts: %+v %v", sc, err)
	}
	cc, err := connectors.InstalledCounts(ctx, []string{"ca", "cb"})
	if err != nil || cc["ca"] != 1 || cc["cb"] != 0 {
		t.Fatalf("connector counts: %+v %v", cc, err)
	}
}

func TestPluginChangesPublishOnlyAfterSuccessfulTransaction(t *testing.T) {
	ctx := context.Background()
	hub := events.New()
	defer hub.Close()
	db := WithEventPublisher(newTestDB(t), hub)
	store := NewPluginController(db)
	if err := store.Create(ctx, models.Plugin{ID: "p", Name: "events", SourceKey: "events"}); err != nil {
		t.Fatal(err)
	}
	subscription := hub.Subscribe(constants.PluginEventTopic, events.Resource{Kind: "plugin", ID: "p"})
	defer subscription.Close()
	tx := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	err := tx.Transaction(ctx, func(ctx context.Context) error {
		if err := store.UpdateConfiguration(ctx, models.PluginConfiguration{PluginID: "p", Values: map[string]string{}}, 0); err != nil {
			return err
		}
		return status_error.PluginVersionConflict
	})
	if err == nil {
		t.Fatal("failed transaction unexpectedly passed")
	}
	select {
	case event := <-subscription.Events:
		t.Fatalf("rollback published change: %+v", event)
	default:
	}
	if err := tx.Transaction(ctx, func(ctx context.Context) error {
		return store.UpdateConfiguration(ctx, models.PluginConfiguration{PluginID: "p", Values: map[string]string{}}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-subscription.Events:
		if event.Resource.ID != "p" {
			t.Fatalf("wrong identity: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("committed configuration change was not published")
	}
}

func TestInstalledPluginSnapshotContainsIdentityAndPartnerNames(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	c := NewPluginController(db)
	if err := NewWakerController(db).Create(ctx, models.Waker{ID: "w1", Name: "伙伴一", Avatar: "companion:fixture-avatar"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, models.Plugin{ID: "p1", Name: "办公", SourceKey: "source"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutVersion(ctx, models.PluginVersion{ID: "v1", PluginID: "p1", Version: "1.0", Digest: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := c.Install(ctx, models.WakerPlugin{ID: "r1", WakerID: "w1", PluginID: "p1", VersionID: "v1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	states, err := c.InstalledStates(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Plugin.ID != "p1" || states[0].Plugin.Name != "办公" || states[0].PartnerNames["w1"] != "伙伴一" || states[0].PartnerAvatars["w1"] != "companion:fixture-avatar" {
		t.Fatalf("execution snapshot lacks resource identity: %+v", states)
	}
}
