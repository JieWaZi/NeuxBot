package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcli"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestAgentRunFreezesFilesFromCLICWDAndConflictsOnChangedBytes(t *testing.T) {
	ctx := context.Background()
	s, _ := workflowTestService(t)
	s.dataDirectory = t.TempDir()
	source := `export const meta={sdk:'ally.workflow/1',title:'文件交付',description:'交付本次报告',inputSchema:{type:'object',properties:{files:{type:'array',title:'报告',description:'选择本次报告。',items:{type:'string',format:'file'}},note:{type:'string',title:'备注',description:'填写备注。'}},required:['files']},outputSchema:{type:'string',format:'file'},phases:[{id:'deliver',title:'交付',detail:'交付报告文件',canExit:true}]};phase('deliver');return args.files[0];`
	definition := savedTestWorkflow(t, s, source)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "子目录 with spaces")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{filepath.Join(root, "report.txt"): "wrong root file", filepath.Join(sub, "report.txt"): "actual child report"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lease := models.WorkflowConsumerLease{ID: "execution", ChatID: "chat", AuthToken: "consumer-token", DefinitionIDs: []string{definition.ID}, DeadlineAt: time.Now().Add(time.Hour).UnixMilli()}
	if err := s.consumers.CreateConsumer(ctx, lease); err != nil {
		t.Fatal(err)
	}
	s.executions = consumerExecutionBoundary{}
	s.chats = &consumerChatBoundary{actual: models.Execution{ID: lease.ID, ChatID: lease.ChatID, Status: constants.ExecutionStatusRunning, PermissionLevel: constants.PermissionLevelDefault}, summary: chat.ChatSummary{Chat: models.Chat{ID: lease.ChatID, ActiveExecutionID: lease.ID, AdditionalWorkingDirectories: []string{root}}}}
	registry := agentcapability.New(nil)
	defer registry.Close()
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: lease.ID, FileRoots: []string{root}, Bindings: s.AgentBindings(lease.ID, lease.AuthToken, constants.WorkflowCapabilityScopeConsumer), Validate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	activate()
	endpoint := httptest.NewServer(registry)
	defer endpoint.Close()
	connection, err := agentcapability.WriteConnection(root, "connection.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: endpoint.URL + constants.AgentCLIPath, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	call := func(arguments []string, params any) (int, agentcapability.Response) {
		t.Helper()
		var input, output, failure bytes.Buffer
		if params != nil {
			if err := json.NewEncoder(&input).Encode(params); err != nil {
				t.Fatal(err)
			}
		}
		code := agentcli.Execute(ctx, arguments, agentcli.IO{In: &input, Out: &output, Err: &failure, ContextFile: connection})
		body := output.Bytes()
		if code != 0 {
			body = failure.Bytes()
		}
		var response agentcapability.Response
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatalf("invalid CLI response: %s %s %v", &output, &failure, err)
		}
		return code, response
	}
	arguments := []string{"workflow", "runs", "start", "--request-id", "report", "--input", "-"}
	params := map[string]any{"definitionId": definition.ID, "revisionId": definition.CurrentRevisionID, "args": map[string]any{"files": []string{"report.txt"}, "note": "report.txt"}}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("not authorized"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := map[string]any{"definitionId": definition.ID, "revisionId": definition.CurrentRevisionID, "args": map[string]any{"files": []string{"report.txt", outside}}}
	code, rejected := call([]string{"workflow", "runs", "start", "--request-id", "outside", "--input", "-"}, invalid)
	if code == 0 || rejected.Error == nil || rejected.Error.Code != status_error.WorkflowFileInvalid.Key() {
		t.Fatalf("input escaped the grant roots: %+v", rejected)
	}
	code, missing := call([]string{"receipts", "get", "--operation", "workflow.runs.start", "--request-id", "outside"}, nil)
	if code == 0 || missing.Error == nil || missing.Error.Code != status_error.AgentReceiptNotFound.Key() {
		t.Fatalf("rejected input produced a receipt: %+v", missing)
	}
	code, first := call(arguments, params)
	if code != 0 {
		t.Fatalf("file admission failed: %+v", first.Error)
	}
	runID, _ := first.Data.(map[string]any)["runId"].(string)
	run, err := s.runs.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	args := run.Args.(map[string]any)
	frozen := args["files"].([]any)[0].(string)
	if body, err := os.ReadFile(frozen); err != nil || string(body) != "actual child report" || args["note"] != "report.txt" {
		t.Fatalf("incorrect input resolution: args=%v body=%q err=%v", args, body, err)
	}
	code, replay := call(arguments, params)
	if code != 0 || !replay.Replayed || replay.CommandID != first.CommandID {
		t.Fatalf("unchanged files did not replay: %+v", replay)
	}
	currentArguments := []string{"workflow", "runs", "start", "--request-id", "current", "--input", "-"}
	currentParams := map[string]any{"definitionId": definition.ID, "revisionId": "", "args": params["args"]}
	code, current := call(currentArguments, currentParams)
	if code != 0 {
		t.Fatalf("current revision admission failed: %+v", current)
	}
	changedSource := strings.Replace(source, "交付本次报告", "交付更新后的报告", 1)
	saved, _, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, ExpectedRevisionID: definition.CurrentRevisionID, RequestID: "new-head", Source: changedSource, SDKVersion: constants.WorkflowSDKVersion})
	if err != nil || saved.RevisionID == definition.CurrentRevisionID {
		t.Fatalf("new revision was not created: %+v %v", saved, err)
	}
	code, currentReplay := call(currentArguments, currentParams)
	if code != 0 || !currentReplay.Replayed || currentReplay.CommandID != current.CommandID || currentReplay.Data.(map[string]any)["revisionId"] != definition.CurrentRevisionID {
		t.Fatalf("current revision replay followed a new head: %+v", currentReplay)
	}
	if err := os.WriteFile("report.txt", []byte("changed report"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, conflict := call(arguments, params)
	if code == 0 || conflict.Error == nil || conflict.Error.Code != status_error.AgentCommandRequestConflict.Key() {
		t.Fatalf("changed file replayed original run: %+v", conflict)
	}
	if body, err := os.ReadFile(frozen); err != nil || string(body) != "actual child report" {
		t.Fatalf("original input was replaced: %q %v", body, err)
	}
	if err := os.Remove("report.txt"); err != nil {
		t.Fatal(err)
	}
	code, receipt := call([]string{"receipts", "get", "--operation", "workflow.runs.start", "--request-id", "report"}, nil)
	if code != 0 || receipt.CommandID != first.CommandID {
		t.Fatalf("receipt depended on deleted source file: %+v", receipt)
	}
	entries, err := os.ReadDir(s.dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "workflow-input-staging-") {
			t.Fatal("temporary input snapshot leaked")
		}
	}
}

