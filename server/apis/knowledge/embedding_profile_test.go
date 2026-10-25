package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/memory"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// interruptedEmbeddingCommit 保留真实数据库，仅模拟核心成功后宿主确认丢失。
type interruptedEmbeddingCommit struct {
	// KnowledgeController 使用真实配置与回执存储。
	controllers.KnowledgeController
	// interrupt 控制一次宿主提交失败。
	interrupt bool
}

func (c *interruptedEmbeddingCommit) CommitLibraryEmbedding(ctx context.Context, id string, version int64) (models.LibraryEmbeddingProfile, error) {
	if c.interrupt {
		return models.LibraryEmbeddingProfile{}, status_error.DatabaseError
	}
	return c.KnowledgeController.CommitLibraryEmbedding(ctx, id, version)
}

func TestLibraryEmbeddingAPIRecoversUnknownSaveAndNeverSharesMemory(t *testing.T) {
	ctx := context.Background()
	e, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "core.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "host.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	defer sqlDB.Close()
	mem, err := memory.NewService(memory.ServiceConfig{DBPath: filepath.Join(t.TempDir(), "memory.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	_, err = mem.UpdateEmbeddingSettings(ctx, memory.EmbeddingSettingsUpdate{BaseURL: "https://memory.test/v1", Model: "memory-only", APIKey: "memory-secret"})
	if err != nil {
		t.Fatal(err)
	}
	oldE, oldR, oldM := global.KnowledgeEngine, global.KnowledgeControllerManager, global.MemoryService
	global.KnowledgeEngine = e
	refs := &interruptedEmbeddingCommit{KnowledgeController: controllers.NewKnowledgeController(db.DB), interrupt: true}
	global.KnowledgeControllerManager = refs
	global.MemoryService = mem
	defer func() {
		global.KnowledgeEngine = oldE
		global.KnowledgeControllerManager = oldR
		global.MemoryService = oldM
	}()
	input := CreateLibraryInput{RequestID: "independent-create", Name: "独立库", WikiLanguage: "zh-CN", EmbeddingProfile: &LibraryEmbeddingInput{BaseURL: "https://knowledge.test/v1", Model: "library-only", APIKey: "library-secret"}}
	if _, err = (&CreateLibrary{Body: input}).Output(&gin.Context{}); !errors.Is(err, status_error.DatabaseError) {
		t.Fatal("partial create reported success", err)
	}
	_, count, err := e.ListLibraries(ctx, 0, 10)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	refs.interrupt = false
	value, err := (&CreateLibrary{Body: input}).Output(&gin.Context{})
	if err != nil {
		t.Fatal(err)
	}
	id := value.(Receipt).LibraryID
	_, count, err = e.ListLibraries(ctx, 0, 10)
	if err != nil || count != 1 {
		t.Fatal("duplicate core library", count, err)
	}
	got, err := (&GetLibraryEmbeddingProfile{LibraryID: id}).Output(&gin.Context{})
	if err != nil {
		t.Fatal(err)
	}
	saved := got.(models.LibraryEmbeddingProfile)
	raw, _ := json.Marshal(saved)
	var object map[string]any
	_ = json.Unmarshal(raw, &object)
	if _, ok := object["apiKey"]; ok {
		t.Fatal("API returned a key field")
	}
	if saved.Version != 1 || !saved.HasAPIKey || saved.Model != "library-only" {
		t.Fatal(saved)
	}
	settings, err := mem.GetEmbeddingSettings()
	if err != nil || settings.BaseURL != "https://memory.test/v1" || settings.Model != "memory-only" || !settings.APIKeyConfigured {
		t.Fatal("knowledge overwrote memory", settings, err)
	}
	_, err = mem.UpdateEmbeddingSettings(ctx, memory.EmbeddingSettingsUpdate{BaseURL: "https://memory-next.test", Model: "memory-next", APIKey: "new-memory-secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := refs.LibraryEmbeddingSnapshot(ctx, id)
	if err != nil || key != "library-secret" {
		t.Fatal("memory changed library key", err)
	}
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer library-secret" {
			t.Error("probe loaded other key")
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.5,0.25]}]}`))
	}))
	defer probe.Close()
	tested, err := (&TestLibraryEmbedding{Body: TestLibraryEmbeddingInput{LibraryID: id, LibraryEmbeddingInput: LibraryEmbeddingInput{BaseURL: probe.URL, Model: "draft"}}}).Output(&gin.Context{})
	if err != nil || tested.(LibraryEmbeddingTestResult).Dimensions != 2 {
		t.Fatal(tested, err)
	}
	after, _ := refs.LibraryEmbeddingProfile(ctx, id)
	if after.Version != saved.Version || after.BaseURL != saved.BaseURL {
		t.Fatal("connection test persisted draft")
	}
	update := UpdateLibraryEmbeddingProfile{LibraryID: id, Body: UpdateLibraryEmbeddingProfileInput{RequestID: "embedding-edit", ExpectedVersion: 1, ExpectedLibraryVersion: saved.LibraryVersion, LibraryEmbeddingInput: LibraryEmbeddingInput{BaseURL: probe.URL, Model: "library-only"}}}
	refs.interrupt = true
	if _, err = update.Output(&gin.Context{}); !errors.Is(err, status_error.DatabaseError) {
		t.Fatal("partial update reported success", err)
	}
	lib, _ := e.GetLibrary(ctx, id)
	_, err = e.UpdateLibrary(ctx, core.UpdateLibraryInput{RequestID: "after-partial-rename", LibraryID: id, ExpectedVersion: lib.Version, Name: "Later name", Purpose: lib.Purpose, PolicyVersion: lib.PolicyVersion})
	if err != nil {
		t.Fatal(err)
	}
	refs.interrupt = false
	if _, err = update.Output(&gin.Context{}); err != nil {
		t.Fatal("original core intent cannot recover", err)
	}
	lib, _ = e.GetLibrary(ctx, id)
	if lib.Name != "Later name" {
		t.Fatal("recovery overwrote later edit")
	}
	// 创建回执重放不得被后来用户改过的独立模型阻断，也不能恢复旧配置。
	update.Body.RequestID = "valid-model-change"
	update.Body.ExpectedVersion = 2
	update.Body.ExpectedLibraryVersion = lib.Version
	update.Body.Model = "library-next"
	if _, err = update.Output(&gin.Context{}); err != nil {
		t.Fatal(err)
	}
	if _, err = (&CreateLibrary{Body: input}).Output(&gin.Context{}); err != nil {
		t.Fatal("create replay failed after later model edit", err)
	}
	lib, _ = e.GetLibrary(ctx, id)
	_, err = e.ImportSource(ctx, core.ImportSourceInput{RequestID: "nonempty", LibraryID: id, Filename: "x.md", Bytes: []byte("实际文档使上游模型更换守卫生效。")})
	if err != nil {
		t.Fatal(err)
	}
	update.Body.RequestID = "forbidden-model-change"
	update.Body.ExpectedVersion = 3
	update.Body.ExpectedLibraryVersion = lib.Version
	update.Body.Model = "different"
	if _, err = update.Output(&gin.Context{}); !errors.Is(err, status_error.KnowledgeInvalid) {
		t.Fatal("nonempty core model guard bypassed", err)
	}
	after, _ = refs.LibraryEmbeddingProfile(ctx, id)
	if after.Version != 3 || after.Model != "library-next" {
		t.Fatal("rejected core changed active host profile")
	}
	if _, err = (bridge.LibraryEmbeddings{Engine: e, References: refs}).Freeze(ctx, id, "different"); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatal("mismatched model froze", err)
	}
}
