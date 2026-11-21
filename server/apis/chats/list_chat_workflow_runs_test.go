package chats

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestListChatWorkflowRunsReturnsOriginalMessageAndExactRun(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "workflow-chat.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	previousChats, previousRuns, previousTx := global.ChatControllerManager, global.WorkflowRunControllerManager, global.TransactionControllerManager
	t.Cleanup(func() {
		global.ChatControllerManager, global.WorkflowRunControllerManager, global.TransactionControllerManager = previousChats, previousRuns, previousTx
	})
	global.ChatControllerManager = controllers.NewChatController(db.DB)
	global.WorkflowRunControllerManager = controllers.NewWorkflowRunController(db.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	if err := db.DB.Create(&models.Chat{ID: "member"}).Error; err != nil {
		t.Fatal(err)
	}
	old := models.WorkflowRun{ID: "tenth", DefinitionID: "flow", RevisionID: "v1", SourceChatID: "member", SourceName: "评审", SourceExecutionID: "turn10", SourceMessageID: "message10", Status: constants.WorkflowRunStatusFailed, Context: map[string]any{}, WorkerConfigurations: map[string]models.WorkflowExecutionDefaults{}}
	newRun := old
	newRun.ID, newRun.SourceExecutionID, newRun.SourceMessageID, newRun.SourceRunID, newRun.Status = "eleventh", "turn11", "message11", "tenth", constants.WorkflowRunStatusSucceeded
	newRun.OutputFiles = map[string]models.WorkflowOutputFile{
		"/partial": {Partial: true, Error: status_error.WorkflowFileNotFound.Key()},
		"/final":   {Error: status_error.WorkflowFileInvalid.Key()},
	}
	newRun.ChatDeliveryError = status_error.MessageAttachmentStorageError.Key()
	foreign := old
	foreign.ID, foreign.SourceChatID, foreign.SourceExecutionID = "foreign", "leader", "leader-turn"
	if err := db.DB.Create(&[]models.WorkflowRun{old, newRun, foreign}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	lookup := func(ids string) []ChatWorkflowRun {
		t.Helper()
		cmd := ListChatWorkflowRuns{ChatID: "member", ExecutionIDs: ids}
		if err := cmd.Validate(ctx); err != nil {
			t.Fatal(err)
		}
		result, err := cmd.Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return result.(ChatWorkflowRunsResponse).Resources
	}
	before, after := lookup("turn10,leader-turn"), lookup("turn11")
	if len(before) != 1 || before[0].ID != "tenth" || before[0].InputMessageID != "message10" || before[0].Status != constants.WorkflowRunStatusFailed || len(after) != 1 || after[0].ID != "eleventh" || after[0].SourceRunID != "tenth" || after[0].InputMessageID != "message11" || after[0].RevisionID != "v1" || after[0].Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	if after[0].SnapshotError != status_error.WorkflowFileInvalid.Key() || after[0].DeliveryError != status_error.MessageAttachmentStorageError.Key() {
		t.Fatalf("timeline lost either source or target error: %+v", after[0])
	}
	if err := global.WorkflowRunControllerManager.DeleteRunHistory(ctx, "flow", "tenth", 1); err != nil {
		t.Fatal(err)
	}
	deleted := lookup("turn10")
	if len(deleted) != 1 || !deleted[0].Removed || deleted[0].ID != "tenth" || len(deleted[0].ActiveRequests) != 0 {
		t.Fatal(deleted)
	}
	for _, invalid := range []string{"", "turn10,", strings.Repeat("a,", 100) + "b", strings.Repeat("a", 33)} {
		if (&ListChatWorkflowRuns{ExecutionIDs: invalid}).Validate(ctx) == nil {
			t.Fatalf("accepted invalid executionIds: %q", invalid)
		}
	}
}

func TestListChatWorkflowRunsReadsAuthoritativeSourcesAfterFailure(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "ordinary-chat.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	previousChats, previousRuns, previousTx := global.ChatControllerManager, global.WorkflowRunControllerManager, global.TransactionControllerManager
	t.Cleanup(func() {
		global.ChatControllerManager, global.WorkflowRunControllerManager, global.TransactionControllerManager = previousChats, previousRuns, previousTx
	})
	global.ChatControllerManager = controllers.NewChatController(database.DB)
	global.WorkflowRunControllerManager = unavailableChatWorkflowSources{WorkflowRunController: controllers.NewWorkflowRunController(database.DB)}
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	if err := database.DB.Create(&models.Chat{ID: "ordinary-chat"}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	query := ListChatWorkflowRuns{ChatID: "ordinary-chat", ExecutionIDs: "turn"}
	if _, err := query.Output(ctx); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("query did not consult its source while its store is unavailable: %v", err)
	}
	global.WorkflowRunControllerManager = controllers.NewWorkflowRunController(database.DB)
	result, err := query.Output(ctx)
	response, ok := result.(ChatWorkflowRunsResponse)
	if err != nil || !ok || response.Resources == nil || len(response.Resources) != 0 {
		t.Fatalf("recovered source did not return the empty chat: %+v, %v", result, err)
	}
	query.ChatID = "missing-chat"
	_, err = query.Output(ctx)
	var missing interface{ Key() string }
	if !errors.As(err, &missing) || missing.Key() != status_error.ChatNotFound.Key() {
		t.Fatalf("missing chat did not return ChatNotFound: %v", err)
	}
}

type unavailableChatWorkflowSources struct {
	// WorkflowRunController 保留来源查询以外的真实仓储行为。
	controllers.WorkflowRunController
}

func (unavailableChatWorkflowSources) ListChatRunSources(context.Context, string, []string) ([]models.WorkflowRun, error) {
	return nil, status_error.DatabaseError
}
