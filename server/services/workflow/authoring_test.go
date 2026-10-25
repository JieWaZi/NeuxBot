package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcli"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

func authoringTestCLI(t *testing.T, s *Service, session models.WorkflowCreationSession) func([]string, any) (int, agentcapability.Response) {
	t.Helper()
	if s.chats == nil {
		session.ChatID = "authoring-chat"
		s.chats = &consumerChatBoundary{
			actual:  models.Execution{ID: session.ActiveExecutionID, ChatID: session.ChatID, Status: constants.ExecutionStatusRunning},
			summary: chat.ChatSummary{Chat: models.Chat{ID: session.ChatID, ActiveExecutionID: session.ActiveExecutionID}},
		}
	}
	if err := s.creationSessions.CreateCreationSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	registry := agentcapability.New(nil)
	t.Cleanup(registry.Close)
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{
		ExecutionID: "authoring", Bindings: s.AgentBindings(session.ID, session.AuthToken, constants.WorkflowCapabilityScopeCreation),
		Validate: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(revoke)
	activate()
	endpoint := httptest.NewServer(registry)
	t.Cleanup(endpoint.Close)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	connection, err := agentcapability.WriteConnection(root, "connection.json", agentcapability.Connection{
		ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: endpoint.URL + constants.AgentCLIPath,
		CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token,
	})
	if err != nil {
		t.Fatal(err)
	}
	return func(args []string, params any) (int, agentcapability.Response) {
		t.Helper()
		var input, output, failure bytes.Buffer
		if params != nil {
			if err := json.NewEncoder(&input).Encode(params); err != nil {
				t.Fatal(err)
			}
		}
		code := agentcli.Execute(context.Background(), args, agentcli.IO{
			In: &input, Out: &output, Err: &failure, ContextFile: connection,
		})
		body := output.Bytes()
		if code != 0 {
			if output.Len() != 0 {
				t.Fatal("failure wrote success output")
			}
			body = failure.Bytes()
		}
		var response agentcapability.Response
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatalf("invalid CLI result: %s: %v", body, err)
		}
		return code, response
	}
}

