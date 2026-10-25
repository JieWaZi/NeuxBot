package workflows

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestNodeResultAPIKeepsFrozenAttemptsAndNeverExposesCandidates(t *testing.T) {
	for _, fileDelivery := range []bool{false, true} {
		t.Run(map[bool]string{false: "result page", true: "file viewer"}[fileDelivery], func(t *testing.T) {
			testNodeResultFrozenAttempts(t, fileDelivery)
		})
	}
}

func testNodeResultFrozenAttempts(t *testing.T, fileDelivery bool) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(directory, "results.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := database.DB.DB()
	t.Cleanup(func() { connection.Close() })
	runs, store := controllers.NewWorkflowRunController(database.DB), controllers.NewWorkflowController(database.DB)
	previousRuns, previousStore := global.WorkflowRunControllerManager, global.WorkflowControllerManager
	global.WorkflowRunControllerManager, global.WorkflowControllerManager = runs, store
	t.Cleanup(func() {
		global.WorkflowRunControllerManager, global.WorkflowControllerManager = previousRuns, previousStore
	})
	schema := map[string]any{"type": "object", "properties": map[string]any{"file": map[string]any{"type": "string", "format": "file"}, "ordinary": map[string]any{"type": "string"}}}
	page := workflowdsl.PageDefinition{FormatVersion: 1, CatalogVersion: constants.WorkflowResultCatalogVersion, Title: "冻结页面", Spec: map[string]any{"root": "text", "elements": map[string]any{"text": map[string]any{"type": "Text", "props": map[string]any{"value": map[string]any{"$state": "/result"}}}}}}
	revision := models.WorkflowRevision{ID: "revision", DefinitionID: "workflow", Ordinal: 1, Manifest: workflowdsl.Manifest{Meta: workflowdsl.Meta{NodeResultPages: map[string]workflowdsl.PageDefinition{"deliver": page}}, Nodes: []workflowdsl.Node{{ID: "deliver", Kind: "worker", Options: map[string]any{"schema": schema}}}}}
	if fileDelivery {
		revision.Manifest.Meta.NodeResultPages = nil
		revision.Manifest.Meta.NodeResultFiles = map[string]workflowdsl.FileResultDefinition{
			"deliver": {Title: "冻结文件"},
		}
	}
	run := models.WorkflowRun{ID: "run", DefinitionID: "workflow", RevisionID: revision.ID, Status: constants.WorkflowRunStatusFailed, ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}}
	instance := models.WorkflowNodeInstance{ID: "instance", RunID: run.ID, NodeID: "deliver", EffectID: "new-attempt", AttemptGeneration: 2}
	value := workflowdsl.JSONValue(map[string]any{"file": "report.txt", "ordinary": "undocumented.txt"})
	candidate := workflowdsl.JSONValue("private candidate")
	old := models.WorkflowEffect{ID: "old-attempt", RunID: run.ID, InstanceID: instance.ID, Status: constants.WorkflowStepStatusSucceeded, AttemptGeneration: 1, Result: &value, ChatID: "worker-chat", ExecutionID: "execution", FinalMessageID: "final-message", AuthToken: "private token", Boundary: workflowdsl.EffectBoundary{NodeID: "deliver"}}
	next := models.WorkflowEffect{ID: "new-attempt", RunID: run.ID, InstanceID: instance.ID, Status: constants.WorkflowStepStatusRunning, AttemptGeneration: 2, Candidate: &candidate, Boundary: workflowdsl.EffectBoundary{NodeID: "deliver"}}
	for _, model := range []any{&revision, &run, &instance, &old, &next} {
		if err := database.DB.Create(model).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "report.txt"), []byte("report"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/results", nil)
	request := GetWorkflowNodeResult{ID: run.DefinitionID, RunID: run.ID, InstanceID: instance.ID, AttemptID: old.ID}
	output, err := request.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	response := output.(WorkflowNodeResultResponse)
	if !response.Present || response.AttemptGeneration != 1 || len(response.Files) != 1 || !response.Files[0].Available {
		t.Fatalf("old successful output changed after later failure or retry: %+v", response)
	}
	if fileDelivery {
		if response.Page != nil || response.FileView == nil || response.FileView.Title != "冻结文件" || response.WorkingDirectory != directory {
			t.Fatal("file delivery lost viewer context or invented a page", response)
		}
	} else if response.Page == nil || response.Page.Title != "冻结页面" || response.FileView != nil {
		t.Fatal("page presentation changed", response)
	}
	encoded, _ := json.Marshal(response)
	if strings.Contains(string(encoded), "private candidate") || strings.Contains(string(encoded), "private token") || strings.Contains(string(encoded), "authToken") {
		t.Fatal("private effect fields leaked")
	}
	request.AttemptID = next.ID
	output, err = request.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pending := output.(WorkflowNodeResultResponse)
	if pending.Present || pending.Result != nil || len(pending.Files) != 0 {
		t.Fatal("candidate exposed before successful execution", pending)
	}
	request.AttemptID = old.ID
	request.ID = "different-workflow"
	if _, err := request.Output(ctx); err == nil {
		t.Fatal("cross-workflow attempt accepted")
	}
	refsOutput, err := (&GetWorkflowChatResults{ChatID: old.ChatID}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	refs := refsOutput.([]WorkflowNodeResultReference)
	if len(refs) != 1 || refs[0].MessageID != "final-message" || refs[0].AttemptID != old.ID {
		t.Fatal("lost stable message anchor", refs)
	}

	download := DownloadWorkflowFile{ID: run.DefinitionID, RunID: run.ID, InstanceID: instance.ID, AttemptID: old.ID, FileID: response.Files[0].ID}
	attachment, err := download.Output(ctx)
	if err != nil || string(attachment.(*ginx.Attachment).Bytes()) != "report" {
		t.Fatal("declared file download failed", err)
	}
	download.AttemptID = next.ID
	if _, err := download.Output(ctx); err == nil {
		t.Fatal("unconfirmed attempt download accepted")
	}
	download.AttemptID = old.ID
	if err := os.Remove(filepath.Join(directory, "report.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := download.Output(ctx); err == nil {
		t.Fatal("deleted file download accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "report.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := download.Output(ctx); err == nil {
		t.Fatal("escaping symlink download accepted")
	}
	request.ID = run.DefinitionID
	request.InstanceID = "different-instance"
	if _, err := request.Output(ctx); err == nil {
		t.Fatal("cross-instance attempt accepted")
	}
	request.InstanceID = instance.ID
	// 合法标量不能被真值判断或序列化省略为缺失。
	for _, data := range []any{nil, false, float64(0), ""} {
		result := workflowdsl.JSONValue(data)
		if err := database.DB.Model(&models.WorkflowEffect{}).Where("id = ?", old.ID).Select("Result").Updates(&models.WorkflowEffect{Result: &result}).Error; err != nil {
			t.Fatal(err)
		}
		request.ID = run.DefinitionID
		output, err = request.Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !output.(WorkflowNodeResultResponse).Present {
			t.Fatalf("legal result lost: %#v", data)
		}
	}
}
