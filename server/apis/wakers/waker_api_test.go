package wakers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestDeleteWakerRejectsDefaultWaker(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	_, err := (&DeleteWaker{
		WakerID: builtinrolepreset.PersonalAssistantWakerID,
		Body: DeleteWakerRequest{
			ConfirmationName: "NeuxBot",
		},
	}).Output(ctx)
	if !errors.Is(err, status_error.DefaultWakerCannotDelete) {
		t.Fatalf("DeleteWaker.Output() error = %v, want DefaultWakerCannotDelete", err)
	}
}

func TestWakerAPICopiesPresetAndMaintainsIndependentDocuments(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "waker-api.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	previousWakers := global.WakerControllerManager
	previousPresets := global.RolePresetControllerManager
	previousDocuments := global.ProfileDocumentControllerManager
	previousProjects := global.ProjectControllerManager
	previousChats := global.ChatControllerManager
	previousSkills := global.SkillControllerManager
	previousMCPConnectors := global.MCPConnectorControllerManager
	previousTransactions := global.TransactionControllerManager
	previousDataDirectory := global.Config.DataDirectory
	previousChatService := global.ChatService
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.RolePresetControllerManager = previousPresets
		global.ProfileDocumentControllerManager = previousDocuments
		global.ProjectControllerManager = previousProjects
		global.ChatControllerManager = previousChats
		global.SkillControllerManager = previousSkills
		global.MCPConnectorControllerManager = previousMCPConnectors
		global.TransactionControllerManager = previousTransactions
		global.Config.DataDirectory = previousDataDirectory
		global.ChatService = previousChatService
	})

	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.RolePresetControllerManager = controllers.NewRolePresetController(database.DB)
	global.ProfileDocumentControllerManager = controllers.NewProfileDocumentController(database.DB)
	global.ProjectControllerManager = controllers.NewProjectController(database.DB)
	global.ChatControllerManager = controllers.NewChatController(database.DB)
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.MCPConnectorControllerManager = controllers.NewMCPConnectorController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	global.Config.DataDirectory = t.TempDir()
	chatLogDirectory := filepath.Join(t.TempDir(), constants.ChatLogDirectoryName)
	createdChatService, err := chatservice.New(chatservice.Config{
		StorageDirectory: chatLogDirectory,
		Projects:         global.ProjectControllerManager,
		QueryIndexes: chatservice.QueryIndexDependencies{
			Chats:           global.ChatControllerManager,
			RuntimeSessions: controllers.NewRuntimeSessionController(database.DB),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
			Transactions:    global.TransactionControllerManager,
		},
	})
	if err != nil {
		t.Fatalf("create chat service: %v", err)
	}
	global.ChatService = createdChatService

	ctx, _ := gin.CreateTestContext(nil)
	now := time.Now().UnixMilli()
	preset := models.RolePreset{
		ID:            util.New(),
		RoleName:      "产品设计师",
		DefaultAvatar: constants.PersonalAssistantAvatar,
		Description:   "从预设复制的描述",
		Source:        constants.RolePresetSourceUser,
		Expertise: []profile.Expertise{{
			Title:       "界面设计",
			Description: "设计清晰一致的产品界面",
		}},
		WorkStyle:   []string{"先验证"},
		WorkMethods: []profile.WorkMethod{{Name: "设计", Steps: []string{"分析"}}},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := global.RolePresetControllerManager.Create(ctx, preset); err != nil {
		t.Fatalf("create preset: %v", err)
	}
	presetContents := map[constants.ProfileDocumentKind]string{
		constants.ProfileDocumentKindIdentity: "## 擅长领域\n\n### 界面设计\n\n设计清晰一致的产品界面",
		constants.ProfileDocumentKindPersona:  "## 工作风格\n\n- 先验证",
		constants.ProfileDocumentKindPlaybook: "## 工作方式\n\n### 设计\n\n1. 分析",
	}
	presetDocuments := make([]models.RolePresetDocument, 0, 3)
	for _, kind := range profile.EditableKinds() {
		presetDocuments = append(presetDocuments, models.RolePresetDocument{
			ID:           util.New(),
			RolePresetID: preset.ID,
			Kind:         kind,
			Content:      presetContents[kind],
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	if err := global.ProfileDocumentControllerManager.CreateRolePresetDocuments(
		ctx,
		presetDocuments,
	); err != nil {
		t.Fatalf("create preset documents: %v", err)
	}

	create := CreateWaker{
		Body: CreateWakerRequest{
			RolePresetID: preset.ID,
			Name:         " Alice ",
			Avatar:       "",
			Bio:          "独立简介",
		},
	}
	if _, err := create.Output(ctx); err != nil {
		t.Fatalf("create waker output: %v", err)
	}
	wakers, err := global.WakerControllerManager.List(
		context.Background(),
		"",
		10,
		0,
	)
	if err != nil {
		t.Fatalf("list wakers: %v", err)
	}
	if len(wakers) != 1 {
		t.Fatalf("waker count = %d, want 1", len(wakers))
	}
	waker := wakers[0]
	if waker.Name != "Alice" || waker.RoleName != preset.RoleName {
		t.Fatalf("unexpected copied waker: %+v", waker)
	}
	if waker.Bio != "独立简介" ||
		!strings.HasPrefix(waker.Avatar, constants.CompanionAvatarPrefix) ||
		len(waker.Avatar) != len(constants.CompanionAvatarPrefix)+32 {
		t.Fatalf("unexpected user metadata: %+v", waker)
	}
	if !reflect.DeepEqual(waker.Expertise, []profile.Expertise{{
		Title:       "界面设计",
		Description: "设计清晰一致的产品界面",
	}}) ||
		!reflect.DeepEqual(waker.WorkStyle, []string{"先验证"}) ||
		!reflect.DeepEqual(
			waker.WorkMethods,
			[]profile.WorkMethod{{Name: "设计", Steps: []string{"分析"}}},
		) {
		t.Fatalf("unexpected copied profile projection: %+v", waker)
	}

	createWithEmptyBio := CreateWaker{
		Body: CreateWakerRequest{
			RolePresetID: preset.ID,
			Name:         "Bob",
			Avatar:       "",
			Bio:          "",
		},
	}
	if _, err := createWithEmptyBio.Output(ctx); err != nil {
		t.Fatalf("create waker with empty bio output: %v", err)
	}
	inheritedWakers, err := global.WakerControllerManager.List(
		context.Background(),
		"Bob",
		10,
		0,
	)
	if err != nil {
		t.Fatalf("list inherited wakers: %v", err)
	}
	if len(inheritedWakers) != 1 {
		t.Fatalf("inherited waker count = %d, want 1", len(inheritedWakers))
	}
	if inheritedWakers[0].Avatar == waker.Avatar || inheritedWakers[0].Avatar == preset.DefaultAvatar {
		t.Fatal("partners created from one preset must have independent appearances")
	}
	if inheritedWakers[0].Bio != preset.Description {
		t.Fatalf(
			"empty bio = %q, want inherited %q",
			inheritedWakers[0].Bio,
			preset.Description,
		)
	}

	documents, err := global.ProfileDocumentControllerManager.ListWakerDocuments(
		ctx,
		waker.ID,
	)
	if err != nil {
		t.Fatalf("list waker documents: %v", err)
	}
	if len(documents) != 3 {
		t.Fatalf("document count = %d, want 3", len(documents))
	}
	listOutput, err := (&ListWakerDocuments{WakerID: waker.ID}).Output(ctx)
	if err != nil {
		t.Fatalf("list waker document output: %v", err)
	}
	listedDocuments, ok := listOutput.([]profile.Document)
	if !ok {
		t.Fatalf("document output type = %T, want []profile.Document", listOutput)
	}
	wantKinds := []constants.ProfileDocumentKind{
		constants.ProfileDocumentKindIdentity,
		constants.ProfileDocumentKindPersona,
		constants.ProfileDocumentKindPlaybook,
	}
	for index, wantKind := range wantKinds {
		if listedDocuments[index].Kind != wantKind {
			t.Fatalf(
				"document kind at %d = %q, want %q",
				index,
				listedDocuments[index].Kind,
				wantKind,
			)
		}
	}
	updateProfile := UpdateWaker{
		WakerID: waker.ID,
		Body: UpdateWakerRequest{
			Name:     waker.Name,
			RoleName: "用户研究员",
			Avatar:   "",
			Bio:      "更新后的独立简介",
			Manual: profile.ManualInput{
				Expertise: []profile.Expertise{{
					Title:       "用户研究",
					Description: "通过访谈与观察识别真实需求",
				}},
				WorkStyle: []string{"先访谈再验证"},
				WorkMethods: []profile.WorkMethod{
					{Name: "研究流程", Steps: []string{"准备问题", "整理结论"}},
				},
			},
		},
	}
	if _, err := updateProfile.Output(ctx); err != nil {
		t.Fatalf("update waker profile output: %v", err)
	}
	manuallyUpdatedWaker, err := global.WakerControllerManager.Get(ctx, waker.ID)
	if err != nil {
		t.Fatalf("get manually updated waker: %v", err)
	}
	if manuallyUpdatedWaker.Avatar != waker.Avatar {
		t.Fatal("profile changes must preserve the saved appearance")
	}
	if manuallyUpdatedWaker.RoleName != "用户研究员" ||
		manuallyUpdatedWaker.Bio != "更新后的独立简介" ||
		!reflect.DeepEqual(manuallyUpdatedWaker.Expertise, []profile.Expertise{{
			Title:       "用户研究",
			Description: "通过访谈与观察识别真实需求",
		}}) ||
		!reflect.DeepEqual(manuallyUpdatedWaker.WorkStyle, []string{"先访谈再验证"}) ||
		!reflect.DeepEqual(
			manuallyUpdatedWaker.WorkMethods,
			[]profile.WorkMethod{{
				Name:  "研究流程",
				Steps: []string{"准备问题", "整理结论"},
			}},
		) {
		t.Fatalf(
			"unexpected manually updated projection: %+v",
			manuallyUpdatedWaker,
		)
	}
	manuallyUpdatedDocuments, err := global.ProfileDocumentControllerManager.
		ListWakerDocuments(ctx, waker.ID)
	if err != nil {
		t.Fatalf("list manually updated documents: %v", err)
	}
	manuallyUpdatedContents := wakerDocumentContents(manuallyUpdatedDocuments)
	for _, kind := range profile.EditableKinds() {
		if manuallyUpdatedContents[kind] != presetContents[kind] {
			t.Fatalf(
				"manual profile edit must not rewrite %s document: %q",
				kind,
				manuallyUpdatedContents[kind],
			)
		}
	}
	if err := global.ProfileDocumentControllerManager.UpdateRolePresetDocument(
		ctx,
		preset.ID,
		constants.ProfileDocumentKindIdentity,
		"## 擅长领域\n\n### 已修改预设\n\n不应影响伙伴",
		now+1,
	); err != nil {
		t.Fatalf("update source preset document: %v", err)
	}
	if err := global.ProfileDocumentControllerManager.DeleteRolePresetDocuments(
		ctx,
		preset.ID,
	); err != nil {
		t.Fatalf("delete source preset documents: %v", err)
	}
	if err := global.RolePresetControllerManager.Delete(ctx, preset.ID); err != nil {
		t.Fatalf("delete source preset: %v", err)
	}
	independentIdentity, err := global.ProfileDocumentControllerManager.GetWakerDocument(
		ctx,
		waker.ID,
		constants.ProfileDocumentKindIdentity,
	)
	if err != nil {
		t.Fatalf("get independent waker identity: %v", err)
	}
	if independentIdentity.Content !=
		manuallyUpdatedContents[constants.ProfileDocumentKindIdentity] {
		t.Fatalf(
			"waker identity changed with preset: %q",
			independentIdentity.Content,
		)
	}

	updateDocument := UpdateWakerDocument{
		WakerID: waker.ID,
		Kind:    constants.ProfileDocumentKindIdentity,
		Body: UpdateWakerDocumentRequest{
			Content: "## 擅长领域\n\n### 用户研究\n\n识别真实需求",
		},
	}
	if _, err := updateDocument.Output(ctx); err != nil {
		t.Fatalf("update waker document: %v", err)
	}
	editedIdentity, err := global.ProfileDocumentControllerManager.GetWakerDocument(
		ctx,
		waker.ID,
		constants.ProfileDocumentKindIdentity,
	)
	if err != nil {
		t.Fatalf("get edited waker identity: %v", err)
	}
	if editedIdentity.Content != "## 擅长领域\n\n### 用户研究\n\n识别真实需求" {
		t.Fatalf("waker identity document not saved: %q", editedIdentity.Content)
	}
	updatedWaker, err := global.WakerControllerManager.Get(ctx, waker.ID)
	if err != nil {
		t.Fatalf("get updated waker: %v", err)
	}
	if len(updatedWaker.Expertise) != 1 ||
		updatedWaker.Expertise[0].Title != "用户研究" ||
		updatedWaker.Expertise[0].Description != "通过访谈与观察识别真实需求" {
		t.Fatalf(
			"document edit must not recompute projection: %+v",
			updatedWaker.Expertise,
		)
	}

	deleteWaker := DeleteWaker{
		WakerID: waker.ID,
		Body: DeleteWakerRequest{
			ConfirmationName: waker.Name,
		},
	}
	privateSkill := models.SkillPackage{
		ID:               util.New(),
		Name:             "private-review",
		Description:      "Review privately",
		Scope:            constants.SkillScopePrivate,
		Source:           constants.SkillSourceZip,
		OwnerWakerID:     waker.ID,
		CurrentVersionID: util.New(),
	}
	privateVersionPath := filepath.Join("skills", "versions", privateSkill.ID, "1.0.0")
	privateVersion := models.SkillVersion{
		ID:             privateSkill.CurrentVersionID,
		SkillPackageID: privateSkill.ID,
		Version:        "1.0.0",
		Name:           privateSkill.Name,
		Description:    privateSkill.Description,
		ContentPath:    privateVersionPath,
		ContentDigest:  "digest",
	}
	if err := global.SkillControllerManager.CreatePackage(ctx, privateSkill); err != nil {
		t.Fatalf("create private skill: %v", err)
	}
	if err := global.SkillControllerManager.CreateVersion(ctx, privateVersion, true, privateVersion.ID); err != nil {
		t.Fatalf("create private skill version: %v", err)
	}
	if err := global.SkillControllerManager.UpsertWakerSkill(ctx, models.WakerSkill{
		ID: util.New(), WakerID: waker.ID, SkillPackageID: privateSkill.ID, Enabled: true,
	}); err != nil {
		t.Fatalf("enable private skill: %v", err)
	}
	privateVersionDirectory := filepath.Join(global.Config.DataDirectory, privateVersionPath)
	if err := os.MkdirAll(privateVersionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(privateVersionDirectory, "marker"), []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		ID:        util.New(),
		WakerID:   waker.ID,
		Name:      "伙伴专属项目",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := global.ProjectControllerManager.Create(ctx, project); err != nil {
		t.Fatalf("create waker project: %v", err)
	}
	if err := global.ProjectControllerManager.ReplaceDirectories(
		ctx,
		project.ID,
		[]models.ProjectDirectory{{
			Source:           models.ProjectSource{ID: "project-source-1"},
			LoadInstructions: true,
			IsDefault:        true,
		}},
	); err != nil {
		t.Fatalf("create waker project binding: %v", err)
	}
	chatLog, err := eventlog.NewStore(chatLogDirectory)
	if err != nil {
		t.Fatalf("open chat authority: %v", err)
	}
	chatID := "97979797979797979797979797979797"
	authorityExecutionID := "98989898989898989898989898989898"
	layout, err := chatstorage.NewLayout(chatLogDirectory)
	if err != nil {
		t.Fatalf("create chat storage layout: %v", err)
	}
	workspaceDirectory, err := layout.ChatWorkspace(chatID)
	if err != nil {
		t.Fatalf("resolve chat workspace: %v", err)
	}
	if err := workspace.EnsureExecutionWorkspace(workspaceDirectory, chatID, waker.ID, nil); err != nil {
		t.Fatalf("create chat workspace: %v", err)
	}
	if _, err := chatLog.Create(chatID, []eventlog.AppendInput{
		{
			Type: constants.ChatEventTypeCreated,
			Data: mustMarshalWakerAPIEvent(t, eventlog.ChatCreatedData{
				WakerID:            waker.ID,
				WorkspaceDirectory: workspaceDirectory,
				Title:              "进行中的对话",
			}),
		},
		{
			Type:        constants.ChatEventTypeExecutionCreated,
			ExecutionID: authorityExecutionID,
			Data:        mustMarshalWakerAPIEvent(t, eventlog.ExecutionCreatedData{}),
		},
	}); err != nil {
		t.Fatalf("create active chat authority: %v", err)
	}
	if _, err := deleteWaker.Output(ctx); !errors.Is(err, status_error.WakerHasRunningWork) {
		t.Fatalf("delete running waker error = %v, want WakerHasRunningWork", err)
	}
	if _, err := chatLog.Append(chatID, []eventlog.AppendInput{{
		Type:        constants.ChatEventTypeExecutionCompleted,
		ExecutionID: authorityExecutionID,
		Data: mustMarshalWakerAPIEvent(t, eventlog.ExecutionFinishedData{
			Status: constants.ExecutionStatusCompleted,
		}),
	}}); err != nil {
		t.Fatalf("complete authority execution: %v", err)
	}
	if _, err := deleteWaker.Output(ctx); err != nil {
		t.Fatalf("delete waker: %v", err)
	}
	remainingDocuments, err := global.ProfileDocumentControllerManager.
		ListWakerDocuments(ctx, waker.ID)
	if err != nil {
		t.Fatalf("list remaining documents: %v", err)
	}
	if len(remainingDocuments) != 0 {
		t.Fatalf("remaining document count = %d, want 0", len(remainingDocuments))
	}
	if _, err := global.ProjectControllerManager.Get(ctx, project.ID); err == nil {
		t.Fatal("deleted waker project lookup succeeded")
	}
	var remainingProjectBindings int64
	if err := database.DB.Model(&models.ProjectSourceBinding{}).
		Where("project_id = ?", project.ID).
		Count(&remainingProjectBindings).Error; err != nil {
		t.Fatalf("count remaining project bindings: %v", err)
	}
	if remainingProjectBindings != 0 {
		t.Fatalf(
			"remaining project binding count = %d, want 0",
			remainingProjectBindings,
		)
	}
	if _, err := global.SkillControllerManager.GetPackage(ctx, privateSkill.ID); err == nil {
		t.Fatal("deleted waker private skill still exists")
	}
	if _, err := os.Stat(privateVersionDirectory); !os.IsNotExist(err) {
		t.Fatalf("deleted waker skill directory %q still exists: %v", privateVersionDirectory, err)
	}
}

func mustMarshalWakerAPIEvent(t *testing.T, value interface{}) json.RawMessage {
	t.Helper()
	data, err := eventlog.MarshalData(value)
	if err != nil {
		t.Fatalf("marshal chat event: %v", err)
	}
	return data
}
