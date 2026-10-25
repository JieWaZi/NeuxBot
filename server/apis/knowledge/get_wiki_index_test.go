package knowledge

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

func TestGetWikiIndexBindsPaginationAndProjectsWholeLibrary(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Params = gin.Params{{Key: "libraryId", Value: "library"}}
	ctx.Request = httptest.NewRequest("GET", "/knowledge/libraries/library/wiki/index?pageTypes=concept&limit=1&cursor=1", nil)
	o := &GetWikiIndex{}
	if err := ginx.ParameterBinding(ctx, o, ginx.GetOperatorTypeInfo(reflect.TypeOf(o))); err != nil {
		t.Fatal(err)
	}
	if o.LibraryID != "library" || o.PageTypes != "concept" || o.Limit != 1 || o.Cursor != "1" {
		t.Fatalf("binding=%+v", o)
	}
	engine, err := core.Open(context.Background(), core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	previous := global.KnowledgeEngine
	global.KnowledgeEngine = engine
	t.Cleanup(func() { global.KnowledgeEngine = previous })
	receipt, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "create", Name: "知识库", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	libID := receipt.ResourceID
	for _, p := range []core.Page{{Slug: "concept/old", Title: "既有知识", PageType: "concept", Summary: "页面简介", Content: "正文不可出现在轻量索引"}, {Slug: "summary/new", Title: "新增文档", PageType: "summary", Content: "资料正文"}, {Slug: "index", Title: "Index", PageType: "index", Content: "# 全库导言\n\n全库说明。", Summary: "# 全库导言\n\n全库说明。"}} {
		lib, _ := engine.GetLibrary(ctx, libID)
		if _, err = engine.WritePage(ctx, core.WritePageInput{RequestID: p.Slug, LibraryID: libID, StructureVersion: lib.StructureVersion, Page: p}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := (&GetWikiIndex{LibraryID: libID}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "正文不可出现在轻量索引") || strings.Contains(string(raw), "next_cursor") || strings.Contains(string(raw), "category_path") {
		t.Fatalf("incorrect public projection: %s", raw)
	}
	var view WikiIndexView
	if err = json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if view.Intro != "# Wiki Index\n\n全库说明。" || len(view.Groups) != 5 {
		t.Fatalf("view=%+v", view)
	}
	found := map[string]bool{}
	for _, g := range view.Groups {
		for _, item := range g.Items {
			found[item.Slug] = true
		}
	}
	if !found["concept/old"] || !found["summary/new"] {
		t.Fatal("missing whole-library directory", found)
	}
}
