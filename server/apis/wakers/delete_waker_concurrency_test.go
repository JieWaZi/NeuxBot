package wakers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	keyrules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// emptyWakerChats 只省略本测试不存在的聊天文件，其他删除使用真实仓库。
type emptyWakerChats struct {
	// Module 补齐与本次伙伴删除无关的聊天能力。
	chatservice.Module
}

// DeleteWakerChats 表示当前伙伴没有需要删除的聊天文件。
func (emptyWakerChats) DeleteWakerChats(context.Context, string) error { return nil }

func TestDeleteWakerCompletesAlongsideIntegrationKnowledgePublish(t *testing.T) {
	ctx := context.Background()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB.DB()
	defer pool.Close()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	receipt, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "library", Name: "知识", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	library, _ := engine.GetLibrary(ctx, receipt.ResourceID)
	keys := controllers.NewIntegrationKeyController(database.DB)
	if _, _, err = keys.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: "key", Digest: "fixture", KeyID: "key", Action: "create"}); err != nil {
		t.Fatal(err)
	}
	_, err = keys.CompleteMutation(ctx, "key", "fixture", models.IntegrationKey{ID: "key", Name: "集成", KeyHash: keyrules.HashKey("fixture"), Enabled: true, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{library.ID}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	authority := integrationservice.New(integrationservice.Config{Keys: keys})
	identity, err := authority.Authenticate(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := authority.Authorize(ctx, identity, constants.IntegrationResourceKindKnowledge, library.ID, []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(integrationservice.KnowledgeRecovery{Origin: "direct_api", Subject: "key", Authorization: auth, Permission: core.PermissionSnapshot{PolicyVersion: library.PolicyVersion, Scope: []string{}, Operations: []string{"organize"}}})
	guardHeld := make(chan func(), 1)
	resumePublish := make(chan struct{})
	service := knowledgeservice.New(knowledgeservice.Config{Engine: engine, References: controllers.NewKnowledgeController(database.DB), ResolveModel: func(_ context.Context, in models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
		in.RuntimeID = constants.RuntimeIDCodex
		return in, nil
	}, IntegrationGuard: func(ctx context.Context, _ json.RawMessage) (func(), error) {
		release, err := authority.AcquireGuard(ctx, auth)
		if err == nil {
			guardHeld <- release
			<-resumePublish
		}
		return release, err
	}})
	previousWakers, previousProjects, previousSkills := global.WakerControllerManager, global.ProjectControllerManager, global.SkillControllerManager
	previousDocuments, previousConnectors, previousTasks := global.ProfileDocumentControllerManager, global.MCPConnectorControllerManager, global.AutomaticTaskControllerManager
	previousTransactions, previousKnowledge, previousAuthority := global.TransactionControllerManager, global.KnowledgeService, global.IntegrationService
	previousChat, previousSchedule, previousChannel, previousPlugin, previousMemory := global.ChatService, global.ScheduleService, global.ChannelService, global.PluginControllerManager, global.MemoryService
	defer func() {
		global.WakerControllerManager, global.ProjectControllerManager, global.SkillControllerManager = previousWakers, previousProjects, previousSkills
		global.ProfileDocumentControllerManager, global.MCPConnectorControllerManager, global.AutomaticTaskControllerManager = previousDocuments, previousConnectors, previousTasks
		global.TransactionControllerManager, global.KnowledgeService, global.IntegrationService = previousTransactions, previousKnowledge, previousAuthority
		global.ChatService, global.ScheduleService, global.ChannelService, global.PluginControllerManager, global.MemoryService = previousChat, previousSchedule, previousChannel, previousPlugin, previousMemory
	}()
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ProjectControllerManager = controllers.NewProjectController(database.DB)
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.ProfileDocumentControllerManager = controllers.NewProfileDocumentController(database.DB)
	global.MCPConnectorControllerManager = controllers.NewMCPConnectorController(database.DB)
	global.AutomaticTaskControllerManager = authority.CoordinateTasks(controllers.NewAutomaticTaskController(database.DB))
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	global.KnowledgeService, global.IntegrationService = service, authority
	global.ChatService = emptyWakerChats{}
	global.ScheduleService, global.ChannelService, global.PluginControllerManager, global.MemoryService = nil, nil, nil, nil
	if err = global.WakerControllerManager.Create(ctx, models.Waker{ID: "waker", Name: "伙伴"}); err != nil {
		t.Fatal(err)
	}
	publishDone := make(chan error, 1)
	go func() {
		_, _, err := service.Organize(ctx, knowledgeservice.OrganizeCommand{RequestID: "publish", LibraryID: library.ID, Configuration: models.KnowledgeTaskConfiguration{RemoteSubject: "key", RemoteAuthorization: raw}})
		publishDone <- err
	}()
	var rescue func()
	select {
	case rescue = <-guardHeld:
	case err = <-publishDone:
		t.Fatalf("publication did not reach guard: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("publication did not reach guard")
	}
	defer rescue()
	deleteDone := make(chan error, 1)
	apiContext, _ := gin.CreateTestContext(nil)
	go func() {
		_, err := (&DeleteWaker{WakerID: "waker", Body: DeleteWakerRequest{ConfirmationName: "伙伴"}}).Output(apiContext)
		deleteDone <- err
	}()
	// 等到删除实际进入授权锁等待，再允许发布继续争用知识锁。
	deadline := time.Now().Add(5 * time.Second)
	for {
		stack := make([]byte, 1<<20)
		n := runtime.Stack(stack, true)
		waiting := false
		for _, goroutine := range strings.Split(string(stack[:n]), "\n\n") {
			if strings.Contains(goroutine, "(*DeleteWaker).Output") && strings.Contains(goroutine, "(*Service).WithAuthorizationWrite") {
				waiting = true
				break
			}
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			rescue()
			close(resumePublish)
			t.Fatal("deletion did not enter authorization coordination")
		}
		runtime.Gosched()
	}
	close(resumePublish)
	select {
	case err = <-publishDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		// 失败时释放原集成读锁，使两个真实调用结束并保留清晰断言。
		rescue()
		<-publishDone
		<-deleteDone
		t.Fatal("deletion and knowledge publication deadlocked on opposite lock order")
	}
	select {
	case err = <-deleteDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deletion did not finish after publication")
	}
	if _, err = global.WakerControllerManager.Get(ctx, "waker"); !util.HasStatusCode(err, status_error.WakerNotFound) {
		t.Fatalf("waker deletion not persisted: %v", err)
	}
}