// pausedAgentDefinition 在读取首个版本快照后暂停，确定性覆盖跨版本并发接纳。
type pausedAgentDefinition struct {
	// WorkflowController 保留真实 SQLite 仓储行为。
	controllers.WorkflowController
	// taken 保证只暂停第一份定义快照。
	taken atomic.Bool
	// paused 通知测试首个调用已经取得旧版本。
	paused chan struct{}
	// resume 允许首个调用继续处理已经取得的快照。
	resume <-chan struct{}
}

func (s *pausedAgentDefinition) GetDefinition(ctx context.Context, id string) (models.WorkflowDefinition, error) {
	definition, err := s.WorkflowController.GetDefinition(ctx, id)
	if err == nil && s.taken.CompareAndSwap(false, true) {
		close(s.paused)
		select {
		case <-s.resume:
		case <-ctx.Done():
			return models.WorkflowDefinition{}, status_error.DatabaseOperationCancelled
		}
	}
	return definition, err
}

func TestAgentConcurrentCurrentRevisionAdmissionReplaysWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, _ := workflowTestService(t)
	s.dataDirectory = t.TempDir()
	source := workflowTestSource(t)
	definition := savedTestWorkflow(t, s, source)
	lease := models.WorkflowConsumerLease{ID: "execution", ChatID: "chat", AuthToken: "token", DefinitionIDs: []string{definition.ID}, DeadlineAt: time.Now().Add(time.Hour).UnixMilli()}
	if err := s.consumers.CreateConsumer(ctx, lease); err != nil {
		t.Fatal(err)
	}
	s.executions = consumerExecutionBoundary{}
	s.chats = &consumerChatBoundary{actual: models.Execution{ID: lease.ID, ChatID: lease.ChatID, Status: constants.ExecutionStatusRunning, PermissionLevel: constants.PermissionLevelDefault}, summary: chat.ChatSummary{Chat: models.Chat{ID: lease.ChatID, ActiveExecutionID: lease.ID, AdditionalWorkingDirectories: []string{t.TempDir()}}}}
	var start agentcapability.Binding
	for _, binding := range s.AgentBindings(lease.ID, lease.AuthToken, constants.WorkflowCapabilityScopeConsumer) {
		if binding.Operation == "workflow.runs.start" {
			start = binding
		}
	}
	request := agentcapability.Request{Operation: start.Operation, RequestID: "same", Params: map[string]any{"definitionId": definition.ID, "revisionId": "", "args": map[string]any{"text": "same"}}}
	resume := make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	defer release()
	paused := &pausedAgentDefinition{WorkflowController: s.store, paused: make(chan struct{}), resume: resume}
	s.store = paused
	firstResults, firstErrors := make(chan agentcapability.Result, 1), make(chan error, 1)
	go func() {
		value, err := start.Call(ctx, request)
		firstResults <- value
		firstErrors <- err
	}()
	select {
	case <-paused.paused:
	case <-ctx.Done():
		t.Fatal("first admission did not read its revision")
	}
	saved, _, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, ExpectedRevisionID: definition.CurrentRevisionID, RequestID: "revision", Source: strings.Replace(source, "纯计算验收", "第二版", 1), SDKVersion: constants.WorkflowSDKVersion})
	if err != nil || saved.RevisionID == definition.CurrentRevisionID {
		t.Fatalf("new revision was not created: %+v %v", saved, err)
	}
	second, err := start.Call(ctx, request)
	release()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case first := <-firstResults:
		if err := <-firstErrors; err != nil || first.CommandID != second.CommandID || !first.Replayed || first.Data.(map[string]any)["revisionId"] != saved.RevisionID {
			t.Fatalf("old revision admission did not replay the winner: first=%+v second=%+v err=%v", first, second, err)
		}
	case <-ctx.Done():
		t.Fatal("first admission did not finish")
	}
}
