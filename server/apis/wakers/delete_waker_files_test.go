package wakers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// failingWakerDelete 保留真实查询，只在事务最终删除伙伴时注入失败。
type failingWakerDelete struct {
	// WakerController 提供真实的伙伴查询与其他持久化操作。
	controllers.WakerController
}

func (f failingWakerDelete) Delete(context.Context, string) error { return status_error.DatabaseError }

func TestDeleteWakerOwnsAgentFilesWithoutAnyChatsAndRollsBackDatabaseFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[fail], func(t *testing.T) {
			database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "api.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			pool, _ := database.DB.DB()
			t.Cleanup(func() { _ = pool.Close() })
			previousWakers, previousProjects, previousSkills := global.WakerControllerManager, global.ProjectControllerManager, global.SkillControllerManager
			previousDocuments, previousConnectors, previousChats := global.ProfileDocumentControllerManager, global.MCPConnectorControllerManager, global.ChatService
			previousTransactions, previousTasks, previousPlugin := global.TransactionControllerManager, global.AutomaticTaskControllerManager, global.PluginControllerManager
			previousKnowledge, previousIntegration, previousMemory := global.KnowledgeService, global.IntegrationService, global.MemoryService
			previousSchedule, previousChannel, previousThreads := global.ScheduleService, global.ChannelService, global.AgentThreads
			previousManaged := global.Config.ManagedFilesDirectory
			t.Cleanup(func() {
				global.WakerControllerManager, global.ProjectControllerManager, global.SkillControllerManager = previousWakers, previousProjects, previousSkills
				global.ProfileDocumentControllerManager, global.MCPConnectorControllerManager, global.ChatService = previousDocuments, previousConnectors, previousChats
				global.TransactionControllerManager, global.AutomaticTaskControllerManager, global.PluginControllerManager = previousTransactions, previousTasks, previousPlugin
				global.KnowledgeService, global.IntegrationService, global.MemoryService = previousKnowledge, previousIntegration, previousMemory
				global.ScheduleService, global.ChannelService, global.AgentThreads = previousSchedule, previousChannel, previousThreads
				global.Config.ManagedFilesDirectory = previousManaged
			})
			global.Config.ManagedFilesDirectory = t.TempDir()
			global.WakerControllerManager = controllers.NewWakerController(database.DB)
			global.ProjectControllerManager = controllers.NewProjectController(database.DB)
			global.SkillControllerManager = controllers.NewSkillController(database.DB)
			global.ProfileDocumentControllerManager = controllers.NewProfileDocumentController(database.DB)
			global.MCPConnectorControllerManager = controllers.NewMCPConnectorController(database.DB)
			global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
			global.AutomaticTaskControllerManager, global.PluginControllerManager = nil, nil
			global.KnowledgeService, global.IntegrationService, global.MemoryService = nil, nil, nil
			global.ScheduleService, global.ChannelService, global.AgentThreads = nil, nil, nil
			service, err := chatservice.New(chatservice.Config{StorageDirectory: global.Config.ManagedFilesDirectory, Projects: global.ProjectControllerManager, QueryIndexes: chatservice.QueryIndexDependencies{Chats: controllers.NewChatController(database.DB), DeletionIntents: controllers.NewChatDeletionIntentController(database.DB), Transactions: global.TransactionControllerManager}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(service.Close)
			global.ChatService = service
			ctx, _ := gin.CreateTestContext(nil)
			if err = global.WakerControllerManager.Create(ctx, models.Waker{ID: "owned", Name: "伙伴"}); err != nil {
				t.Fatal(err)
			}
			layout, err := chatstorage.NewLayout(global.Config.ManagedFilesDirectory)
			if err != nil {
				t.Fatal(err)
			}
			cwd, err := layout.AgentWorkspace("owned")
			if err != nil {
				t.Fatal(err)
			}
			if err = workspace.EnsureAgentWorkspace(cwd, "owned"); err != nil {
				t.Fatal(err)
			}
			report := filepath.Join(cwd, "report.txt")
			if err = os.WriteFile(report, []byte("outcome"), 0600); err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(t.TempDir(), "project.txt")
			if err = os.WriteFile(external, []byte("project"), 0600); err != nil {
				t.Fatal(err)
			}
			if fail {
				global.WakerControllerManager = failingWakerDelete{global.WakerControllerManager}
			}
			_, err = (&DeleteWaker{WakerID: "owned", Body: DeleteWakerRequest{ConfirmationName: "伙伴"}}).Output(ctx)
			if fail {
				if err == nil {
					t.Fatal("database failure hidden")
				}
				if content, readErr := os.ReadFile(report); readErr != nil || string(content) != "outcome" {
					t.Fatalf("files not restored: %q %v", content, readErr)
				}
				if _, getErr := global.WakerControllerManager.Get(ctx, "owned"); getErr != nil {
					t.Fatal(getErr)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, statErr := os.Stat(cwd); !os.IsNotExist(statErr) {
					t.Fatalf("owned Agent remains: %v", statErr)
				}
			}
			if content, err := os.ReadFile(external); err != nil || string(content) != "project" {
				t.Fatalf("external project changed: %q %v", content, err)
			}
		})
	}
}
