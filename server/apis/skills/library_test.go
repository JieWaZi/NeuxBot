package skills

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestLibrarySkillHasNoImplicitPartnerInstallation(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "library.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	defer sqlDB.Close()
	oldSkills, oldWakers, oldTx := global.SkillControllerManager, global.WakerControllerManager, global.TransactionControllerManager
	defer func() {
		global.SkillControllerManager = oldSkills
		global.WakerControllerManager = oldWakers
		global.TransactionControllerManager = oldTx
	}()
	previousCategories := global.ResourceCategoryControllerManager
	t.Cleanup(func() { global.ResourceCategoryControllerManager = previousCategories })
	global.ResourceCategoryControllerManager = controllers.NewResourceCategoryController(db.DB)
	global.SkillControllerManager = controllers.NewSkillController(db.DB)
	global.WakerControllerManager = controllers.NewWakerController(db.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	ctx, _ := gin.CreateTestContext(nil)
	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: "partner", Name: "伙伴"}); err != nil {
		t.Fatal(err)
	}
	if err := global.SkillControllerManager.CreatePackage(ctx, models.SkillPackage{ID: "shared", Name: "共享技能"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&GetLibrarySkill{SkillID: "shared"}).Output(ctx); err != nil {
		t.Fatal("global detail incorrectly requires a partner", err)
	}
	items, err := global.SkillControllerManager.ListInstalled(context.Background(), "partner", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("uninstalled resource visible: %+v %v", items, err)
	}
	if _, err := (&InstallLibrarySkill{SkillID: "shared", Body: InstallLibrarySkillRequest{WakerIDs: []string{"partner"}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	items, err = global.SkillControllerManager.ListInstalled(context.Background(), "partner", "")
	if err != nil || len(items) != 1 {
		t.Fatalf("installation absent: %+v %v", items, err)
	}
}

func TestImportedSkillsCategoryPersistsWithoutChangingVersion(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "categories.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	oldSkills, oldWakers, oldTx := global.SkillControllerManager, global.WakerControllerManager, global.TransactionControllerManager
	t.Cleanup(func() {
		global.SkillControllerManager, global.WakerControllerManager, global.TransactionControllerManager = oldSkills, oldWakers, oldTx
	})
	previousCategories := global.ResourceCategoryControllerManager
	t.Cleanup(func() { global.ResourceCategoryControllerManager = previousCategories })
	global.ResourceCategoryControllerManager = controllers.NewResourceCategoryController(db.DB)
	global.SkillControllerManager = controllers.NewSkillController(db.DB)
	global.WakerControllerManager = controllers.NewWakerController(db.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	ctx, _ := gin.CreateTestContext(nil)
	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: "partner", Name: "分类伙伴"}); err != nil {
		t.Fatal(err)
	}
	for _, skill := range []models.SkillPackage{
		{ID: "git", Name: "Git技能", Source: constants.SkillSourceGit, CurrentVersionID: "version"},
		{ID: "zip", Name: "ZIP技能", Source: constants.SkillSourceZip},
		{ID: "internal", Name: "内部技能", Source: constants.SkillSourceBuiltin, BuiltIn: true},
		{ID: "legacy", Name: "其他技能", Source: constants.SkillSourceUnspecified},
	} {
		if err := global.SkillControllerManager.CreatePackage(ctx, skill); err != nil {
			t.Fatal(err)
		}
		if err := global.SkillControllerManager.EnsureWakerSkill(ctx, models.WakerSkill{ID: "relation-" + skill.ID, WakerID: "partner", SkillPackageID: skill.ID, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	version := models.SkillVersion{ID: "version", SkillPackageID: "git", Version: "1.0.0", Name: "original", ContentPath: "skills/original", ContentDigest: "unchanged", CreatedAt: 123}
	if err := global.SkillControllerManager.CreateVersion(ctx, version, true, version.ID); err != nil {
		t.Fatal(err)
	}
	// 数据库会补齐导入操作等默认值，分类更新前先固定真实持久版本。
	version, err = global.SkillControllerManager.GetVersion(ctx, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	category := []string{"  开发工具  ", "写作", "开发工具"}
	update := &UpdateLibrarySkill{SkillID: "git", Body: UpdateWakerSkillRequest{Categories: &category}}
	if err := update.Validate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := update.Output(ctx); err != nil {
		t.Fatal(err)
	}
	detail, err := getSkill(ctx, &GetWakerSkill{WakerID: "partner", SkillID: "git"})
	if err != nil || !slices.Equal(detail.Skill.Categories, []string{"开发工具", "写作"}) || detail.Skill.Name != "Git技能" || detail.CurrentVersion != version {
		t.Fatalf("category changed unrelated data: %+v %v", detail, err)
	}
	library, err := (&ListLibrarySkills{}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page := library.(ListLibrarySkillsResponse)
	if page.TotalSize != 2 || len(page.Resources) != 2 {
		t.Fatalf("library leaked non-imported skills: %+v", page)
	}
	waker, err := (&ListWakerSkills{WakerID: "partner"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows := waker.([]WakerSkillResponse)
	if len(rows) != 2 {
		t.Fatalf("partner list leaked non-imported skills: %+v", rows)
	}
	for _, rows := range [][]WakerSkillResponse{page.Resources, rows} {
		for _, item := range rows {
			if item.Skill.ID == "git" && !slices.Equal(item.Skill.Categories, []string{"开发工具", "写作"}) {
				t.Fatalf("list lost category: %+v", item)
			}
		}
	}
	// 内部技能仍保留执行关系，产品列表过滤不改变运行时资源。
	enabled, err := global.SkillControllerManager.ListEnabled(ctx, "partner")
	if err != nil || len(enabled) != 4 {
		t.Fatalf("product filtering changed runtime resources: %+v %v", enabled, err)
	}
	clear := []string{}
	partnerUpdate := &UpdateWakerSkill{WakerID: "partner", SkillID: "git", Body: UpdateWakerSkillRequest{Categories: &clear}}
	if err := partnerUpdate.Validate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := partnerUpdate.Output(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := global.SkillControllerManager.GetPackage(ctx, "git")
	if err != nil || len(stored.Categories) != 0 || stored.CurrentVersionID != version.ID || stored.Name != "Git技能" {
		t.Fatalf("clear category failed: %+v %v", stored, err)
	}
	versions, err := global.SkillControllerManager.ListVersions(ctx, "git")
	if err != nil || len(versions) != 1 || versions[0] != version {
		t.Fatalf("category created or rewrote version: %+v %v", versions, err)
	}
}
