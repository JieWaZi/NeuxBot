package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"path/filepath"
	"testing"
)

func TestKnowledgeAPIRawOnlyAndCreatePageThemeUseCoreIdentity(t *testing.T) {
	ctx := context.Background()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "ally.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	defer sqlDB.Close()
	oldEngine, oldRefs := global.KnowledgeEngine, global.KnowledgeControllerManager
	global.KnowledgeEngine = engine
	global.KnowledgeControllerManager = controllers.NewKnowledgeController(db.DB)
	defer func() { global.KnowledgeEngine = oldEngine; global.KnowledgeControllerManager = oldRefs }()
	g := &gin.Context{}
	created, err := (&CreateLibrary{Body: CreateLibraryInput{RequestID: "lib", Name: "Knowledge", WikiLanguage: "zh-CN"}}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	libID := created.(Receipt).ResourceID
	themes, err := (&ListThemes{LibraryID: libID}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(themes)
	if err != nil {
		t.Fatal(err)
	}
	var themeList map[string][]map[string]interface{}
	if err := json.Unmarshal(encoded, &themeList); err != nil {
		t.Fatal(err)
	}
	if len(themeList["items"]) != 1 || themeList["items"][0]["systemKey"] != "unclassified" {
		t.Fatalf("system folder identity missing: %s", encoded)
	}
	original := []byte("# Original bytes\r\nKeep raw unchanged.\n")
	saved, err := (&ImportSource{LibraryID: libID, Body: ImportSourceInput{RequestID: "raw", Filename: "evidence.md", MIME: "text/markdown", Bytes: base64.StdEncoding.EncodeToString(original)}}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := (&GetRaw{LibraryID: libID, SourceID: saved.(Receipt).ResourceID}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	if raw.(RawSource).Bytes != base64.StdEncoding.EncodeToString(original) {
		t.Fatal("raw bytes changed")
	}
	if _, total, _ := engine.ListJobs(ctx, libID, 0, 50); total != 0 {
		t.Fatal("raw upload started a knowledge job")
	}
	lib, _ := engine.GetLibrary(ctx, libID)
	theme, err := (&CreateTheme{LibraryID: libID, Body: CreateThemeInput{RequestID: "theme", StructureVersion: lib.StructureVersion, Theme: Theme{ID: "untrusted-client-id", Name: "Concepts"}}}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	if theme.(Receipt).ResourceID == "untrusted-client-id" || theme.(Receipt).ResourceID == "" {
		t.Fatal("theme did not use core identity")
	}
	lib, _ = engine.GetLibrary(ctx, libID)
	page, err := (&CreatePage{LibraryID: libID, Body: CreatePageInput{RequestID: "page", StructureVersion: lib.StructureVersion, Page: Page{ID: "untrusted-client-id", Slug: "concept/topic", Title: "Topic", PageType: "concept", Status: "published", Content: "A real page", ThemeID: theme.(Receipt).ResourceID}}}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	if page.(Receipt).ResourceID == "untrusted-client-id" || page.(Receipt).ResourceID == "" {
		t.Fatal("page did not use core identity")
	}
	listing, listErr := (&ListLibraries{}).Output(g)
	if listErr != nil {
		t.Fatal(listErr)
	}
	listed := listing.(LibraryList).Items
	if len(listed) != 1 || listed[0].PageTotal == nil || *listed[0].PageTotal != 1 {
		t.Fatal(listed)
	}
	_, err = (&ImportSource{LibraryID: libID, Body: ImportSourceInput{RequestID: "bad", Filename: "file.md", Bytes: "not base64"}}).Output(g)
	if err == nil {
		t.Fatal("invalid base64 accepted")
	}
}