func TestAuthoringCLICombinesPreparationAndSavesWithDurableReceipt(t *testing.T) {
	ctx := context.Background()
	s, _ := workflowTestService(t)
	definition, err := fixtureDefinition(ctx, s.store, definitionFixture{RequestID: "prepare", Title: "Draft"})
	if err != nil {
		t.Fatal(err)
	}
	call := authoringTestCLI(t, s, models.WorkflowCreationSession{
		ID: "creation", ActiveExecutionID: "authoring", DefinitionID: definition.ID, AuthToken: "private",
	})
	code, available := call([]string{"capabilities"}, nil)
	if code != 0 {
		t.Fatal(available.Error)
	}
	found := false
	for _, item := range available.Data.(map[string]any)["operations"].([]any) {
		op := item.(map[string]any)
		if op["id"] == "workflow.sdk.get" {
			mapping := op["discovery"].(map[string]any)["primitiveTopics"].(map[string]any)
			found = mapping["functionTask"] == "function" && op["inputSchema"] == nil
		}
	}
	if !found {
		t.Fatal("capabilities lost the lightweight topic directory")
	}
	code, prepared := call([]string{"workflow", "authoring", "prepare", "--primitives", "functionTask"}, nil)
	if code != 0 {
		t.Fatalf("prepare failed: %+v", prepared)
	}
	data := prepared.Data.(map[string]any)
	current := data["workflow"].(map[string]any)
	content := data["content"].(string)
	t.Logf("minimal Function authoring guidance: %d bytes", len(content))
	if current["currentRevisionId"] != "" || current["source"] != "" ||
		len(data["resources"].(map[string]any)) != 0 || len(data["operations"].([]any)) != 2 ||
		!strings.Contains(content, "declare function functionTask") || strings.Contains(content, "declare function worker") ||
		strings.Contains(content, "# Worker outputs") || strings.Count(content, "This is a restricted DSL") != 1 {
		t.Fatalf("function preparation did not produce focused complete context: %v", data)
	}
	for _, args := range [][]string{
		{"workflow", "sdk", "get", "--topic", "functionTask"},
		{"workflow", "resources", "list", "--kind", "partners"},
	} {
		code, response := call(args, nil)
		if code == 0 || response.Error == nil || response.Error.Details["discovery"] == nil {
			t.Fatalf("invalid query lacks legal choices: %+v", response)
		}
	}
	source := `export const meta={sdk:'ally.workflow/1',title:'Echo',description:'Echo input',inputSchema:{type:'object',properties:{text:{type:'string',title:'Text',description:'Text to echo.'}},required:['text']},outputSchema:{type:'string'},phases:[{id:'echo',title:'Echo',detail:'Echo input',canExit:true}]};phase('echo');return await functionTask({id:'echo_text',label:'Echo text',phase:'echo',language:'javascript',input:args.text,code:'function main(input){return input;}'});`
	invalid := strings.Replace(source, "language:'javascript'", "outputSchema:meta.outputSchema,language:'javascript'", 1)
	code, rejected := call([]string{"workflow", "source", "save", "--input", "-", "--request-id", "invalid"}, map[string]any{
		"source": invalid, "expectedRevisionId": "",
	})
	if code == 0 || rejected.Error == nil || rejected.Error.Code != status_error.WorkflowSourceInvalid.Key() {
		t.Fatalf("invalid source saved: %+v", rejected)
	}
	diagnostics := rejected.Error.Details["diagnostics"].([]any)
	diagnostic := diagnostics[0].(map[string]any)
	if diagnostic["code"] != "STATIC_OPTION_REQUIRED" || diagnostic["field"] != "outputSchema" || diagnostic["hint"] == "" || diagnostic["range"] == nil {
		t.Fatalf("save failure discarded actionable diagnostics: %v", diagnostic)
	}
	definition, err = s.store.GetDefinition(ctx, definition.ID)
	if err != nil || definition.CurrentRevisionID != "" {
		t.Fatal("failed save changed the workflow head")
	}
	code, missing := call([]string{"receipts", "get", "--operation", "workflow.source.save", "--request-id", "invalid"}, nil)
	if code == 0 || missing.Error.Code != status_error.AgentReceiptNotFound.Key() {
		t.Fatalf("failed save produced a success receipt: %+v", missing)
	}
	file := filepath.Join(t.TempDir(), "workflow source.ts")
	if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"workflow", "source", "save", "--source", file, "--expected-revision-id", "", "--request-id", "save"}
	code, saved := call(args, nil)
	if code != 0 || saved.Completion != constants.AgentCLICompletionImmediate || saved.Data.(map[string]any)["saved"] != true {
		t.Fatalf("save without a separate validate failed: %+v", saved)
	}
	revision := saved.Data.(map[string]any)["revisionId"]
	for _, args := range [][]string{
		args, {"receipts", "get", "--operation", "workflow.source.save", "--request-id", "save"},
	} {
		code, replay := call(args, nil)
		if code != 0 || !replay.Replayed || replay.CommandID != saved.CommandID ||
			replay.Data.(map[string]any)["saved"] != true || replay.Data.(map[string]any)["revisionId"] != revision {
			t.Fatalf("durable receipt does not match original save: %+v", replay)
		}
	}
	code, conflict := call([]string{"workflow", "source", "save", "--input", "-", "--request-id", "stale"}, map[string]any{
		"source": strings.Replace(source, "Echo input", "Updated input", 1), "expectedRevisionId": "",
	})
	if code == 0 || conflict.Error.Code != status_error.WorkflowRevisionConflict.Key() || conflict.Error.RetryAdvice != "read_current_source" {
		t.Fatalf("CAS conflict lost recovery semantics: %+v", conflict)
	}
	code, unchanged := call([]string{"workflow", "source", "save", "--input", "-", "--request-id", "unchanged"}, map[string]any{
		"source": source, "expectedRevisionId": revision,
	})
	if code != 0 || unchanged.Data.(map[string]any)["unchanged"] != true || unchanged.Data.(map[string]any)["revisionId"] != revision {
		t.Fatalf("unchanged save created a new revision: %+v", unchanged)
	}
	for _, include := range []bool{false, true} {
		code, preview := call([]string{"workflow", "source", "validate", "--input", "-"}, map[string]any{
			"source": source, "includeManifest": include,
		})
		if code != 0 {
			t.Fatal(preview.Error)
		}
		result := preview.Data.(map[string]any)
		_, hasManifest := result["manifest"]
		if result["valid"] != true || result["summary"] == nil || hasManifest != include {
			t.Fatalf("preflight output selection failed: %v", result)
		}
	}
	code, latest := call([]string{"workflow", "authoring", "prepare", "--primitives", "functionTask"}, nil)
	if code != 0 || latest.Data.(map[string]any)["workflow"].(map[string]any)["source"] != source {
		t.Fatal("editing preparation lost the actual saved source")
	}
}

