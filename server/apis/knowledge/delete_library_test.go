package knowledge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	knowledgeService "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestLibraryDeleteRejectsStaleVersionAndCleansTaskConfiguration(t *testing.T) {
	ctx := context.Background()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "ally.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	defer sqlDB.Close()
	oldEngine, oldRefs := global.KnowledgeEngine, global.KnowledgeControllerManager
	global.KnowledgeEngine = engine
	global.KnowledgeControllerManager = controllers.NewKnowledgeController(database.DB)
	defer func() { global.KnowledgeEngine = oldEngine; global.KnowledgeControllerManager = oldRefs }()
	g := &gin.Context{}
	value, err := (&CreateLibrary{Body: CreateLibraryInput{RequestID: "delete-library-create", Name: "永久删除", WikiLanguage: "zh-CN"}}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	id := value.(Receipt).ResourceID
	_, err = (&ImportSource{LibraryID: id, Body: ImportSourceInput{RequestID: "delete-library-import", Filename: "private.md", Bytes: base64.StdEncoding.EncodeToString([]byte("private original"))}}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	library, err := engine.GetLibrary(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.DB.Create(&models.KnowledgeTaskConfiguration{ID: "configuration", RuntimeID: "runtime", Backend: "local", ConnectionID: "local", KnowledgeBaseID: id}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = (&DeleteLibrary{LibraryID: id, Body: DeleteLibraryInput{RequestID: "stale-delete", ExpectedVersion: library.Version + 1}}).Output(g)
	if !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatal(err)
	}
	var refs []models.KnowledgeTaskConfiguration
	err = database.DB.Where("knowledge_base_id = ?", id).Find(&refs).Error
	if err != nil || len(refs) != 1 {
		t.Fatal(refs, err)
	}
	operation := &DeleteLibrary{LibraryID: id, Body: DeleteLibraryInput{RequestID: "confirmed-delete", ExpectedVersion: library.Version}}
	value, err = operation.Output(g)
	if err != nil {
		t.Fatal(err)
	}
	if value.(Receipt).State != "accepted" {
		t.Fatal(value)
	}
	if _, err = engine.GetLibrary(ctx, id); err == nil {
		t.Fatal("deleted library remains readable")
	}
	refs = nil
	err = database.DB.Where("knowledge_base_id = ?", id).Find(&refs).Error
	if err != nil || len(refs) != 0 {
		t.Fatal(refs, err)
	}
	statusValue, err := (&GetLibraryDeletion{LibraryID: id}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	status := statusValue.(*core.LibraryDeletionStatus)
	if status.State != "completed" || status.DeletedBlobCount != 1 || status.PendingBlobCount != 0 {
		t.Fatal(status)
	}
	if _, err = operation.Output(g); err != nil {
		t.Fatal("same request cannot replay after logical removal", err)
	}
}

func TestLibraryDeleteLeavesAllChatsToTheirOriginalLifecycle(t *testing.T) {
	ctx := context.Background()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "ally.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	defer sqlDB.Close()
	root := t.TempDir()
	layout, err := chatstorage.NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := chatservice.New(chatservice.Config{
		StorageDirectory: root, Projects: controllers.NewProjectController(database.DB),
		QueryIndexes: chatservice.QueryIndexDependencies{
			Chats: controllers.NewChatController(database.DB), RuntimeSessions: controllers.NewRuntimeSessionController(database.DB),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
			Transactions:    controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	oldEngine, oldRefs, oldChats, oldService := global.KnowledgeEngine, global.KnowledgeControllerManager, global.ChatService, global.KnowledgeService
	global.KnowledgeEngine, global.KnowledgeControllerManager, global.ChatService, global.KnowledgeService = engine, controllers.NewKnowledgeController(database.DB), manager, nil
	defer func() {
		global.KnowledgeEngine, global.KnowledgeControllerManager, global.ChatService, global.KnowledgeService = oldEngine, oldRefs, oldChats, oldService
	}()
	receipt, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "create", Name: "删除测试", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	library, err := engine.GetLibrary(ctx, receipt.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"11111111111111111111111111111111", "22222222222222222222222222222222", "33333333333333333333333333333333", "44444444444444444444444444444444"} {
		dir, err := layout.ChatWorkspace(id)
		if err != nil {
			t.Fatal(err)
		}
		if err = workspace.EnsureExecutionWorkspace(dir, id, "waker", nil); err != nil {
			t.Fatal(err)
		}
		if _, err = manager.CreateDormantChats(ctx, []chatservice.DormantChat{{Chat: models.Chat{ID: id, WakerID: "waker", WorkspaceDirectory: dir, AdditionalWorkingDirectories: []string{dir}}}}); err != nil {
			t.Fatal(err)
		}
		kb := library.ID
		if id == "44444444444444444444444444444444" {
			kb = "another-library"
		}
		if err = global.KnowledgeControllerManager.BeginOperation(ctx, models.KnowledgeOperationLink{RequestID: id, Operation: "chat.organize", Digest: id, Backend: "local", ConnectionID: "local", KnowledgeBaseID: kb, ChatID: id, ReceiptConfirmed: true}); err != nil {
			t.Fatal(err)
		}
	}

	operation := &DeleteLibrary{LibraryID: library.ID, Body: DeleteLibraryInput{RequestID: "delete", ExpectedVersion: library.Version}}
	if _, err = operation.Output(&gin.Context{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"11111111111111111111111111111111", "22222222222222222222222222222222", "33333333333333333333333333333333", "44444444444444444444444444444444"} {
		if _, err = manager.GetChatSummary(ctx, id); err != nil {
			t.Fatalf("unrelated chat removed: %s %v", id, err)
		}
	}
	if _, err = operation.Output(&gin.Context{}); err != nil {
		t.Fatalf("delete replay failed: %v", err)
	}
}

// deletionModel 模拟取消后仍返回正文的模型，验证删除屏障拒绝迟到成果。
type deletionModel struct {
	entered chan struct{} // 通知模型已进入生成。
	stopped chan struct{} // 通知模型已收到取消。
	release chan struct{} // 非空时延迟管线退出，验证请求中断后的恢复。
}

func (m *deletionModel) Generate(ctx context.Context, _ core.GenerateRequest) (core.GenerateResult, error) {
	select {
	case m.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	select {
	case m.stopped <- struct{}{}:
	default:
	}
	if m.release != nil {
		<-m.release
	}
	return core.GenerateResult{Text: "迟到模型结果"}, nil
}

func TestLibraryDeleteStopsActiveAndQueuedOrganize(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprint("running=", running), func(t *testing.T) {
			ctx := context.Background()
			engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "ally.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := database.DB.DB()
			defer sqlDB.Close()
			refs := controllers.NewKnowledgeController(database.DB)
			model := &deletionModel{entered: make(chan struct{}, 10), stopped: make(chan struct{}, 10)}
			service := knowledgeService.New(knowledgeService.Config{Engine: engine, References: refs,
				ResolveModel: func(_ context.Context, v models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
					return v, nil
				},
				Generator: func(context.Context, models.LibraryExecutionDefaults) (core.Generator, error) { return model, nil },
			})
			oldEngine, oldRefs, oldService := global.KnowledgeEngine, global.KnowledgeControllerManager, global.KnowledgeService
			global.KnowledgeEngine, global.KnowledgeControllerManager, global.KnowledgeService = engine, refs, service
			defer func() {
				global.KnowledgeEngine, global.KnowledgeControllerManager, global.KnowledgeService = oldEngine, oldRefs, oldService
			}()
			created, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "create", Name: "删除中的库", Kind: "general", WikiLanguage: "zh-CN", IndexingStrategy: &core.IndexingStrategy{WikiEnabled: true}})
			if err != nil {
				t.Fatal(err)
			}
			jobs := []string{}
			for i := 0; i < 2; i++ {
				source, err := engine.ImportSource(ctx, core.ImportSourceInput{RequestID: fmt.Sprint("source", i), LibraryID: created.LibraryID, Filename: fmt.Sprint(i, ".md"), Bytes: []byte(fmt.Sprint("# 真实来源资料\n知识库删除需要先停止所有整理任务。这里保存真实来源、处理规则和原始资料，正在生成的模型调用必须收到取消，迟到结果不能恢复已删除的内容。", i))})
				if err != nil {
					t.Fatal(err)
				}
				receipt, _, err := service.Organize(ctx, knowledgeService.OrganizeCommand{RequestID: fmt.Sprint("job", i), LibraryID: created.LibraryID, SourceVersionIDs: []string{source.ResourceID}, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: "codex"}})
				if err != nil {
					t.Fatal(err)
				}
				jobs = append(jobs, receipt.JobID)
			}
			if running {
				model.release = make(chan struct{})
				if err := service.Start(ctx); err != nil {
					t.Fatal(err)
				}
				defer service.Close()
				defer func() {
					select {
					case <-model.release:
					default:
						close(model.release)
					}
				}()
				select {
				case <-model.entered:
				case <-time.After(5 * time.Second):
					states, _, _ := engine.ListJobs(ctx, created.LibraryID, 0, 10)
					stages, _ := engine.ListStages(ctx, states[0].ID)
					sources, _, _ := engine.ListSources(ctx, created.LibraryID, 0, 10)
					t.Fatalf("model did not start: stages=%+v source=%+v", stages, sources[0])
				}
			}
			lib, err := engine.GetLibrary(ctx, created.LibraryID)
			if err != nil {
				t.Fatal(err)
			}
			stale := &DeleteLibrary{LibraryID: lib.ID, Body: DeleteLibraryInput{RequestID: "stale", ExpectedVersion: lib.Version + 1}}
			if _, err := stale.Output(&gin.Context{}); !errors.Is(err, status_error.KnowledgeConflict) {
				t.Fatalf("stale delete: %v", err)
			}
			for _, id := range jobs {
				job, err := engine.GetJob(ctx, id)
				if err != nil || job.CancelRequested {
					t.Fatalf("stale delete cancelled job: %+v %v", job, err)
				}
			}
			operation := &DeleteLibrary{LibraryID: lib.ID, Body: DeleteLibraryInput{RequestID: "delete", ExpectedVersion: lib.Version}}
			if running {
				requestCtx, cancelRequest := context.WithCancel(ctx)
				stopped := make(chan error, 1)
				go func() {
					_, err := operation.Output(&gin.Context{Request: (&http.Request{}).WithContext(requestCtx)})
					stopped <- err
				}()
				select {
				case <-model.stopped:
				case <-time.After(5 * time.Second):
					cancelRequest()
					t.Fatal("model not cancelled")
				}
				cancelRequest()
				select {
				case err := <-stopped:
					if !errors.Is(err, status_error.KnowledgeCancelled) {
						t.Fatalf("cancelled request: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("deletion wait ignored request cancellation")
				}
				close(model.release)
			}
			if _, err := operation.Output(&gin.Context{}); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.GetLibrary(ctx, lib.ID); err == nil {
				t.Fatal("library remains")
			}
			for _, id := range jobs {
				if _, err := engine.GetJob(ctx, id); err == nil {
					t.Fatal("job remains")
				}
			}
			status, err := engine.GetLibraryDeletion(ctx, lib.ID)
			if err != nil || status.State != "completed" || status.DeletedBlobCount != 2 {
				t.Fatalf("cleanup: %+v %v", status, err)
			}
			if _, err := operation.Output(&gin.Context{}); err != nil {
				t.Fatalf("replay: %v", err)
			}
		})
	}
}
