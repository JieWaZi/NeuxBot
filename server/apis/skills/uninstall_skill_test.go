package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func TestDeleteLibrarySkillDeletesDatabaseFactsAndAuthorityFiles(t *testing.T) {
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
		Source: constants.SkillSourceZip, CurrentVersionID: "version-uninstall-0000000000000000",
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
		ID: "relation-uninstall-000000000000000", WakerID: "waker-1",
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
	for _, request := range []DeleteLibrarySkill{
		{SkillID: skill.ID, ExpectedAffectedCount: 1},
		{SkillID: skill.ID, Confirmed: true, ExpectedAffectedCount: 0},
	} {
		_, err := request.Output(ctx)
		var codedError interface{ Code() int64 }
		if !errors.As(err, &codedError) || codedError.Code() != status_error.PluginConfigurationConflict.Code() {
			t.Fatalf("unconfirmed or stale-count deletion accepted: %v", err)
		}
		if _, err := global.SkillControllerManager.GetPackage(ctx, skill.ID); err != nil {
			t.Fatalf("rejected global deletion changed library: %v", err)
		}
		if _, err := os.Stat(versionDirectory); err != nil {
			t.Fatalf("rejected global deletion changed authority files: %v", err)
		}
	}
	if _, err := (&DeleteLibrarySkill{SkillID: skill.ID, Confirmed: true, ExpectedAffectedCount: 1}).Output(ctx); err != nil {
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
		Source: constants.SkillSourceBuiltin, BuiltIn: true,
	}
	if err := global.SkillControllerManager.CreatePackage(ctx, skill); err != nil {
		t.Fatal(err)
	}
	_, err = (&DeleteLibrarySkill{SkillID: skill.ID}).Output(ctx)
	if !errors.Is(err, status_error.BuiltInSkillCannotUninstall) && err != status_error.BuiltInSkillCannotUninstall {
		t.Fatalf("uninstall built-in error = %v", err)
	}
}

func TestUninstallSkillRejectsMissingWaker(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "uninstall-library-skill.db"), Migrate: true,
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
		ID: "skill-library-uninstall-missing-waker", Name: "library-review",
		Source: constants.SkillSourceZip,
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
		t.Fatalf("library skill was removed through missing waker: %v", err)
	}
}

