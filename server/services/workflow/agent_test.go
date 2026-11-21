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
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	agentcli "github.com/JieWaZi/NeuxBot/server/internal/agent/cli"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

type workflowWaitRunStore struct {
	// WorkflowRunController 读取原运行的其余详情。
	controllers.WorkflowRunController
	// hub 在首次读取后发布运行进入终态的变化。
	hub *events.Hub
	// reads 记录观察读取的次数。
	reads int
}

func (s *workflowWaitRunStore) GetRun(_ context.Context, id string) (models.WorkflowRun, error) {
	s.reads++
	if s.reads == 1 {
		s.hub.Publish(events.Event{Topic: "workflow", Resource: events.Resource{Kind: "workflow", ID: id}})
		return models.WorkflowRun{ID: id, Status: constants.WorkflowRunStatusRunning}, nil
	}
	return models.WorkflowRun{ID: id, Status: constants.WorkflowRunStatusFailed}, nil
}

func TestAgentWorkflowWaitReturnsTheSnapshotThatReachedTerminal(t *testing.T) {
	s, _ := workflowTestService(t)
	hub := events.New()
	t.Cleanup(hub.Close)
	s.events = hub
	store := &workflowWaitRunStore{WorkflowRunController: s.runs, hub: hub}
	s.runs = store
	_, call := bindLocalWorkflowFixture(t, s, consumerFixtureChat(t, "observer"), nil)
	result, err := call("workflow.runs.wait", "", map[string]any{"runId": "observed-run", "timeoutMs": 2000})
	if err != nil {
		t.Fatal(err)
	}
	data := result.Data.(map[string]any)
	run := data["resource"].(map[string]any)["run"].(map[string]any)
	if data["completed"] != true || run["status"] != constants.WorkflowRunStatusFailed || store.reads != 2 {
		t.Fatalf("observation returned inconsistent run: %+v, reads = %d", data, store.reads)
	}
}

func TestWorkflowRunReadsReturnActiveAndTerminalSnapshots(t *testing.T) {
	s, _ := workflowTestService(t)
	hub := events.New()
	t.Cleanup(hub.Close)
	s.events = hub
	conversation := consumerFixtureChat(t, "observer")
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	_, call := bindLocalWorkflowFixture(t, s, conversation, nil)
	for id, status := range map[string]constants.WorkflowRunStatus{"active": constants.WorkflowRunStatusRunning, "finished": constants.WorkflowRunStatusSucceeded} {
		insertWorkflowChatHistory(t, s, models.WorkflowRun{ID: id, DefinitionID: definition.ID, RevisionID: definition.CurrentRevisionID, SourceChatID: conversation.ID, Status: status})
	}
	for _, operation := range []string{"workflow.runs.get", "workflow.runs.wait"} {
		for id, status := range map[string]constants.WorkflowRunStatus{"active": constants.WorkflowRunStatusRunning, "finished": constants.WorkflowRunStatusSucceeded} {
			params := map[string]any{"runId": id}
			if operation == "workflow.runs.wait" {
				params["timeoutMs"] = 1
			}
			result, err := call(operation, "", params)
			if err != nil {
				t.Fatal(err)
			}
			details := result.Data.(map[string]any)
			if operation == "workflow.runs.wait" {
				if details["completed"] != (status == constants.WorkflowRunStatusSucceeded) {
					t.Errorf("%s(%s) returned an incorrect wait result: %+v", operation, id, details)
				}
				details = details["resource"].(map[string]any)
			}
			if run := details["run"].(map[string]any); run["id"] != id || run["status"] != status {
				t.Errorf("%s(%s) returned the wrong snapshot: %+v", operation, id, run)
			}
		}
	}
}

func TestAgentRunFreezesFilesFromCLICWDAndConflictsOnChangedBytes(t *testing.T) {
	testAgentRunFileScope(t, false)
}

func TestAgentRunFreezesExactReferencedFileWithoutParentScope(t *testing.T) {
	testAgentRunFileScope(t, true)
}

