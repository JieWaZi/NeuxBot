package automatic_tasks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowapi "github.com/JieWaZi/NeuxBot/server/apis/workflows"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowinput "github.com/JieWaZi/NeuxBot/server/internal/workflow/input"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
)

func workflowTask(t *testing.T, h *testHarness, source string) (models.WorkflowDefinition, models.AutomaticTaskConfiguration) {
	t.Helper()
	ctx := context.Background()
	definition := models.WorkflowDefinition{ID: util.New(), Title: "自动流程", CreationWakerID: h.wakerID, ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: h.directory}}
	err := h.workflowStore.CreateDefinition(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	first, diagnostics, err := h.workflows.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save", Source: source, SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatalf("%v: %+v", err, diagnostics)
	}
	definition.CurrentRevisionID = first.RevisionID
	config := models.AutomaticTaskConfiguration{Name: "自动流程", TargetKind: constants.AutomaticTaskTargetKindWorkflow, WorkflowID: definition.ID, WorkflowRevisionPolicy: constants.WorkflowRevisionPolicyCurrent, WorkflowInputBindings: map[string]workflowinput.Binding{"text": {Source: constants.WorkflowInputSourceAPI}}, Trigger: constants.AutomaticTaskTriggerAPI, Enabled: true, PermissionLevel: constants.PermissionLevelDefault}
	return definition, config
}

