package plugins

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

func TestWakerPluginReferencesUseFourNarrowQueriesWithoutManagementState(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "references.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db := database.DB
	previousPlugins, previousWakers, previousTx := global.PluginControllerManager, global.WakerControllerManager, global.TransactionControllerManager
	defer func() {
		global.PluginControllerManager, global.WakerControllerManager, global.TransactionControllerManager = previousPlugins, previousWakers, previousTx
	}()
	global.PluginControllerManager = controllers.NewPluginController(db)
	global.WakerControllerManager = controllers.NewWakerController(db)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db))
	for _, row := range []any{
		&models.Waker{ID: "w1", Name: "伙伴"},
		&models.Plugin{ID: "p1", Name: "插件", Description: "说明", SourceKey: "source"},
		&models.PluginVersion{ID: "v1", PluginID: "p1", Version: "1.0"},
		&models.WakerPlugin{ID: "i1", WakerID: "w1", PluginID: "p1", VersionID: "v1", Enabled: true},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("UPDATE plugin_versions SET package = ?", "invalid-json").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE plugin_configurations").Error; err != nil {
		t.Fatal(err)
	}
	queries := []string{}
	if err := db.Callback().Query().After("gorm:query").Register("test:reference-api-budget", func(query *gorm.DB) {
		if !query.DryRun {
			queries = append(queries, query.Statement.Table+":"+strings.Join(query.Statement.Selects, ","))
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/", nil)
	value, err := (&ListWakerPluginReferences{WakerID: "w1"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	refs := value.([]models.PluginReference)
	if len(refs) != 1 || refs[0].ID != "p1" || refs[0].VersionID != "v1" || len(queries) != 4 {
		t.Fatalf("refs=%+v queries=%v", refs, queries)
	}
	for _, query := range queries {
		if strings.Contains(query, "package") || strings.Contains(query, "plugin_configurations") {
			t.Fatalf("management data read: %s", query)
		}
	}
}
