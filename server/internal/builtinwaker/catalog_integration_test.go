package builtinwaker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestEnsureDefaultsSeedsBuiltinWakerWithoutPersistingDocuments(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "builtin-waker-seed.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	ctx := context.Background()
	rolePresets := controllers.NewRolePresetController(database.DB)
	documents := controllers.NewProfileDocumentController(database.DB)
	wakers := controllers.NewWakerController(database.DB)

	roleCatalog := builtinrolepreset.ControllerCatalog{
		Presets:   rolePresets,
		Documents: documents,
	}
	if err := builtinrolepreset.EnsureCatalog(ctx, roleCatalog); err != nil {
		t.Fatalf("EnsureCatalog() error = %v", err)
	}

	wakerCatalog := ControllerCatalog{Wakers: wakers}
	if err := EnsureDefaults(ctx, wakerCatalog); err != nil {
		t.Fatalf("EnsureDefaults() error = %v", err)
	}

	presetDocuments, err := documents.ListRolePresetDocuments(
		ctx,
		builtinrolepreset.PersonalAssistantRolePresetID,
	)
	if err != nil {
		t.Fatalf("list role preset documents: %v", err)
	}
	if len(presetDocuments) != 0 {
		t.Fatalf("role preset document count = %d, want 0", len(presetDocuments))
	}

	waker, err := wakers.Get(ctx, builtinrolepreset.PersonalAssistantWakerID)
	if err != nil {
		t.Fatalf("get builtin waker: %v", err)
	}
	if waker.Name != "lumi" {
		t.Fatalf("waker name = %q", waker.Name)
	}

	storedWakerDocuments, err := documents.ListWakerDocuments(
		ctx,
		builtinrolepreset.PersonalAssistantWakerID,
	)
	if err != nil {
		t.Fatalf("list waker documents: %v", err)
	}
	if len(storedWakerDocuments) != 0 {
		t.Fatalf("waker document count = %d, want 0", len(storedWakerDocuments))
	}

	resolved, err := builtinrolepreset.ResolveWakerContents(
		builtinrolepreset.PersonalAssistantWakerID,
		storedWakerDocuments,
	)
	if err != nil {
		t.Fatalf("ResolveWakerContents() error = %v", err)
	}
	if len(resolved) != len(profile.EditableKinds()) {
		t.Fatalf("resolved document count = %d", len(resolved))
	}

	if err := EnsureDefaults(ctx, wakerCatalog); err != nil {
		t.Fatalf("EnsureDefaults() must be idempotent: %v", err)
	}
	count, err := wakers.Count(ctx, "")
	if err != nil {
		t.Fatalf("count wakers: %v", err)
	}
	if count != int64(len(builtinrolepreset.DefinitionsWithDefaultWaker())) {
		t.Fatalf("waker count = %d, want all registered built-ins", count)
	}

	researcher, err := wakers.Get(ctx, builtinrolepreset.WikiResearcherWakerID)
	if err != nil || researcher.Name != "nova" || researcher.RoleName != "维基研究员" {
		t.Fatalf("unexpected builtin researcher: %+v, %v", researcher, err)
	}
	researcher.Name = "我的研究伙伴"
	if err := wakers.Update(ctx, researcher.ID, researcher); err != nil {
		t.Fatal(err)
	}
	if err := documents.CreateWakerDocuments(ctx, []models.WakerDocument{{
		ID: "d72204129e3e4eff8036af4a317b5c93", WakerID: researcher.ID,
		Kind: constants.ProfileDocumentKindPersona, Content: "按用户指定的研究风格表达",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDefaults(ctx, wakerCatalog); err != nil {
		t.Fatal(err)
	}
	researcher, err = wakers.Get(ctx, researcher.ID)
	if err != nil || researcher.Name != "我的研究伙伴" {
		t.Fatalf("startup overwrote customized researcher: %+v, %v", researcher, err)
	}
	stored, err := documents.ListWakerDocuments(ctx, researcher.ID)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = builtinrolepreset.ResolveWakerContents(researcher.ID, stored)
	if err != nil || resolved[constants.ProfileDocumentKindPersona] != "按用户指定的研究风格表达" ||
		resolved[constants.ProfileDocumentKindPlaybook] == "" {
		t.Fatalf("startup lost researcher profile override or fallback: %v", err)
	}
}

func TestEnsureDefaultsCreatesMissingBuiltinWakerWhenOtherWakersExist(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "builtin-waker-existing.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	ctx := context.Background()
	rolePresets := controllers.NewRolePresetController(database.DB)
	documents := controllers.NewProfileDocumentController(database.DB)
	wakers := controllers.NewWakerController(database.DB)

	roleCatalog := builtinrolepreset.ControllerCatalog{
		Presets:   rolePresets,
		Documents: documents,
	}
	if err := builtinrolepreset.EnsureCatalog(ctx, roleCatalog); err != nil {
		t.Fatalf("EnsureCatalog() error = %v", err)
	}
	if err := wakers.Create(ctx, modelsWaker("custom-waker-id", "已有伙伴")); err != nil {
		t.Fatalf("create existing waker: %v", err)
	}

	if err := EnsureDefaults(ctx, ControllerCatalog{Wakers: wakers}); err != nil {
		t.Fatalf("EnsureDefaults() error = %v", err)
	}
	for _, definition := range builtinrolepreset.DefinitionsWithDefaultWaker() {
		if _, err := wakers.Get(ctx, definition.DefaultWaker.ID); err != nil {
			t.Fatalf("missing builtin waker %q alongside existing waker: %v", definition.RoleName, err)
		}
	}
}

func modelsWaker(id string, name string) models.Waker {
	return models.Waker{
		ID:          id,
		Name:        name,
		RoleName:    name,
		Avatar:      "preset:generic",
		Bio:         "",
		Expertise:   []profile.Expertise{},
		WorkStyle:   []string{},
		WorkMethods: []profile.WorkMethod{},
	}
}
