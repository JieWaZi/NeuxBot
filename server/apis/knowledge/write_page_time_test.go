package knowledge

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// TestWritePagePreservesCallerReadTime 防止复制最新证据时用最新时间掩盖旧整页请求。
func TestWritePagePreservesCallerReadTime(t *testing.T) {
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
	oldEngine, oldController := global.KnowledgeEngine, global.KnowledgeControllerManager
	global.KnowledgeEngine, global.KnowledgeControllerManager = engine, controllers.NewKnowledgeController(db.DB)
	defer func() { global.KnowledgeEngine, global.KnowledgeControllerManager = oldEngine, oldController }()
	receipt, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "time-library", Name: "时间校验", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	lib, _ := engine.GetLibrary(ctx, receipt.ResourceID)
	rec, err := engine.WritePage(ctx, core.WritePageInput{RequestID: "time-page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "concept/time", Title: "时间", PageType: "concept", Content: "最新正文"}})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := engine.GetPage(ctx, lib.ID, rec.ResourceID)
	request := pageDTO(p)
	request.UpdatedAt-- // 模拟调用者持有的旧读取时间，内容版本仍相同。
	request.Content = "旧草稿"
	_, err = (&WritePage{LibraryID: lib.ID, PageID: p.ID, Body: WritePageInput{RequestID: "stale-time", ExpectedVersion: p.Version, StructureVersion: lib.StructureVersion, Page: request}}).Output(&gin.Context{})
	if !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("stale whole-page request error=%v", err)
	}
	after, _ := engine.GetPage(ctx, lib.ID, p.ID)
	if after.Content != "最新正文" {
		t.Fatal("stale request overwrote current body")
	}
}
