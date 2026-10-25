package workflows

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestArtifactAPIValidatesOwnerAndBuildsTextAndImagePreviews(t *testing.T) {
	directory := t.TempDir()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(directory, "preview.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := database.DB.DB()
	t.Cleanup(func() { connection.Close() })
	runs := controllers.NewWorkflowRunController(database.DB)
	store := controllers.NewWorkflowController(database.DB)
	transactions := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	state := workflowstate.New(store, runs, transactions)
	s := workflow.New(workflow.Config{Store: store, Runs: runs, Transactions: transactions, State: state})
	api := &workflowDependencies{Store: store, Runs: runs, Transactions: transactions, State: state}
	previous := global.WorkflowRunControllerManager
	global.WorkflowRunControllerManager = runs
	t.Cleanup(func() { global.WorkflowRunControllerManager = previous })
	definition, err := createDefinition(context.Background(), api, CreateDefinitionCommand{RequestID: "create-preview", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	source := `export const meta={sdk:'ally.workflow/1',title:'文件预览',description:'读取产物',phases:[{id:'deliver',title:'交付',detail:'交付文件',canExit:true}]};return true;`
	if _, _, err := s.SaveSource(context.Background(), workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save-preview", Source: source, SDKVersion: constants.WorkflowSDKVersion}); err != nil {
		t.Fatal(err)
	}
	run, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "run-preview"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/artifact", nil)
	text := "<script>window.bad=true</script><h1>真实报告</h1>"
	if err := os.WriteFile(filepath.Join(directory, "report.html"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	operator := &GetWorkflowArtifact{ID: definition.ID, RunID: run.ID, Candidate: "report.html"}
	value, err := operator.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := value.(WorkflowArtifactResponse)
	if !first.Verified || first.Text == nil || *first.Text != text || first.DataURL != "" || first.MediaType != "text/plain; charset=utf-8" || first.Digest == "" {
		t.Fatalf("HTML must remain text: %+v", first)
	}
	operator.ExpectedDigest = first.Digest
	if err := os.WriteFile(filepath.Join(directory, "report.html"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	value, err = operator.Output(ctx)
	if err != nil || !value.(WorkflowArtifactResponse).Changed {
		t.Fatalf("content change not exposed: %+v %v", value, err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "image.png"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	operator.Candidate, operator.ExpectedDigest = "image.png", ""
	value, err = operator.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	preview := value.(WorkflowArtifactResponse)
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(preview.DataURL, "data:image/png;base64,"))
	if err != nil || !preview.Verified || preview.Text != nil || !bytes.Equal(payload, encoded.Bytes()) {
		t.Fatalf("verified image encoding: %+v %v", preview, err)
	}
	operator.ID = "another-definition"
	if _, err := operator.Output(ctx); err == nil {
		t.Fatal("another workflow inspected the run's artifact")
	}
}
