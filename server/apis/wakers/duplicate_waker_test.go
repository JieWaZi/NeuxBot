package wakers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/memory"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	storageservice "github.com/JieWaZi/NeuxBot/server/services/storage"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestDuplicateWakerCopiesOwnedResourcesAndReplays(t *testing.T) {
	root := t.TempDir()
	previousConfig := global.Config
	previousSkills, previousMemory, previousChats := global.SkillControllerManager, global.MemoryService, global.ChatControllerManager
	config := *previousConfig
	config.ManagedFilesDirectory = root
	global.Config = &config
	memories, err := memory.NewService(memory.ServiceConfig{DBPath: filepath.Join(t.TempDir(), "memory.db")})
	if err != nil {
		t.Fatal(err)
	}
	global.MemoryService = memories
	t.Cleanup(func() {
		memories.Close()
		global.Config, global.SkillControllerManager, global.MemoryService, global.ChatControllerManager = previousConfig, previousSkills, previousMemory, previousChats
	})
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "wakers.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	oldWakers, oldProfiles, oldTransactions := global.WakerControllerManager, global.ProfileDocumentControllerManager, global.TransactionControllerManager
	wakers := controllers.NewWakerController(database.DB)
	profiles := controllers.NewProfileDocumentController(database.DB)
	global.WakerControllerManager, global.ProfileDocumentControllerManager = wakers, profiles
	skills := controllers.NewSkillController(database.DB)
	global.SkillControllerManager = skills
	global.ChatControllerManager = controllers.NewChatController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	t.Cleanup(func() {
		global.WakerControllerManager, global.ProfileDocumentControllerManager, global.TransactionControllerManager = oldWakers, oldProfiles, oldTransactions
	})

	source := models.Waker{
		ID: "source", Name: "Reviewer", RoleName: "Editor", Bio: "Review drafts", Avatar: "avatar",
		OnboardingStatus: constants.WakerOnboardingStatusReady, OnboardingChatID: "original-chat",
		Expertise: []profile.Expertise{{Title: "Review"}},
		WorkStyle: []string{"Concise"}, WorkMethods: []profile.WorkMethod{{Name: "Review", Steps: []string{"Check evidence"}}},
		DefaultModel: &models.WakerDefaultModel{RuntimeID: "codex", AccountID: "system", Model: "sample"},
	}
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest("POST", "/", nil)
	if err := wakers.Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := global.TransactionControllerManager.Transaction(ctx, func(tx context.Context) error {
		return wakers.MoveToNavigationGroup(tx, source, "Reviewers")
	}); err != nil {
		t.Fatal(err)
	}
	const content = "# Working agreement\n\nReview drafts."
	if err := profiles.SaveWakerAgents(ctx, source.ID, content, 1); err != nil {
		t.Fatal(err)
	}
	layout, err := chatstorage.NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceWorkspace, err := layout.AgentWorkspace(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureAgentWorkspace(sourceWorkspace, source.ID); err != nil {
		t.Fatal(err)
	}
	file := "review-notes.txt"
	if err := os.WriteFile(filepath.Join(sourceWorkspace, file), []byte("Review carefully"), 0o600); err != nil {
		t.Fatal(err)
	}
	written, err := memories.Write(ctx, memory.Context{PartnerID: source.ID}, "Prefer primary sources.")
	if err != nil {
		t.Fatal(err)
	}
	for index, skillID := range []string{"enable-skill", "disable-skill"} {
		if err := skills.CreatePackage(ctx, models.SkillPackage{ID: skillID, Name: skillID}); err != nil {
			t.Fatal(err)
		}
		if err := skills.UpsertWakerSkill(ctx, models.WakerSkill{
			ID: "relation-" + skillID, WakerID: source.ID, SkillPackageID: skillID, Enabled: index == 0,
		}); err != nil {
			t.Fatal(err)
		}
	}
	op := &DuplicateWaker{WakerID: source.ID, Body: DuplicateWakerRequest{RequestID: "copy-one"}}
	conversations, err := conversation.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	chatModule, err := chatservice.New(chatservice.Config{
		StorageDirectory: root, Conversations: conversations, Projects: controllers.NewProjectController(database.DB),
		QueryIndexes: chatservice.QueryIndexDependencies{
			Chats: global.ChatControllerManager, Transactions: global.TransactionControllerManager,
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chatModule.Close)
	otherWorkspace, err := layout.AgentWorkspace("other")
	if err != nil {
		t.Fatal(err)
	}
	created, err := json.Marshal(eventlog.ChatCreatedData{
		WakerID: "other", WorkspaceDirectory: otherWorkspace, AdditionalWorkingDirectories: []string{sourceWorkspace},
	})
	if err != nil {
		t.Fatal(err)
	}
	const occupiedChatID = "97000000000000000000000000000000"
	if _, err = conversations.Commit(t.Context(), agent.ConversationBatch{
		ChatID: occupiedChatID, Key: "create-occupied-chat",
		Changes: []agent.ConversationChange{{Fact: &agent.ChatFact{Type: constants.ChatEventTypeCreated, Data: created}}},
	}); err != nil {
		t.Fatal(err)
	}
	occupied := []string{occupiedChatID}
	storage := storageservice.New(t.Context(), storageservice.Config{
		PolicyDirectory: root,
		CheckFileIdle: func(ctx context.Context, wakerID, _ string) error {
			return chatModule.CheckOccupiedWorkspace(ctx, wakerID, occupied)
		},
	})
	t.Cleanup(storage.Close)
	previousStorage := global.StorageService
	global.StorageService = storage
	t.Cleanup(func() { global.StorageService = previousStorage })
	if _, err := op.Output(ctx); !util.HasStatusCode(err, status_error.WakerHasRunningWork) {
		t.Fatalf("copy during overlapping work = %v, want WakerHasRunningWork", err)
	}
	occupied = nil
	first, err := op.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := first.(DuplicateWakerResponse).WakerID
	replay, err := op.Output(ctx)
	if err != nil || replay != first {
		t.Fatalf("copy replay = %v, %v", replay, err)
	}
	copy, err := wakers.Get(ctx, id)
	if err != nil || copy.ID == source.ID || copy.Name != "Reviewer (2)" ||
		copy.OnboardingStatus != constants.WakerOnboardingStatusReady || copy.OnboardingChatID != "" ||
		copy.RoleName != source.RoleName || copy.Bio != source.Bio || copy.Avatar != source.Avatar ||
		copy.DefaultModel == nil || *copy.DefaultModel != *source.DefaultModel ||
		len(copy.Expertise) != 1 || len(copy.WorkStyle) != 1 || len(copy.WorkMethods) != 1 {
		t.Fatalf("copied partner = %+v, %v", copy, err)
	}
	doc, err := profiles.GetWakerDocument(ctx, id, constants.ProfileDocumentKindAgents)
	if err != nil || doc.Content != content {
		t.Fatalf("copied profile = %+v, %v", doc, err)
	}
	groups, _, err := wakers.ListNavigationGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].Name != "Reviewers" || len(groups[0].WakerIDs) != 2 ||
		groups[0].WakerIDs[0] != source.ID || groups[0].WakerIDs[1] != id {
		t.Fatalf("copied group = %+v, %v", groups, err)
	}
	copyWorkspace, err := layout.AgentWorkspace(id)
	if err != nil {
		t.Fatal(err)
	}
	fileContent, err := os.ReadFile(filepath.Join(copyWorkspace, file))
	if err != nil || string(fileContent) != "Review carefully" {
		t.Errorf("copied workspace file = %q, %v", fileContent, err)
	}
	copyMemory, err := memories.ListMemories(ctx, memory.ListFilter{PartnerID: id, Limit: 10})
	if err != nil || len(copyMemory.Items) != 1 || copyMemory.Items[0].ID == written.Memory.ID ||
		copyMemory.Items[0].Data != "Prefer primary sources." {
		t.Errorf("copied memories = %+v, %v", copyMemory, err)
	}
	for index, skillID := range []string{"enable-skill", "disable-skill"} {
		skill, err := skills.GetInstalled(ctx, id, skillID)
		if err != nil || skill.Enabled != (index == 0) {
			t.Errorf("copied skill %s = %+v, %v", skillID, skill, err)
		}
	}
	op.Body.RequestID = "copy-two"
	second, err := op.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	copy, err = wakers.Get(ctx, second.(DuplicateWakerResponse).WakerID)
	if err != nil || copy.Name != "Reviewer (3)" {
		t.Fatalf("second copied name = %q, %v", copy.Name, err)
	}

	// 注入业务库最后一步失败，确保各存储无可访问的半成品且可用同一请求重试。
	op.Body.RequestID = "copy-fails"
	failedIdentity, err := requestidempotency.New(op.Body.RequestID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	failedID := failedIdentity.ResourceID("waker-copy")
	global.SkillControllerManager = &failCopiedSkillWrite{SkillController: skills}
	if _, err := op.Output(ctx); !util.HasStatusCode(err, status_error.DatabaseError) {
		t.Fatalf("copy failure = %v", err)
	}
	global.SkillControllerManager = skills
	if _, err := wakers.Get(ctx, failedID); !util.HasStatusCode(err, status_error.WakerNotFound) {
		t.Fatalf("failed copy still visible: %v", err)
	}
	failedWorkspace, err := layout.AgentWorkspace(failedID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(failedWorkspace); !os.IsNotExist(err) {
		t.Fatalf("failed copy still has a workspace: %v", err)
	}
	failedMemory, err := memories.ListMemories(ctx, memory.ListFilter{PartnerID: failedID, Limit: 10})
	if err != nil || failedMemory.Total != 0 {
		t.Fatalf("failed copy still has memory: %+v, %v", failedMemory, err)
	}
	retried, err := op.Output(ctx)
	if err != nil || retried != (DuplicateWakerResponse{WakerID: failedID}) {
		t.Fatalf("retry after rollback = %v, %v", retried, err)
	}
}

type failCopiedSkillWrite struct {
	// SkillController 先实际写入技能关系，再让业务事务触发回滚。
	controllers.SkillController
}

func (c *failCopiedSkillWrite) DuplicateWakerSkills(ctx context.Context, sourceID, targetID string) error {
	if err := c.SkillController.DuplicateWakerSkills(ctx, sourceID, targetID); err != nil {
		return err
	}
	return status_error.DatabaseError
}
