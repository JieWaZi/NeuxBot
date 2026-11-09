package workspace

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	managed "github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/internal/workspacefiles"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestOpenChatScopeEndpointsKeepPrivateFilesSeparateAndReadActualProjects(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB.DB()
	defer pool.Close()
	previousChat, previousManaged := global.ChatService, global.Config.ManagedFilesDirectory
	defer func() { global.ChatService = previousChat; global.Config.ManagedFilesDirectory = previousManaged }()
	global.Config.ManagedFilesDirectory = t.TempDir()
	service, err := chatservice.New(chatservice.Config{StorageDirectory: global.Config.ManagedFilesDirectory, Projects: controllers.NewProjectController(database.DB), QueryIndexes: chatservice.QueryIndexDependencies{Chats: controllers.NewChatController(database.DB), DeletionIntents: controllers.NewChatDeletionIntentController(database.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	global.ChatService = service
	layout, err := chatstorage.NewLayout(global.Config.ManagedFilesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := layout.AgentWorkspace("waker")
	if err != nil {
		t.Fatal(err)
	}
	if err = managed.EnsureAgentWorkspace(cwd, "waker"); err != nil {
		t.Fatal(err)
	}
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(project, "report.txt"), []byte("real project"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("ally.auth.subject", "desktop")
	scopes := []workspacefiles.Scope{}
	for _, id := range []string{"first", "second"} {
		if _, err = service.CreateDormantChats(ctx, []chatservice.DormantChat{{Chat: models.Chat{ID: id, WakerID: "waker", WorkspaceDirectory: cwd, AdditionalWorkingDirectories: []string{project}}}}); err != nil {
			t.Fatal(err)
		}
		private, _ := layout.ChatRoot(id)
		if err = os.MkdirAll(filepath.Join(private, "attachments"), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(private, "attachments", "input.txt"), []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
		value, err := (&OpenScope{Body: ScopeRequest{ChatID: id}}).Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		scope := value.(workspacefiles.Scope)
		defer files.Close("desktop", scope.ID)
		scopes = append(scopes, scope)
		if scope.Root != cwd {
			t.Fatalf("public root is not real cwd: %s", scope.Root)
		}
		value, err = (&ReadFile{ScopeID: scope.ID, Root: project, RelativePath: "report.txt"}).Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		content, err := base64.StdEncoding.DecodeString(value.(ReadResponse).BytesBase64)
		if err != nil || string(content) != "real project" {
			t.Fatalf("preview/download content: %q %v", content, err)
		}
		if _, err = (&ReadFile{ScopeID: scope.ID, Root: private, RelativePath: "log"}).Output(ctx); err == nil {
			t.Fatal("private log granted as file root")
		}
		value, err = (&ResolveReference{Body: ResolveReferenceRequest{ScopeID: scope.ID, Path: filepath.Join(project, "report.txt")}}).Output(ctx)
		if err != nil || value.(workspacefiles.Resolution).Status != "resolved" {
			t.Fatalf("project reference: %+v %v", value, err)
		}
	}
	if scopes[0].ID == scopes[1].ID {
		t.Fatal("OpenScope reused another Chat identity")
	}
	if _, err = (&DeleteChatFile{ScopeID: scopes[0].ID, Root: cwd, RelativePath: "attachments/input.txt"}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = (&ReadFile{ScopeID: scopes[1].ID, Root: cwd, RelativePath: "attachments/input.txt"}).Output(ctx); err != nil {
		t.Fatal("other Chat attachment removed", err)
	}
	second, _ := layout.ChatRoot("second")
	if _, _, err = files.Read("desktop", scopes[0].ID, filepath.Join(second, "attachments"), "input.txt"); err != status_error.WorkspacePathInvalid {
		t.Fatalf("foreign attachment authorized: %v", err)
	}
}