func TestWorkflowAPIUploadsUseManagedRootSeparateFromProgramData(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source := `export const meta = {sdk:'ally.workflow/1',title:'输入文件',inputSchema:{type:'object',properties:{document:{type:'string',format:'file'}},required:['document']},phases:[{id:'read',title:'读取文件',detail:'接纳文件输入',canExit:true}]};phase('read');return args.document;`
	definition, config := workflowTask(t, h, source)
	config.WorkflowInputBindings = map[string]workflowinput.Binding{"document": {Source: constants.WorkflowInputSourceAPI}}
	saved, err := h.service.SaveConfiguration(ctx, h.wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	upload, err := h.workflows.UploadFile(ctx, definition.ID, saved.TaskID, "input.txt", strings.NewReader("input content"))
	if err != nil {
		t.Fatal(err)
	}
	view, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "file-input", "", map[string]any{"document": upload.Reference})
	if err != nil {
		t.Fatal(err)
	}
	run, err := h.api.config.WorkflowRuns.GetRun(ctx, view.WorkflowRunID)
	if err != nil {
		t.Fatal(err)
	}
	input := run.Args.(map[string]any)["document"].(string)
	if !strings.Contains(filepath.ToSlash(input), "/workflow-inputs/") {
		t.Fatalf("unexpected frozen input path: %s", input)
	}
	content, err := os.ReadFile(input)
	if err != nil || string(content) != "input content" {
		t.Fatalf("frozen input is unavailable: %q %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(h.api.config.ManagedFilesDirectory, "workflow-files", definition.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.api.config.DataDirectory, "workflow-files")); !os.IsNotExist(err) {
		t.Fatalf("workflow uploads leaked into program data root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.api.config.DataDirectory, "integration-api.json")); err != nil {
		t.Fatalf("API preferences moved out of program data: %v", err)
	}
}

func TestWorkflowOccurrenceFreezesVersionAndReconcilesAcceptedReceipt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	data, err := os.ReadFile("../../internal/workflow/dsl/testdata/f0.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	definition, config := workflowTask(t, h, string(data))
	saved, err := h.service.SaveConfiguration(ctx, h.wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	first, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "same", "", map[string]any{"text": "Test"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ChatID != "" || first.ExecutionID != "" || first.WorkflowRunID == "" || len(h.boundary.intents) != 0 {
		t.Fatalf("workflow created ordinary execution: %+v", first)
	}
	if first.Configuration.WorkflowRevisionID != definition.CurrentRevisionID || first.Configuration.WorkflowRevisionPolicy != constants.WorkflowRevisionPolicyPinned {
		t.Fatal("occurrence was not frozen")
	}
	secondRevision, _, err := h.workflows.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "new", ExpectedRevisionID: definition.CurrentRevisionID, Source: strings.Replace(string(data), "parts.join(' ')", "parts.join('-')", 1), SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatal(err)
	}
	// 模拟 Workflow 接纳已提交、调度响应确认未提交的恢复窗口。
	pending := first.AutomaticRun
	pending.Admission = constants.AutomaticRunAdmissionPending
	pending.WorkflowRunID = ""
	if err := h.api.config.Tasks.UpdateRunAdmission(ctx, pending); err != nil {
		t.Fatal(err)
	}
	replay, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "same", "", map[string]any{"text": "Test"})
	if err != nil || replay.ID != first.ID || replay.WorkflowRunID != first.WorkflowRunID || replay.Configuration.WorkflowRevisionID == secondRevision.RevisionID {
		t.Fatalf("replay changed target: %+v %v", replay, err)
	}
	h.runWorkflowUntil(t, ctx, first.WorkflowRunID, constants.WorkflowRunStatusSucceeded)
	view, err := h.service.Get(ctx, h.wakerID, saved.TaskID)
	if err != nil || view.ActiveRun != nil || view.LastRun.State != constants.AutomaticRunStateCompleted {
		t.Fatalf("slot not released: %+v %v", view, err)
	}
	result, err := h.api.config.WorkflowRuns.GetRun(ctx, first.WorkflowRunID)
	if err != nil || result.Result.(map[string]any)["message"] != "TEST TEST!" || result.Context["triggerSource"] != "api" {
		t.Fatalf("actual computation: %+v %v", result, err)
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "same", "", map[string]any{"text": "Other"}); !errors.Is(err, status_error.AutomaticTaskRequestConflict) {
		t.Fatalf("different input replay: %v", err)
	}
	if _, err := h.api.TriggerAPI(ctx, saved.TaskID, saved.Token, "denied", "", map[string]any{"repeat": 3}); !errors.Is(err, status_error.BadRequest) && util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.AutomaticTaskInputInvalid.Key() {
		t.Fatalf("undeclared override: %v", err)
	}
}

func TestWorkflowWaitingKeepsSlotAndPinnedPolicyRejectsWrongOwner(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	source := `export const meta = {sdk:'ally.workflow/1',title:'等待输入',inputSchema:{type:'object',properties:{text:{type:'string'}}},phases:[{id:'review',title:'审核',detail:'人工审核',canExit:true}]};phase('review');const answer = await askUser('审核 '+args.text,{id:'approve',phase:'review',label:'确认',options:[{value:'yes',label:'是'},{value:'no',label:'否'}]});return answer;`
	definition, config := workflowTask(t, h, source)
	config.WorkflowRevisionPolicy = constants.WorkflowRevisionPolicyPinned
	config.WorkflowRevisionID = definition.CurrentRevisionID
	saved, err := h.service.SaveConfiguration(ctx, h.wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	first, err := h.api.TriggerManual(ctx, h.wakerID, saved.TaskID, "manual", map[string]any{"text": "NeuxBot"})
	if err != nil {
		t.Fatal(err)
	}
	h.runWorkflowUntil(t, ctx, first.WorkflowRunID, constants.WorkflowRunStatusWaiting)
	view, err := h.service.Get(ctx, h.wakerID, saved.TaskID)
	if err != nil || view.ActiveRun == nil || view.ActiveRun.State != constants.AutomaticRunStateWaitingInput {
		t.Fatalf("waiting lost slot: %+v %v", view, err)
	}
	if _, err := h.api.TriggerManual(ctx, h.wakerID, saved.TaskID, "second"); !errors.Is(err, status_error.AutomaticTaskBusy) {
		t.Fatalf("double trigger: %v", err)
	}
	if err := h.api.Delete(ctx, h.wakerID, saved.TaskID); !errors.Is(err, status_error.AutomaticTaskBusy) {
		t.Fatalf("deleted active workflow: %v", err)
	}
	if _, err := h.service.SaveConfiguration(ctx, "other-owner", "", 0, config, true); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.WorkflowResourceInvalid.Key() {
		t.Fatalf("wrong owner: %v", err)
	}
}

func TestWorkflowDefinitionDeletionProtectsDisabledTriggerDependencies(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	data, err := os.ReadFile("../../internal/workflow/dsl/testdata/f0.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	definition, config := workflowTask(t, h, string(data))
	config.Enabled = false
	saved, err := h.service.SaveConfiguration(ctx, h.wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteWorkflowFixture(h, ctx, definition.ID, definition.CurrentRevisionID); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.WorkflowStateConflict.Key() {
		t.Fatal("disabled trigger lost definition", err)
	}
	if err := h.api.Delete(ctx, h.wakerID, saved.TaskID); err != nil {
		t.Fatal(err)
	}
	if err := deleteWorkflowFixture(h, ctx, definition.ID, definition.CurrentRevisionID); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowHTTPInputsStayTypedAndTaskBindingsIndependent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	data, err := os.ReadFile("../../internal/workflow/dsl/testdata/f0.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(data), "text: { type: 'string' }", "segments: { type: 'array', items: { type: 'string' } }, text: { type: 'string' }", 1)
	_, config := workflowTask(t, h, source)
	config.WorkflowInputBindings["segments"] = workflowinput.Binding{Source: constants.WorkflowInputSourceAPI}
	config.WorkflowInputBindings["repeat"] = workflowinput.Binding{Source: constants.WorkflowInputSourceFixed, Value: 3}
	first, err := h.service.SaveConfiguration(ctx, h.wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	config.WorkflowInputBindings["repeat"] = workflowinput.Binding{Source: constants.WorkflowInputSourceFixed, Value: 4}
	second, err := h.service.SaveConfiguration(ctx, h.wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	view, err := h.service.Get(ctx, h.wakerID, first.TaskID)
	if err != nil || view.WorkflowInputBindings["repeat"].Value != float64(3) {
		t.Fatalf("sibling changed: %+v %v", view, err)
	}
	config.WorkflowInputBindings["repeat"] = workflowinput.Binding{Source: constants.WorkflowInputSourceFixed, Value: 5}
	if _, err := h.service.SaveConfiguration(ctx, h.wakerID, second.TaskID, 1, config, true); err != nil {
		t.Fatal(err)
	}
	url := "/v1/automatic-tasks/" + first.TaskID + "/runs"
	for _, body := range []string{`{"args":{}}`, `{"args":{"text":"x","repeat":3}}`, `{"args":{"text":"x","segments":"a,b"}}`, `{"args":{"text":"x","segments":[1]}}`, `{"args":{"text":"x","text":"y"}}`, `{"input":"","args":{"text":"x"}}`, `{"args":null}`} {
		response := callLocal(&h.api, http.MethodPost, url, first.Token, "retry-valid", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", body, response.Code, response.Body.String())
		}
		if _, exists, err := h.api.config.Tasks.FindRun(ctx, first.TaskID, "api:1:retry-valid"); err != nil || exists {
			t.Fatalf("invalid request consumed identity: %v %v", exists, err)
		}
	}
	response := callLocal(&h.api, http.MethodPost, url, first.Token, "retry-valid", `{"args":{"text":"O'Reilly","segments":["one","two"]}}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	var receipt map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	run, err := h.api.config.Tasks.GetRun(ctx, receipt["runId"].(string))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := h.api.config.WorkflowRuns.GetRun(ctx, run.WorkflowRunID)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Args.(map[string]any)["repeat"] != float64(3) || actual.Args.(map[string]any)["text"] != "O'Reilly" {
		t.Fatalf("workflow received different inputs: %+v", actual.Args)
	}
	if run.WorkflowResolvedArgs["repeat"] != float64(3) || run.WorkflowCallArgs["text"] != "O'Reilly" || len(run.WorkflowCallArgs["segments"].([]any)) != 2 {
		t.Fatalf("typed input lost: %+v", run)
	}
	replay := callLocal(&h.api, http.MethodPost, url, first.Token, "retry-valid", `{"args":{"segments":["one","two"],"text":"O'Reilly"}}`)
	if replay.Code != http.StatusAccepted {
		t.Fatalf("reordered replay failed: %s", replay.Body.String())
	}
	conflicting := callLocal(&h.api, http.MethodPost, url, first.Token, "retry-valid", `{"args":{"text":"O'Reilly","segments":["two","one"]}}`)
	if conflicting.Code != http.StatusConflict {
		t.Fatalf("array order not preserved: %d", conflicting.Code)
	}
}

// deleteWorkflowFixture 调用真实删除Operator，测试只装配仓储与共享命令边界。
func deleteWorkflowFixture(h *testHarness, ctx context.Context, definitionID, revisionID string) error {
	previousStore, previousRuns, previousState, previousTransactions := global.WorkflowControllerManager, global.WorkflowRunControllerManager, global.WorkflowState, global.TransactionControllerManager
	global.WorkflowControllerManager, global.WorkflowRunControllerManager, global.WorkflowState, global.TransactionControllerManager = h.workflowStore, h.api.config.WorkflowRuns, h.workflowState, h.api.config.Transactions
	defer func() {
		global.WorkflowControllerManager, global.WorkflowRunControllerManager, global.WorkflowState, global.TransactionControllerManager = previousStore, previousRuns, previousState, previousTransactions
	}()
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = httptest.NewRequest(http.MethodDelete, "/workflows/"+definitionID, nil).WithContext(ctx)
	operator := workflowapi.DeleteWorkflow{ID: definitionID, Body: workflowapi.DeleteWorkflowBody{ExpectedRevisionID: revisionID}}
	_, err := operator.Output(ginContext)
	return err
}
