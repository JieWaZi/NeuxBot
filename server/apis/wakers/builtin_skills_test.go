package wakers

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestCreatePersonalAssistantUsesApplicationOwnedSystemSkill(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "wakers.db"), Migrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	previousWakers, previousPresets := global.WakerControllerManager, global.RolePresetControllerManager
	previousDocuments, previousSkills := global.ProfileDocumentControllerManager, global.SkillControllerManager
	previousTransactions := global.TransactionControllerManager
	t.Cleanup(func() {
		global.WakerControllerManager, global.RolePresetControllerManager = previousWakers, previousPresets
		global.ProfileDocumentControllerManager, global.SkillControllerManager = previousDocuments, previousSkills
		global.TransactionControllerManager = previousTransactions
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.RolePresetControllerManager = controllers.NewRolePresetController(database.DB)
	global.ProfileDocumentControllerManager = controllers.NewProfileDocumentController(database.DB)
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	ctx := context.Background()
	if err := builtinrolepreset.EnsureCatalog(ctx, builtinrolepreset.ControllerCatalog{
		Presets: global.RolePresetControllerManager, Documents: global.ProfileDocumentControllerManager,
	}); err != nil {
		t.Fatal(err)
	}
	requestContext, _ := gin.CreateTestContext(nil)
	create := &CreateWaker{Body: CreateWakerRequest{
		RolePresetID: builtinrolepreset.PersonalAssistantRolePresetID, Name: "My assistant",
	}}
	installer := builtinskills.Installer{
		DataDirectory: t.TempDir(), Skills: global.SkillControllerManager, Transactions: global.TransactionControllerManager,
	}
	if err := installer.EnsureCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := create.Output(requestContext); err != nil {
		t.Fatal(err)
	}
	wakers, err := global.WakerControllerManager.List(ctx, "", 10, 0)
	if err != nil || len(wakers) != 1 {
		t.Fatalf("created partner: %+v %v", wakers, err)
	}
	enabled, err := global.SkillControllerManager.ListEnabled(ctx, wakers[0].ID)
	if err != nil || len(enabled) != 0 {
		t.Fatalf("system skill must not be stored in editable partner skill state: %+v %v", enabled, err)
	}
}
