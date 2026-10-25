package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	service "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// failingDefaultsController 注入跨数据库成功后宿主偏好提交失败。
type failingDefaultsController struct {
	controllers.KnowledgeController      // 复用真实SQLite控制器。
	fail                            bool // 只在指定提交返回确定失败。
}

func (f *failingDefaultsController) UpdateLibraryExecutionDefaults(ctx context.Context, id string, version int64, value models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
	if f.fail {
		return models.LibraryExecutionDefaults{}, status_error.DatabaseError
	}
	return f.KnowledgeController.UpdateLibraryExecutionDefaults(ctx, id, version, value)
}

func TestCreateLibraryRecoversHostExecutionDefaultsWithoutDuplicatingCore(t *testing.T) {
	ctx := context.Background()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "host.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	db, _ := database.DB.DB()
	defer db.Close()
	oldEngine, oldRefs, oldService := global.KnowledgeEngine, global.KnowledgeControllerManager, global.KnowledgeService
	global.KnowledgeEngine = engine
	refs := &failingDefaultsController{KnowledgeController: controllers.NewKnowledgeController(database.DB), fail: true}
	global.KnowledgeControllerManager = refs
	global.KnowledgeService = service.New(service.Config{ResolveModel: func(_ context.Context, value models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
		return value, nil
	}})
	defer func() {
		global.KnowledgeEngine = oldEngine
		global.KnowledgeControllerManager = oldRefs
		global.KnowledgeService = oldService
	}()
	input := CreateLibraryInput{RequestID: "stable-create", Name: "Library", WikiLanguage: "zh-CN", ExecutionDefaults: &LibraryExecutionDefaultsInput{RuntimeID: "codex", Model: "chosen"}}
	g := &gin.Context{}
	if _, err = (&CreateLibrary{Body: input}).Output(g); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("partial failure reported success: %v", err)
	}
	_, count, err := engine.ListLibraries(ctx, 0, 10)
	if err != nil || count != 1 {
		t.Fatalf("core mutation missing: %d %v", count, err)
	}
	refs.fail = false
	result, err := (&CreateLibrary{Body: input}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	id := result.(Receipt).ResourceID
	_, count, err = engine.ListLibraries(ctx, 0, 10)
	if err != nil || count != 1 {
		t.Fatalf("retry duplicated library: %d %v", count, err)
	}
	saved, err := (&GetLibraryExecutionDefaults{LibraryID: id}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	value := saved.(models.LibraryExecutionDefaults)
	if value.Version != 1 || value.Model != "chosen" {
		t.Fatalf("defaults not recovered: %+v", value)
	}
	updated, err := (&UpdateLibraryExecutionDefaults{LibraryID: id, Body: UpdateLibraryExecutionDefaultsInput{RequestID: "edit-defaults", ExpectedVersion: 1, LibraryExecutionDefaultsInput: LibraryExecutionDefaultsInput{RuntimeID: "codex", Model: "next"}}}).Output(g)
	if err != nil || updated.(models.LibraryExecutionDefaults).Version != 2 {
		t.Fatalf("settings save=%+v %v", updated, err)
	}
	// 已提交保存的重放不应依赖现在仍可解析旧Runtime。
	global.KnowledgeService = service.New(service.Config{ResolveModel: func(_ context.Context, value models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
		return value, status_error.RuntimeUnavailable
	}})
	if _, err = (&CreateLibrary{Body: input}).Output(g); err != nil {
		t.Fatalf("create receipt lost after runtime changed: %v", err)
	}
	if _, err = (&UpdateLibraryExecutionDefaults{LibraryID: id, Body: UpdateLibraryExecutionDefaultsInput{RequestID: "edit-defaults", ExpectedVersion: 1, LibraryExecutionDefaultsInput: LibraryExecutionDefaultsInput{RuntimeID: "codex", Model: "next"}}}).Output(g); err != nil {
		t.Fatalf("defaults receipt lost after runtime changed: %v", err)
	}
	if _, err = (&ImportSource{LibraryID: id, Body: ImportSourceInput{RequestID: "activity-upload", Filename: "source.md", Bytes: base64.StdEncoding.EncodeToString([]byte("classified-body-do-not-log"))}}).Output(g); err != nil {
		t.Fatal(err)
	}
	activity, err := (&ListLibraryActivity{LibraryID: id, Limit: 100}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	rows := activity.(LibraryActivityList)
	if len(rows.Data) != 4 {
		t.Fatalf("activity not durably recorded/idempotent: %+v", rows)
	}
	for _, event := range rows.Data {
		if !strings.HasPrefix(event.ActorUserID, "local:") || event.Outcome != "success" || event.ScopeID != id {
			t.Fatalf("false actor/result/scope: %+v", event)
		}
	}
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), "classified-body-do-not-log") || strings.Contains(string(encoded), "chosen") {
		t.Fatal("activity leaked body or execution settings")
	}
	filtered, err := (&ListLibraryActivity{LibraryID: id, Action: "kb.created", Outcome: "success"}).Output(g)
	if err != nil || len(filtered.(LibraryActivityList).Data) != 1 {
		t.Fatalf("exact filters=%+v %v", filtered, err)
	}
	page, err := (&ListLibraryActivity{LibraryID: id, Limit: 1}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	firstPage := page.(LibraryActivityList)
	next, err := (&ListLibraryActivity{LibraryID: id, Limit: 1, AfterID: firstPage.NextCursor}).Output(g)
	if err != nil || len(next.(LibraryActivityList).Data) != 1 || next.(LibraryActivityList).Data[0].ID >= firstPage.Data[0].ID {
		t.Fatalf("cursor page=%+v %v", next, err)
	}
	library, err := engine.GetLibrary(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.UpdateLibrary(ctx, core.UpdateLibraryInput{RequestID: "archive-for-read-only", LibraryID: id, ExpectedVersion: library.Version, Name: library.Name, PolicyVersion: library.PolicyVersion, Archived: true}); err != nil {
		t.Fatal(err)
	}
	_, err = (&UpdateLibraryExecutionDefaults{LibraryID: id, Body: UpdateLibraryExecutionDefaultsInput{RequestID: "blocked-read-only", ExpectedVersion: 2, LibraryExecutionDefaultsInput: LibraryExecutionDefaultsInput{RuntimeID: "codex", Model: "must-not-save"}}}).Output(g)
	if !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("read-only preference mutation was not rejected before runtime validation: %v", err)
	}
	unchanged, err := refs.LibraryExecutionDefaults(ctx, id)
	if err != nil || unchanged.Version != 2 || unchanged.Model != "next" {
		t.Fatalf("read-only request partially mutated host defaults: %+v %v", unchanged, err)
	}

}
