package workflows

import (
	"context"
	"net/http/httptest"
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

func TestUpdateWorkflowCategoriesSharesVocabularyWithoutCreatingRevision(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "categories.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := database.DB.DB()
	t.Cleanup(func() { _ = connection.Close() })
	previousStore, previousCategories, previousTransactions := global.WorkflowControllerManager, global.ResourceCategoryControllerManager, global.TransactionControllerManager
	t.Cleanup(func() {
		global.WorkflowControllerManager, global.ResourceCategoryControllerManager, global.TransactionControllerManager = previousStore, previousCategories, previousTransactions
	})
	global.WorkflowControllerManager = controllers.NewWorkflowController(database.DB)
	global.ResourceCategoryControllerManager = controllers.NewResourceCategoryController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	if err := global.WorkflowControllerManager.CreateDefinition(ctx, models.WorkflowDefinition{ID: "flow", Title: "流程", CurrentRevisionID: "revision", Ordinal: 2}); err != nil {
		t.Fatal(err)
	}
	update := &UpdateWorkflowCategories{ID: "flow", Body: UpdateWorkflowCategoriesRequest{Categories: []string{" 开发 ", "研究", "开发"}}}
	if _, err := update.Output(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := global.WorkflowControllerManager.GetDefinition(ctx, "flow")
	if err != nil || !slices.Equal(stored.Categories, []string{"开发", "研究"}) || stored.CurrentRevisionID != "revision" || stored.Ordinal != 2 {
		t.Fatalf("unexpected stored metadata: %+v %v", stored, err)
	}
	options, err := global.ResourceCategoryControllerManager.List(context.Background(), constants.ResourceCategoryTypeWorkflow)
	if err != nil || len(options) != 2 {
		t.Fatalf("shared vocabulary missing: %+v %v", options, err)
	}
	events, err := global.WorkflowControllerManager.ListEvents(ctx, "flow", 0, 10)
	if err != nil || len(events) != 1 || events[0].Type != constants.WorkflowDefinitionUpdatedEvent {
		t.Fatalf("metadata event missing: %+v %v", events, err)
	}
	update.Body.Categories = []string{}
	if _, err := update.Output(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err = global.WorkflowControllerManager.GetDefinition(ctx, "flow")
	if err != nil || len(stored.Categories) != 0 {
		t.Fatalf("categories were not cleared: %+v %v", stored, err)
	}
	update.ID, update.Body.Categories = "missing", []string{"不能留下的分类"}
	if _, err := update.Output(ctx); err == nil {
		t.Fatal("missing workflow accepted")
	}
	options, err = global.ResourceCategoryControllerManager.List(ctx, constants.ResourceCategoryTypeWorkflow)
	if err != nil || len(options) != 2 {
		t.Fatalf("failed update changed vocabulary: %+v %v", options, err)
	}
	if err := database.DB.Exec("CREATE TRIGGER reject_category_update BEFORE UPDATE OF categories ON workflow_definitions BEGIN SELECT RAISE(ABORT, 'test category failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	update.ID, update.Body.Categories = "flow", []string{"必须回滚的分类"}
	if _, err := update.Output(ctx); err == nil {
		t.Fatal("database failure was reported as success")
	}
	options, err = global.ResourceCategoryControllerManager.List(ctx, constants.ResourceCategoryTypeWorkflow)
	if err != nil || len(options) != 2 {
		t.Fatalf("failed transaction left a vocabulary entry: %+v %v", options, err)
	}
	events, err = global.WorkflowControllerManager.ListEvents(ctx, "flow", 0, 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("failed transaction published a metadata event: %+v %v", events, err)
	}
}