func TestWorkflowValidatesInputFilesWithoutGlobalCommandLock(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, `export const meta={sdk:'bot.workflow',title:'文件输入',description:'验证文件路径',inputSchema:{type:'object',properties:{file:{type:'string',format:'file',title:'文件',description:'选择已有的文件。'}},required:['file']},phases:[{id:'work',title:'执行',detail:'读取文件',canExit:true}]};phase('work');return args.file;`)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	s.state.Commands.Lock()
	defer s.state.Commands.Unlock()
	result := make(chan error, 1)
	go func() {
		_, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "invalid-input", Args: map[string]any{"file": "missing.txt"}})
		result <- err
	}()
	select {
	case err := <-result:
		if err != status_error.WorkflowFileInvalid {
			t.Fatalf("invalid file did not produce a file error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("preparing inputs waited for the global workflow command lock")
	}
}

func testAgentRunFileScope(t *testing.T, exact bool) {
	t.Helper()
	ctx := context.Background()
	s, _ := workflowTestService(t)
	s.dataDirectory = t.TempDir()
	source := `export const meta={sdk:'bot.workflow',title:'文件交付',description:'交付本次报告',inputSchema:{type:'object',properties:{files:{type:'array',title:'报告',description:'选择本次报告。',items:{type:'string',format:'file'}},note:{type:'string',title:'备注',description:'填写备注。'}},required:['files']},outputSchema:{type:'string',format:'file'},phases:[{id:'deliver',title:'交付',detail:'交付报告文件',canExit:true}]};phase('deliver');return args.files[0];`
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
	conversation := consumerFixtureChat(t, "chat")
	_, lease := bindConsumerFixture(t, s, conversation, definition.ID, "execution", "input")
	s.executions = consumerExecutionBoundary{}
	s.chats = &consumerChatBoundary{actual: models.Execution{ID: lease.ID, ChatID: lease.ChatID, Status: constants.ExecutionStatusRunning, UserMessageID: "input", PermissionLevel: constants.PermissionLevelDefault}, summary: chat.ChatSummary{Chat: models.Chat{ID: lease.ChatID, ActiveExecutionID: lease.ID, AdditionalWorkingDirectories: []string{root}}}}
	registry := capability.New(nil)
	defer registry.Close()
	roots, files := []string{root}, []string(nil)
	if exact {
		roots, files = nil, []string{filepath.Join(sub, "report.txt")}
	}
	grant, activate, revoke, err := registry.Prepare(capability.GrantSpec{ExecutionID: lease.ID, FileRoots: roots, FilePaths: files, ManagedSpace: root, Bindings: s.consumerAgentBindings(lease), Validate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	activate()
	endpoint := httptest.NewServer(registry)
	defer endpoint.Close()
	connection, err := capability.WriteConnection(root, "connection.json", capability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: endpoint.URL + constants.AgentCLIPath, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(constants.AgentCLIContextEnvironment, connection)
	t.Chdir(sub)
	call := func(arguments []string, params any) (int, capability.Response) {
		t.Helper()
		var input, output, failure bytes.Buffer
		if params != nil {
			if err := json.NewEncoder(&input).Encode(params); err != nil {
				t.Fatal(err)
			}
		}
		code := agentcli.Execute(ctx, arguments, agentcli.IO{In: &input, Out: &output, Err: &failure})
		body := output.Bytes()
		if code != 0 {
			body = failure.Bytes()
		}
		var response capability.Response
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
	if exact {
		outside = filepath.Join(sub, "unreferenced.txt")
		if err := os.WriteFile(outside, []byte("not explicitly referenced"), 0600); err != nil {
			t.Fatal(err)
		}
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
	if !exact {
		settings := filepath.Join(root, ".claude", "settings.json")
		if err := os.Mkdir(filepath.Dir(settings), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(settings, []byte("internal configuration"), 0600); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(sub, "settings.json")
		if err := os.Symlink(settings, alias); err != nil {
			t.Fatal(err)
		}
		invalid := map[string]any{"definitionId": definition.ID, "revisionId": definition.CurrentRevisionID, "args": map[string]any{"files": []string{alias}}}
		code, rejected := call([]string{"workflow", "runs", "start", "--request-id", "internal", "--input", "-"}, invalid)
		if code == 0 || rejected.Error == nil || rejected.Error.Code != status_error.WorkflowFileInvalid.Key() {
			t.Fatalf("internal space file was frozen through an alias: %+v", rejected)
		}
	}
	concurrent, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resume := make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	defer release()
	paused := &pausedAgentDefinition{WorkflowController: s.store, paused: make(chan struct{}), resume: resume}
	s.store = paused
	loser := make(chan capability.Response, 1)
	go func() {
		_, response := call(arguments, params)
		loser <- response
	}()
	select {
	case <-paused.paused:
	case <-concurrent.Done():
		t.Fatal("first candidate never reached run admission")
	}
	code, first := call(arguments, params)
	release()
	if code != 0 {
		t.Fatalf("file admission failed: %+v", first.Error)
	}
	select {
	case response := <-loser:
		if response.CommandID != first.CommandID || response.Data.(map[string]any)["runId"] != first.Data.(map[string]any)["runId"] {
			t.Fatalf("concurrent file candidate did not replay the winning receipt: %+v", response)
		}
	case <-concurrent.Done():
		t.Fatal("first candidate never returned its receipt")
	}
	runID, _ := first.Data.(map[string]any)["runId"].(string)
	run, err := s.runs.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	args := run.Args.(map[string]any)
	frozen := args["files"].([]any)[0].(string)
	managed, err := filepath.EvalSymlinks(s.dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	internal := filepath.Join(managed, "WorkflowWorkspaces", definition.ID)
	if !strings.HasPrefix(frozen, internal+string(filepath.Separator)) {
		t.Fatalf("input snapshot escaped workflow storage: %s", frozen)
	}
	if body, err := os.ReadFile(frozen); err != nil || string(body) != "actual child report" || args["note"] != "report.txt" {
		t.Fatalf("incorrect input resolution: args=%v body=%q err=%v", args, body, err)
	}
	info, err := os.Stat(frozen)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("input lost private file permissions: %v %v", info, err)
	}
	code, rejected = call([]string{"workflow", "runs", "start", "--request-id", "second-run", "--input", "-"}, params)
	if code == 0 || rejected.Error == nil || rejected.Error.Code != status_error.WorkflowRequestConflict.Key() {
		t.Fatalf("second run escaped the same-turn admission check: %+v", rejected)
	}
	acceptedInputs, err := os.ReadDir(filepath.Join(internal, constants.WorkflowInputFileDirectoryName))
	if err != nil || len(acceptedInputs) != 1 || acceptedInputs[0].Name() != runID {
		t.Fatalf("losing or rejected candidates retained input files: %+v %v", acceptedInputs, err)
	}
	code, replay := call(arguments, params)
	if code != 0 || !replay.Replayed || replay.CommandID != first.CommandID {
		t.Fatalf("unchanged files did not replay: %+v", replay)
	}
	changedSource := strings.Replace(source, "交付本次报告", "交付更新后的报告", 1)
	saved, _, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, ExpectedRevisionID: definition.CurrentRevisionID, RequestID: "new-head", Source: changedSource, SDK: constants.WorkflowSDKIdentifier})
	if err != nil || saved.RevisionID == definition.CurrentRevisionID {
		t.Fatalf("new revision was not created: %+v %v", saved, err)
	}
	code, fixedReplay := call(arguments, params)
	if code != 0 || !fixedReplay.Replayed || fixedReplay.CommandID != first.CommandID || fixedReplay.Data.(map[string]any)["revisionId"] != definition.CurrentRevisionID {
		t.Fatalf("selected revision replay followed a new head: %+v", fixedReplay)
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
	entries, err := os.ReadDir(filepath.Join(internal, constants.WorkflowInputStagingDirectoryName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("temporary input snapshot leaked")
	}
}

// pausedAgentDefinition 在读取首个版本快照后暂停，允许并发发布新版和接纳同一个固定版本。
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

func TestAgentConcurrentFixedRevisionAdmissionReplaysWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, _ := workflowTestService(t)
	s.dataDirectory = t.TempDir()
	source := workflowTestSource(t)
	definition := savedTestWorkflow(t, s, source)
	conversation := consumerFixtureChat(t, "chat")
	_, lease := bindConsumerFixture(t, s, conversation, definition.ID, "execution", "input")
	s.executions = consumerExecutionBoundary{}
	s.chats = &consumerChatBoundary{actual: models.Execution{ID: lease.ID, ChatID: lease.ChatID, Status: constants.ExecutionStatusRunning, UserMessageID: "input", PermissionLevel: constants.PermissionLevelDefault}, summary: chat.ChatSummary{Chat: models.Chat{ID: lease.ChatID, ActiveExecutionID: lease.ID, WorkspaceDirectory: conversation.WorkspaceDirectory}}}
	var start capability.Binding
	for _, binding := range s.consumerAgentBindings(lease) {
		if binding.Operation == "workflow.runs.start" {
			start = binding
		}
	}
	request := capability.Request{Operation: start.Operation, RequestID: "same", Params: map[string]any{"definitionId": definition.ID, "revisionId": definition.CurrentRevisionID, "args": map[string]any{"text": "same"}}}
	resume := make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	defer release()
	paused := &pausedAgentDefinition{WorkflowController: s.store, paused: make(chan struct{}), resume: resume}
	s.store = paused
	firstResults, firstErrors := make(chan capability.Result, 1), make(chan error, 1)
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
	saved, _, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, ExpectedRevisionID: definition.CurrentRevisionID, RequestID: "revision", Source: strings.Replace(source, "纯计算验收", "第二版", 1), SDK: constants.WorkflowSDKIdentifier})
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
		if err := <-firstErrors; err != nil || first.CommandID != second.CommandID ||
			first.Data.(map[string]any)["runId"] != second.Data.(map[string]any)["runId"] ||
			first.Data.(map[string]any)["revisionId"] != definition.CurrentRevisionID {
			t.Fatalf("concurrent admission did not replay the selected revision: first=%+v second=%+v err=%v", first, second, err)
		}
	case <-ctx.Done():
		t.Fatal("first admission did not finish")
	}
}
