package skills

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestUninstallSkillDeletesDatabaseFactsAndAuthorityFiles(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "uninstall-skill.db"), Migrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	previousSkills := global.SkillControllerManager
	previousWakers := global.WakerControllerManager
	previousTransactions := global.TransactionControllerManager
	previousDataDirectory := global.Config.DataDirectory
	t.Cleanup(func() {
		global.SkillControllerManager = previousSkills
		global.WakerControllerManager = previousWakers
		global.TransactionControllerManager = previousTransactions
		global.Config.DataDirectory = previousDataDirectory
	})
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	global.Config.DataDirectory = t.TempDir()
	ctx, _ := gin.CreateTestContext(nil)
	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: "waker-1", Name: "卸载测试伙伴"}); err != nil {
		t.Fatal(err)
	}
	skill := models.SkillPackage{
		ID: "skill-uninstall-0000000000000000000", Name: "review",
		Scope: constants.SkillScopePrivate, Source: constants.SkillSourceZip,
		OwnerWakerID: "waker-1", CurrentVersionID: "version-uninstall-0000000000000000",
	}
	version := models.SkillVersion{
		ID: skill.CurrentVersionID, SkillPackageID: skill.ID, Version: "1.0.0",
		Name: skill.Name, ContentPath: filepath.Join("skills", "versions", skill.ID, "1.0.0"),
		ContentDigest: "digest",
	}
	if err := global.SkillControllerManager.CreatePackage(ctx, skill); err != nil {
		t.Fatal(err)
	}
	if err := global.SkillControllerManager.CreateVersion(ctx, version, true, version.ID); err != nil {
		t.Fatal(err)
	}
	if err := global.SkillControllerManager.UpsertWakerSkill(ctx, models.WakerSkill{
		ID: "relation-uninstall-000000000000000", WakerID: skill.OwnerWakerID,
		SkillPackageID: skill.ID, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	versionDirectory := filepath.Join(global.Config.DataDirectory, version.ContentPath)
	if err := os.MkdirAll(versionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDirectory, "SKILL.md"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&UninstallSkill{WakerID: skill.OwnerWakerID, SkillID: skill.ID}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := global.SkillControllerManager.GetPackage(ctx, skill.ID); err == nil {
		t.Fatal("uninstalled skill still exists")
	}
	if _, err := os.Stat(versionDirectory); !os.IsNotExist(err) {
		t.Fatalf("uninstalled version directory still exists: %v", err)
	}
}

func TestUninstallSkillRejectsBuiltInPackage(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "uninstall-builtin.db"), Migrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	previousSkills := global.SkillControllerManager
	previousWakers := global.WakerControllerManager
	t.Cleanup(func() {
		global.SkillControllerManager = previousSkills
		global.WakerControllerManager = previousWakers
	})
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	ctx, _ := gin.CreateTestContext(nil)
	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: "waker-1", Name: "内置测试伙伴"}); err != nil {
		t.Fatal(err)
	}
	skill := models.SkillPackage{
		ID: "skill-builtin-00000000000000000000", Name: "builtin-review",
		Scope: constants.SkillScopePublic, Source: constants.SkillSourceBuiltin, BuiltIn: true,
	}
	if err := global.SkillControllerManager.CreatePackage(ctx, skill); err != nil {
		t.Fatal(err)
	}
	_, err = (&UninstallSkill{WakerID: "waker-1", SkillID: skill.ID}).Output(ctx)
	if !errors.Is(err, status_error.BuiltInSkillCannotUninstall) && err != status_error.BuiltInSkillCannotUninstall {
		t.Fatalf("uninstall built-in error = %v", err)
	}
}

func TestUninstallPublicSkillRejectsMissingWaker(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "uninstall-public-skill.db"), Migrate: true,
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
	previousTransactions := global.TransactionControllerManager
	previousDataDirectory := global.Config.DataDirectory
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.SkillControllerManager = previousSkills
		global.TransactionControllerManager = previousTransactions
		global.Config.DataDirectory = previousDataDirectory
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	global.Config.DataDirectory = t.TempDir()
	ctx, _ := gin.CreateTestContext(nil)
	skill := models.SkillPackage{
		ID: "skill-public-uninstall-missing-waker", Name: "public-review",
		Scope: constants.SkillScopePublic, Source: constants.SkillSourceZip,
	}
	if err := global.SkillControllerManager.CreatePackage(ctx, skill); err != nil {
		t.Fatal(err)
	}

	_, err = (&UninstallSkill{WakerID: "missing-waker", SkillID: skill.ID}).Output(ctx)
	var codedError interface{ Code() int64 }
	if !errors.As(err, &codedError) || codedError.Code() != status_error.WakerNotFound.Code() {
		t.Fatalf("uninstall missing waker error = %v, want WakerNotFound", err)
	}
	if _, err := global.SkillControllerManager.GetPackage(ctx, skill.ID); err != nil {
		t.Fatalf("public skill was removed through missing waker: %v", err)
	}
}