func TestAuthoringPreparationIncludesOnlySelectedResourcesAndGuides(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	for index := range 51 {
		if err := s.creationSessions.CreateAnnotation(context.Background(), models.WorkflowAnnotation{
			ID: fmt.Sprintf("%032d", index), DefinitionID: definition.ID,
			BaseRevisionID: definition.CurrentRevisionID, Text: "Requested change", CreatedAt: int64(index + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	call := authoringTestCLI(t, s, models.WorkflowCreationSession{
		ID: "creation", ActiveExecutionID: "authoring", DefinitionID: definition.ID, AuthToken: "private",
	})
	code, response := call([]string{"workflow", "authoring", "prepare", "--primitives", "worker,functionTask", "--features", "files", "--language", "python"}, nil)
	if code != 0 {
		t.Fatalf("selected authoring context failed: %+v", response)
	}
	data := response.Data.(map[string]any)
	annotations := data["annotations"].(map[string]any)
	if len(annotations["items"].([]any)) != 50 || annotations["nextCursor"] == "" {
		t.Fatalf("preparation lost the bounded annotation cursor: %v", annotations)
	}
	code, remaining := call([]string{"workflow", "annotations", "list", "--status", "pending", "--cursor", annotations["nextCursor"].(string)}, nil)
	if code != 0 || len(remaining.Data.(map[string]any)["items"].([]any)) != 1 {
		t.Fatalf("prepared cursor did not reach the remaining annotation: %+v", remaining)
	}
	resources := data["resources"].(map[string]any)
	for _, kind := range []string{"wakers", "runtimes", "accounts", "functionLanguages"} {
		page, ok := resources[kind].(map[string]any)
		if !ok || page["kind"] != kind || page["items"] == nil {
			t.Fatalf("missing bounded resource page: %s: %v", kind, resources)
		}
	}
	content := data["content"].(string)
	if len(resources) != 4 || len(data["guideIds"].([]any)) != 2 ||
		!strings.Contains(content, "# Choose the actual deliverable") ||
		strings.Count(content, constants.WorkflowResultPageDecisionInstructions) != 1 ||
		strings.Count(content, "saved:true, revisionId and sourceDigest") != 1 ||
		strings.Contains(content, "# Workflow SDK: presentation") {
		t.Fatalf("selected features failed to choose guidance: %v", data)
	}
	for _, tc := range []struct {
		// primitive 是本次创作使用的真实 DSL 调用。
		primitive string
		// feature 是只应加载对应资料的业务能力。
		feature string
		// wantPresentation 表示必须包含完整页面 Catalog。
		wantPresentation bool
	}{
		{primitive: "functionTask", feature: "files"},
		{primitive: "worker", feature: "presentation", wantPresentation: true},
	} {
		t.Run(tc.primitive+"/"+tc.feature, func(t *testing.T) {
			code, prepared := call([]string{"workflow", "authoring", "prepare", "--primitives", tc.primitive, "--features", tc.feature}, nil)
			if code != 0 {
				t.Fatalf("focused preparation failed: %+v", prepared)
			}
			data := prepared.Data.(map[string]any)
			content := data["content"].(string)
			t.Logf("%s/%s authoring guidance: %d bytes", tc.primitive, tc.feature, len(content))
			if len(data["guideIds"].([]any)) != 1 ||
				strings.Contains(content, "# Choose the actual deliverable") ||
				strings.Contains(content, "# Workflow SDK: presentation") != tc.wantPresentation {
				t.Fatalf("preparation included unrelated guidance: %v", data["guideIds"])
			}
			if tc.primitive == "functionTask" &&
				(len(data["resources"].(map[string]any)) != 0 || strings.Contains(content, "await worker")) {
				t.Fatal("function file authoring loaded worker resources or examples")
			}
		})
	}
	for _, scope := range []constants.WorkflowCapabilityScope{
		constants.WorkflowCapabilityScopeConsumer, constants.WorkflowCapabilityScopeResult,
	} {
		for _, binding := range s.AgentBindings("other", "token", scope) {
			if binding.Operation == "workflow.authoring.prepare" {
				t.Fatal("authoring preparation escaped its creation scope")
			}
		}
	}
}

func TestAuthoringSaveRollbackCannotReturnSavedOrDurableReceipt(t *testing.T) {
	s, store := workflowTestService(t)
	definition, err := fixtureDefinition(context.Background(), s.store, definitionFixture{RequestID: "rollback", Title: "Draft"})
	if err != nil {
		t.Fatal(err)
	}
	call := authoringTestCLI(t, s, models.WorkflowCreationSession{
		ID: "creation", ActiveExecutionID: "authoring", DefinitionID: definition.ID, AuthToken: "private",
	})
	s.store = receiptFailingStore{WorkflowController: store}
	code, failed := call([]string{"workflow", "source", "save", "--input", "-", "--request-id", "rollback"}, map[string]any{
		"source": workflowTestSource(t), "expectedRevisionId": "",
	})
	if code == 0 || failed.OK || failed.Data != nil || failed.Error == nil {
		t.Fatalf("rolled-back save returned success: %+v", failed)
	}
	code, receipt := call([]string{"receipts", "get", "--operation", "workflow.source.save", "--request-id", "rollback"}, nil)
	if code == 0 || receipt.Error.Code != status_error.AgentReceiptNotFound.Key() {
		t.Fatalf("rolled-back save left a receipt: %+v", receipt)
	}
	definition, err = store.GetDefinition(context.Background(), definition.ID)
	if err != nil || definition.CurrentRevisionID != "" || definition.Ordinal != 0 {
		t.Fatal("rolled-back save left a new current revision")
	}
}

func TestAuthoringCapabilityFollowsExecutionAndRejectsLateSave(t *testing.T) {
	ctx := context.Background()
	s, _ := workflowTestService(t)
	definition, err := fixtureDefinition(ctx, s.store, definitionFixture{RequestID: "lifecycle", Title: "等待回答"})
	if err != nil {
		t.Fatal(err)
	}
	// 模拟已等待两小时的执行，继续读取与保存不需要续期。
	old := time.Now().Add(-2 * time.Hour).UnixMilli()
	boundary := &consumerChatBoundary{
		actual:  models.Execution{ID: "authoring", ChatID: "chat", Status: constants.ExecutionStatusRunning},
		summary: chat.ChatSummary{Chat: models.Chat{ID: "chat", ActiveExecutionID: "authoring"}},
	}
	s.chats = boundary
	call := authoringTestCLI(t, s, models.WorkflowCreationSession{
		ID: "creation", DefinitionID: definition.ID, ChatID: "chat", ActiveExecutionID: "authoring", AuthToken: "private", CreatedAt: old, UpdatedAt: old,
	})
	if code, response := call([]string{"workflow", "source", "get"}, nil); code != 0 {
		t.Fatalf("等待后读取失败: %+v", response)
	}
	params := map[string]any{"source": workflowTestSource(t), "expectedRevisionId": ""}
	if code, response := call([]string{"workflow", "source", "save", "--input", "-", "--request-id", "long-wait"}, params); code != 0 {
		t.Fatalf("等待后保存失败: %+v", response)
	}
	for _, status := range []constants.ExecutionStatus{constants.ExecutionStatusCompleted, constants.ExecutionStatusCanceled, constants.ExecutionStatusFailed} {
		boundary.actual.Status = status
		boundary.summary.Chat.ActiveExecutionID = ""
		code, response := call([]string{"workflow", "source", "get"}, nil)
		if code == 0 || response.Error == nil || response.Error.Code != status_error.WorkflowAuthoringFinished.Key() || response.Error.RetryAdvice != "new_authoring_execution" {
			t.Fatalf("终态仍有权限或缺少恢复提示: %+v", response)
		}
	}
	boundary.actual.Status = constants.ExecutionStatusRunning
	boundary.summary.Chat.ActiveExecutionID = "replacement"
	code, response := call([]string{"workflow", "source", "get"}, nil)
	if code == 0 || response.Error == nil || response.Error.Code != status_error.WorkflowAuthoringSuperseded.Key() {
		t.Fatalf("旧执行仍有权限: %+v", response)
	}
	// 直接进入保存事务，模拟初次能力校验后执行才被替换或结束。
	command := SaveSourceCommand{DefinitionID: definition.ID, RequestID: "late-save", Source: workflowTestSource(t), SDKVersion: constants.WorkflowSDKVersion, CreationSessionID: "creation", CreationExecutionID: "authoring", CreationToken: "private"}
	if _, _, err := s.SaveSource(ctx, command); err != status_error.WorkflowAuthoringSuperseded {
		t.Fatalf("迟到保存未阻止: %v", err)
	}
	boundary.summary.Chat.ActiveExecutionID = "authoring"
	boundary.actual.Status = constants.ExecutionStatusCanceled
	if _, _, err := s.SaveSource(ctx, command); err != status_error.WorkflowAuthoringFinished {
		t.Fatalf("结束后保存未阻止: %v", err)
	}
	boundary.actual.Status = constants.ExecutionStatusRunning
	session, err := s.creationSessions.GetCreationSession(ctx, "creation")
	if err != nil {
		t.Fatal(err)
	}
	session.AuthToken = "replacement-token"
	if err := s.creationSessions.UpdateCreationSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	code, response = call([]string{"workflow", "source", "get"}, nil)
	if code == 0 || response.Error == nil || response.Error.Code != status_error.WorkflowAuthoringSuperseded.Key() {
		t.Fatalf("旧凭证仍有权限: %+v", response)
	}
}
