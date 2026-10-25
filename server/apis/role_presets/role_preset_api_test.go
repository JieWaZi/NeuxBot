package role_presets

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestRolePresetAPICreatesUpdatesAndDeletesUserPreset(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "role-preset-api.db"),
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

	previousPresets := global.RolePresetControllerManager
	previousDocuments := global.ProfileDocumentControllerManager
	previousTransactions := global.TransactionControllerManager
	t.Cleanup(func() {
		global.RolePresetControllerManager = previousPresets
		global.ProfileDocumentControllerManager = previousDocuments
		global.TransactionControllerManager = previousTransactions
	})
	global.RolePresetControllerManager = controllers.NewRolePresetController(database.DB)
	global.ProfileDocumentControllerManager = controllers.NewProfileDocumentController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)

	ctx, _ := gin.CreateTestContext(nil)
	create := CreateRolePreset{
		Body: RolePresetWriteRequest{
			RoleName:      " Designer ",
			DefaultAvatar: "preset:designer",
			Description:   "设计角色",
			InputMode:     constants.RolePresetInputModeManual,
			Manual: profile.ManualInput{
				Expertise: []profile.Expertise{{
					Title:       "界面设计",
					Description: "设计清晰一致的产品界面",
				}},
				WorkStyle: []string{"先验证"},
				WorkMethods: []profile.WorkMethod{{
					Name:  "设计",
					Steps: []string{"分析", "交付"},
				}},
			},
		},
	}
	output, err := create.Output(ctx)
	if err != nil {
		t.Fatalf("create role preset: %v", err)
	}
	preset, ok := output.(models.RolePreset)
	if !ok {
		t.Fatalf("create output type = %T, want models.RolePreset", output)
	}
	if preset.RoleName != "Designer" || len(preset.Expertise) != 1 {
		t.Fatalf("unexpected created preset: %+v", preset)
	}
	documents, err := global.ProfileDocumentControllerManager.
		ListRolePresetDocuments(ctx, preset.ID)
	if err != nil {
		t.Fatalf("list created documents: %v", err)
	}
	if len(documents) != 3 {
		t.Fatalf("document count = %d, want 3", len(documents))
	}
	duplicate := create
	duplicate.Body.RoleName = " Designer "
	duplicate.Body.Description = "重复名称"
	if _, err := duplicate.Output(ctx); !errors.Is(err, status_error.RolePresetNameConflict) {
		t.Fatalf("duplicate role preset error = %v, want RolePresetNameConflict", err)
	}

	identity := "前言\n\n## 擅长领域\n\n### 用户研究\n\n通过访谈与观察识别真实需求。\n\n## 保留章节\n\n不能删除"
	update := UpdateRolePreset{
		RolePresetID: preset.ID,
		Body: RolePresetWriteRequest{
			RoleName:      "Designer",
			DefaultAvatar: "preset:designer",
			Description:   "更新后的角色描述",
			InputMode:     constants.RolePresetInputModeMarkdown,
			Documents: profile.DocumentsInput{
				Identity: &identity,
			},
		},
	}
	if _, err := update.Output(ctx); err != nil {
		t.Fatalf("update role preset: %v", err)
	}
	updated, err := global.RolePresetControllerManager.Get(ctx, preset.ID)
	if err != nil {
		t.Fatalf("get updated preset: %v", err)
	}
	if updated.Description != "更新后的角色描述" ||
		len(updated.Expertise) != 1 ||
		updated.Expertise[0].Title != "用户研究" ||
		updated.Expertise[0].Description != "通过访谈与观察识别真实需求。" {
		t.Fatalf("unexpected updated preset: %+v", updated)
	}

	remove := DeleteRolePreset{RolePresetID: preset.ID}
	if _, err := remove.Output(ctx); err != nil {
		t.Fatalf("delete role preset: %v", err)
	}
	remaining, err := global.ProfileDocumentControllerManager.
		ListRolePresetDocuments(ctx, preset.ID)
	if err != nil {
		t.Fatalf("list remaining documents: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining document count = %d, want 0", len(remaining))
	}
}

func TestRolePresetAPIRejectsSystemPresetWrites(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "system-role-preset-api.db"),
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

	previousPresets := global.RolePresetControllerManager
	previousDocuments := global.ProfileDocumentControllerManager
	previousTransactions := global.TransactionControllerManager
	t.Cleanup(func() {
		global.RolePresetControllerManager = previousPresets
		global.ProfileDocumentControllerManager = previousDocuments
		global.TransactionControllerManager = previousTransactions
	})
	global.RolePresetControllerManager = controllers.NewRolePresetController(database.DB)
	global.ProfileDocumentControllerManager = controllers.NewProfileDocumentController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)

	ctx, _ := gin.CreateTestContext(nil)
	now := time.Now().UnixMilli()
	systemPreset := models.RolePreset{
		ID:            util.New(),
		RoleName:      "系统角色",
		DefaultAvatar: constants.DefaultAvatar,
		Description:   "只读",
		Source:        constants.RolePresetSourceSystem,
		Expertise:     []profile.Expertise{},
		WorkStyle:     []string{},
		WorkMethods:   []profile.WorkMethod{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := global.RolePresetControllerManager.Create(ctx, systemPreset); err != nil {
		t.Fatalf("create system preset: %v", err)
	}

	remove := DeleteRolePreset{RolePresetID: systemPreset.ID}
	if _, err := remove.Output(ctx); !errors.Is(err, status_error.SystemRolePresetReadOnly) {
		t.Fatalf("delete system preset error = %v, want SystemRolePresetReadOnly", err)
	}
}