func TestUninstallSkillPreservesOtherPartnersAndAllVersions(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "remove-library-partner.db"), Migrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	previousSkills, previousWakers, previousTransactions := global.SkillControllerManager, global.WakerControllerManager, global.TransactionControllerManager
	previousDataDirectory := global.Config.DataDirectory
	t.Cleanup(func() {
		global.SkillControllerManager, global.WakerControllerManager, global.TransactionControllerManager = previousSkills, previousWakers, previousTransactions
		global.Config.DataDirectory = previousDataDirectory
	})
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	global.Config.DataDirectory = t.TempDir()
	ctx, _ := gin.CreateTestContext(nil)
	skill := models.SkillPackage{ID: "library-removal", Name: "共享审查", Source: constants.SkillSourceZip, CurrentVersionID: "version-1"}
	if err := global.SkillControllerManager.CreatePackage(ctx, skill); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"version-1", "version-2"} {
		version := models.SkillVersion{ID: id, SkillPackageID: skill.ID, Version: id, Name: skill.Name, ContentPath: filepath.Join("skills", "versions", skill.ID, id), ContentDigest: id}
		if err := global.SkillControllerManager.CreateVersion(ctx, version, false, ""); err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(global.Config.DataDirectory, version.ContentPath)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(id), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for index := 1; index <= 6; index++ {
		id := fmt.Sprintf("partner-%d", index)
		if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
		if err := global.SkillControllerManager.UpsertWakerSkill(ctx, models.WakerSkill{ID: "relation-" + id, WakerID: id, SkillPackageID: skill.ID, Enabled: index != 6}); err != nil {
			t.Fatal(err)
		}
	}
	beforePackage, err := global.SkillControllerManager.GetPackage(ctx, skill.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeVersions, err := global.SkillControllerManager.ListVersions(ctx, skill.ID)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := global.SkillControllerManager.InstalledPartnerPreviews(ctx, []string{skill.ID})
	if err != nil || len(preview[skill.ID]) != 4 {
		t.Fatalf("list preview = %+v, error = %v", preview, err)
	}
	for _, wakerID := range []string{"", "partner-1"} {
		detail, err := getSkill(ctx, &GetWakerSkill{WakerID: wakerID, SkillID: skill.ID})
		if err != nil || len(detail.InstalledPartners) != 6 || detail.AffectedWakerCount != 6 || detail.InstalledPartners[4].ID != "partner-5" {
			t.Fatalf("detail must include fifth partner and disabled installations: %+v, error = %v", detail, err)
		}
		encoded, err := json.Marshal(detail.InstalledPartners)
		if err != nil {
			t.Fatal(err)
		}
		var partners []map[string]any
		if err := json.Unmarshal(encoded, &partners); err != nil {
			t.Fatal(err)
		}
		if partners[4]["enabled"] != true || partners[5]["enabled"] != false {
			t.Fatalf("detail must expose both enabled and disabled relationships: %s", encoded)
		}
	}
	toggle := SetWakerSkillEnabled{WakerID: "partner-6", SkillID: skill.ID, Body: SetSkillEnabledRequest{Enabled: true}}
	if _, err := toggle.Output(ctx); err != nil {
		t.Fatalf("enabling an installed partner failed: %v", err)
	}
	if detail, err := getSkill(ctx, &GetWakerSkill{WakerID: toggle.WakerID, SkillID: skill.ID}); err != nil || !detail.Enabled {
		t.Fatalf("enabled installation did not persist: %+v %v", detail, err)
	}
	toggle.Body.Enabled = false
	if _, err := toggle.Output(ctx); err != nil {
		t.Fatalf("disabling an installed partner failed: %v", err)
	}
	if _, err := (&UninstallSkill{WakerID: "partner-5", SkillID: skill.ID}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	toggle.WakerID = "partner-5"
	toggle.Body.Enabled = true
	_, err = toggle.Output(ctx)
	var codedError interface{ Code() int64 }
	if !errors.As(err, &codedError) || codedError.Code() != status_error.SkillNotFound.Code() {
		t.Fatalf("toggle of removed partner returned %v, want SkillNotFound", err)
	}
	afterPackage, err := global.SkillControllerManager.GetPackage(ctx, skill.ID)
	if err != nil || !reflect.DeepEqual(afterPackage, beforePackage) {
		t.Fatalf("partner removal changed package: %+v %v", afterPackage, err)
	}
	afterVersions, err := global.SkillControllerManager.ListVersions(ctx, skill.ID)
	if err != nil || !reflect.DeepEqual(afterVersions, beforeVersions) {
		t.Fatalf("partner removal changed versions: %+v %v", afterVersions, err)
	}
	for _, version := range afterVersions {
		content, err := os.ReadFile(filepath.Join(global.Config.DataDirectory, version.ContentPath, "SKILL.md"))
		if err != nil || string(content) != version.ID {
			t.Fatalf("partner removal changed authority file: %q %v", content, err)
		}
	}
	for index := 1; index <= 6; index++ {
		id := fmt.Sprintf("partner-%d", index)
		items, err := global.SkillControllerManager.ListInstalled(ctx, id, "")
		if index == 5 {
			if err != nil || len(items) != 0 {
				t.Fatalf("removed partner still installed: %+v %v", items, err)
			}
			continue
		}
		if err != nil || len(items) != 1 || items[0].Enabled != (index != 6) || items[0].CurrentVersion.ID != skill.CurrentVersionID {
			t.Fatalf("other partner installation changed: %+v %v", items, err)
		}
	}
	detail, err := getSkill(ctx, &GetWakerSkill{SkillID: skill.ID})
	if err != nil || len(detail.InstalledPartners) != 5 || detail.AffectedWakerCount != 5 {
		t.Fatalf("detail did not reflect relation removal: %+v %v", detail, err)
	}
}
