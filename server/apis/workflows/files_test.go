package workflows

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestCoreFileUploadBindingAndBinaryDownload(t *testing.T) {
	directory := t.TempDir()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(directory, "files.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := database.DB.DB()
	t.Cleanup(func() { connection.Close() })
	store := controllers.NewWorkflowController(database.DB)
	runs := controllers.NewWorkflowRunController(database.DB)
	transactions := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	state := workflowstate.New(store, runs, transactions)
	s := workflow.New(workflow.Config{Store: store, Runs: runs, Transactions: transactions, State: state, DataDirectory: directory})
	api := &workflowDependencies{Store: store, Runs: runs, Transactions: transactions, State: state}
	previousService, previousState, previousRuns := global.WorkflowService, global.WorkflowState, global.WorkflowRunControllerManager
	global.WorkflowService, global.WorkflowState, global.WorkflowRunControllerManager = s, state, runs
	t.Cleanup(func() {
		global.WorkflowService, global.WorkflowState, global.WorkflowRunControllerManager = previousService, previousState, previousRuns
	})
	ctx := context.Background()
	definition, err := createDefinition(ctx, api, CreateDefinitionCommand{RequestID: "create", Title: "文件", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	source := `export const meta={sdk:'ally.workflow/1',title:'文件',description:'交付文件',inputSchema:{type:'string',format:'file'},outputSchema:{type:'string',format:'file'},phases:[{id:'deliver',title:'交付',detail:'交付文件',canExit:true}]};phase('deliver');return args;`
	if _, diagnostics, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save", Source: source, SDKVersion: constants.WorkflowSDKVersion}); err != nil {
		t.Fatalf("%v: %+v", err, diagnostics)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "report.docx")
	content := []byte{0, 0xff, 0x50, 0x4b}
	part.Write(content)
	writer.Close()
	apiContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiContext.Params = gin.Params{{Key: "id", Value: definition.ID}}
	apiContext.Request = httptest.NewRequest("POST", "/files", &body)
	apiContext.Request.Header.Set("Content-Type", writer.FormDataContentType())
	operator := &UploadWorkflowFile{}
	if err := ginx.ParameterBinding(apiContext, operator, ginx.GetOperatorTypeInfo(reflect.TypeOf(operator))); err != nil {
		t.Fatal(err)
	}
	result, err := operator.Output(apiContext)
	if err != nil {
		t.Fatal(err)
	}
	upload := result.(workflowfiles.Upload)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "run", Args: upload.Reference})
	if err != nil {
		t.Fatal(err)
	}
	runnerContext, cancelRunner := context.WithCancel(ctx)
	defer cancelRunner()
	if err := s.Start(runnerContext); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(25 * time.Millisecond)
	defer poll.Stop()
	for {
		run, err = runs.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if workflowstate.TerminalRun(run.Status) {
			break
		}
		select {
		case <-deadline.C:
			t.Fatalf("workflow did not finish: status=%s", run.Status)
		case <-poll.C:
		}
	}
	files, err := s.OutputFiles(ctx, run)
	if err != nil || len(files) != 1 || !files[0].Available {
		t.Fatalf("declared root file: %+v %v", files, err)
	}
	downloadContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	downloadContext.Request = httptest.NewRequest("GET", "/file", nil)
	attachment, err := (&DownloadWorkflowFile{ID: definition.ID, RunID: run.ID, FileID: files[0].ID}).Output(downloadContext)
	if err != nil {
		t.Fatal(err)
	}
	buffer, ok := attachment.(interface{ Bytes() []byte })
	if !ok || !bytes.Equal(buffer.Bytes(), content) {
		t.Fatal("download altered binary content")
	}
	if _, err := (&DownloadWorkflowFile{ID: definition.ID, RunID: run.ID, Candidate: "../private.txt"}).Output(downloadContext); err == nil {
		t.Fatal("download escaped scope")
	}
}
