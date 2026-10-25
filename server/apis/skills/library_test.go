package skills

import (
	"context"
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
	global.SkillControllerManager = controllers.NewSkillController(db.DB)
	global.WakerControllerManager = controllers.NewWakerController(db.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	ctx, _ := gin.CreateTestContext(nil)
	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: "partner", Name: "伙伴"}); err != nil {
		t.Fatal(err)
	}
	if err := global.SkillControllerManager.CreatePackage(ctx, models.SkillPackage{ID: "shared", Name: "共享技能", Scope: constants.SkillScopePublic}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&GetLibrarySkill{SkillID: "shared"}).Output(ctx); err != nil {
		t.Fatal("global detail incorrectly requires a partner", err)
	}
	items, err := global.SkillControllerManager.ListVisible(context.Background(), "partner", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("uninstalled resource visible: %+v %v", items, err)
	}
	if _, err := (&InstallLibrarySkill{SkillID: "shared", Body: InstallLibrarySkillRequest{WakerIDs: []string{"partner"}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	items, err = global.SkillControllerManager.ListVisible(context.Background(), "partner", "")
	if err != nil || len(items) != 1 {
		t.Fatalf("installation absent: %+v %v", items, err)
	}
}
