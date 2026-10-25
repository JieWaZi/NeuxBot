package skills

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestWakerSkillResponsesExposeSharedChangeImpact(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "skill-impact.db"),
		Migrate: true,
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
	t.Cleanup(func() { global.SkillControllerManager = previousSkills })
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	previousTransactions := global.TransactionControllerManager
	t.Cleanup(func() { global.TransactionControllerManager = previousTransactions })
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	previousWakers := global.WakerControllerManager
	t.Cleanup(func() { global.WakerControllerManager = previousWakers })
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	ctx := context.Background()
	for _, waker := range []models.Waker{
		{ID: "waker-1", Name: "伙伴一", Avatar: "companion:v2:0123456789abcdef0123456789abcdef"},
		{ID: "waker-2", Name: "伙伴二"},
		{ID: "waker-3", Name: "未安装伙伴"},
	} {
		if err := global.WakerControllerManager.Create(ctx, waker); err != nil {
			t.Fatal(err)
		}
	}
	for _, relation := range []models.WakerSkill{{ID: "p1", WakerID: "waker-1", SkillPackageID: "public"}, {ID: "p2", WakerID: "waker-2", SkillPackageID: "public"}, {ID: "s1", WakerID: "waker-1", SkillPackageID: "private"}} {
		if err := global.SkillControllerManager.UpsertWakerSkill(ctx, relation); err != nil {
			t.Fatal(err)
		}
	}
	responses, err := wakerSkillResponses(ctx, []models.SkillWithState{
		{SkillPackage: models.SkillPackage{ID: "public", Scope: constants.SkillScopePublic}},
		{SkillPackage: models.SkillPackage{ID: "private", Scope: constants.SkillScopePrivate}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if responses[0].AffectedWakerCount != 2 {
		t.Fatalf("public affected wakers = %d, want 2", responses[0].AffectedWakerCount)
	}
	if responses[1].AffectedWakerCount != 1 {
		t.Fatalf("private affected wakers = %d, want 1", responses[1].AffectedWakerCount)
	}
	if len(responses[0].InstalledPartners) != 2 || responses[0].InstalledPartners[0].ID != "waker-1" || responses[0].InstalledPartners[0].Avatar != "companion:v2:0123456789abcdef0123456789abcdef" {
		t.Fatalf("installed partner preview = %+v", responses[0].InstalledPartners)
	}
	if len(responses[1].InstalledPartners) != 1 || responses[1].InstalledPartners[0].Name != "伙伴一" {
		t.Fatalf("private partner preview = %+v", responses[1].InstalledPartners)
	}
}
