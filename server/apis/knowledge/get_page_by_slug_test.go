package knowledge

import (
	"context"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

func TestGetPageBySlugBindsLibraryAndRequiresExactAddress(t *testing.T) {
	for _, query := range []string{"?slug=concept%2Ftarget", "", "?slug="} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Params = gin.Params{{Key: "libraryId", Value: "current-library"}}
		ctx.Request = httptest.NewRequest("GET", "/knowledge/libraries/current-library/page-by-slug"+query, nil)
		operator := &GetPageBySlug{}
		err := ginx.ParameterBinding(ctx, operator, ginx.GetOperatorTypeInfo(reflect.TypeOf(operator)))
		if query != "?slug=concept%2Ftarget" {
			if err == nil {
				t.Fatalf("query=%q accepted empty slug", query)
			}
			continue
		}
		if err != nil || operator.LibraryID != "current-library" || operator.Slug != "concept/target" {
			t.Fatalf("binding=%+v err=%v", operator, err)
		}
	}
}

func TestGetPageBySlugResolvesOutsideListWindowAndKeepsLibraryScope(t *testing.T) {
	ctx := context.Background()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	previous := global.KnowledgeEngine
	global.KnowledgeEngine = engine
	t.Cleanup(func() { global.KnowledgeEngine = previous })
	createLibrary := func(name string) string {
		receipt, createErr := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: name, Name: name, Kind: "general", WikiLanguage: "zh-CN"})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return receipt.ResourceID
	}
	createPage := func(libraryID, slug, title string) string {
		library, getErr := engine.GetLibrary(ctx, libraryID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		receipt, writeErr := engine.WritePage(ctx, core.WritePageInput{RequestID: libraryID + slug, LibraryID: libraryID, StructureVersion: library.StructureVersion, Page: core.Page{Slug: slug, Title: title, PageType: "concept", Status: "published", Content: title}})
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		return receipt.ResourceID
	}
	first, second := createLibrary("first"), createLibrary("second")
	target := createPage(first, "concept/target", "精确目标")
	for index := range 51 {
		createPage(first, fmt.Sprintf("concept/target-%02d", index), "相近页面")
	}
	secondTarget := createPage(second, "concept/target", "另一知识库")
	createPage(second, "concept/private", "另一库专有正文")
	window, _, err := engine.ListPages(ctx, core.PageQuery{LibraryID: first, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range window {
		if item.ID == target {
			t.Fatal("target unexpectedly present in first list window")
		}
	}
	for _, libraryID := range []string{first, second} {
		result, getErr := (&GetPageBySlug{LibraryID: libraryID, Slug: "concept/target"}).Output(&gin.Context{})
		if getErr != nil {
			t.Fatal(getErr)
		}
		wantID := target
		if libraryID == second {
			wantID = secondTarget
		}
		if result.(Page).ID != wantID {
			t.Fatalf("library=%s got=%+v want=%s", libraryID, result, wantID)
		}
	}
	for _, slug := range []string{"concept/targe", "concept/missing", "concept/private"} {
		_, getErr := (&GetPageBySlug{LibraryID: first, Slug: slug}).Output(&gin.Context{})
		if getErr != status_error.KnowledgeNotFound {
			t.Fatalf("slug=%s err=%v want=KnowledgeNotFound", slug, getErr)
		}
	}
}
