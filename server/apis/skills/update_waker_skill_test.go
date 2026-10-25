package skills

import (
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestUpdateWakerSkillChangesDisplayNameWithoutVersion(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "update-skill.db"), Migrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	previousWakers := global.WakerControllerManager
	previousSkills := global.SkillControllerManager
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.SkillControllerManager = previousSkills
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	ctx, _ := gin.CreateTestContext(nil)

	waker := models.Waker{ID: "waker-update-skill", Name: "展示名测试伙伴"}
	if err := global.WakerControllerManager.Create(ctx, waker); err != nil {
		t.Fatal(err)
	}
	skill := models.SkillPackage{
		ID: "skill-update-display", Name: "review", Description: "Review changes",
		Scope: constants.SkillScopePrivate, Source: constants.SkillSourceZip,
		OwnerWakerID: waker.ID, CurrentVersionID: "version-update-display",
	}
	if err := global.SkillControllerManager.CreatePackage(ctx, skill); err != nil {
		t.Fatal(err)
	}
	version := models.SkillVersion{
		ID: skill.CurrentVersionID, SkillPackageID: skill.ID, Version: "1.0.0",
		Name: "review-metadata", Description: "Review changes",
		ContentPath: "skills/versions/update/1", ContentDigest: "digest",
	}
	if err := global.SkillControllerManager.CreateVersion(ctx, version, true, version.ID); err != nil {
		t.Fatal(err)
	}

	_, err = (&UpdateWakerSkill{
		WakerID: waker.ID,
		SkillID: skill.ID,
		Body:    UpdateWakerSkillRequest{Name: "代码审查"},
	}).Output(ctx)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	updated, err := global.SkillControllerManager.GetPackage(ctx, skill.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "代码审查" {
		t.Fatalf("display name = %q, want 代码审查", updated.Name)
	}
	current, err := global.SkillControllerManager.GetVersion(ctx, skill.CurrentVersionID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Name != "review-metadata" {
		t.Fatalf("metadata name = %q, want review-metadata", current.Name)
	}
}
