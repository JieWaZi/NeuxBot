package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestWorkflowChatSummaryRunsOnRequestOnce(t *testing.T) {
	for _, replyLag := range []int64{-1, 1} {
		name := "before_run_terminal"
		if replyLag > 0 {
			name = "after_run_terminal_without_observation"
		}
		t.Run(name, func(t *testing.T) { testWorkflowChatSummaryRunsOnRequestOnce(t, replyLag) })
	}
}

func testWorkflowChatSummaryRunsOnRequestOnce(t *testing.T, replyLag int64) {
	t.Helper()
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	conversation := consumerFixtureChat(t, "chat")
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "execution", "original-input")
	response, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{"text": "NeuxBot"}})
	if err != nil {
		t.Fatal(err)
	}
	run := runTicks(t, s, unwrapWorkflow(response)["id"].(string), 8)
	if run.CompletedAt == nil {
		t.Fatal("run has no terminal receipt")
	}
	replyEnded := *run.CompletedAt + replyLag
	actual.Status, actual.CompletedAt = constants.ExecutionStatusCompleted, &replyEnded
	boundary := &deliveryChatBoundary{
		actual:  actual,
		records: []eventlog.Record{coreTerminalHTTPResponseRecord(t, actual.ID, "other-run")},
	}
	s.chats = boundary
	calls := 0
	s.summarizeOutput = func(_ context.Context, _ models.WorkflowExecutionDefaults, evidence string) (string, error) {
		calls++
		if !strings.Contains(evidence, "NEUXBOT NEUXBOT!") || !strings.Contains(evidence, "业务任务") {
			t.Fatalf("summary did not use original input and returned result: %s", evidence)
		}
		return "流程已完成，实际结果为 NEUXBOT NEUXBOT!", nil
	}
	s.drainChatDeliveries(ctx, "")
	if calls != 0 || boundary.published.Summary != "" {
		t.Fatalf("background delivery generated a summary: calls=%d delivery=%+v", calls, boundary.published)
	}
	for range 2 {
		if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || boundary.published.SourceID != run.ID || boundary.published.ExecutionID != actual.ID || boundary.published.Summary != "流程已完成，实际结果为 NEUXBOT NEUXBOT!" {
		t.Fatalf("early turn did not receive exactly one terminal summary: calls=%d delivery=%+v", calls, boundary.published)
	}
	stored, err := s.runs.GetRun(ctx, run.ID)
	if err != nil || stored.ChatSummaryFinishedAt == nil || stored.Status != run.Status || stored.Version != run.Version {
		t.Fatalf("summary did not settle independently of the run: %+v %v", stored, err)
	}
}

func TestCoreHTTPReplyAloneDoesNotSettleWorkflowChatSummary(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	completedAt := int64(200)
	run := models.WorkflowRun{ID: "run", SourceChatID: "chat", SourceExecutionID: "execution", Status: constants.WorkflowRunStatusSucceeded, CompletedAt: &completedAt}
	insertWorkflowChatHistory(t, s, run)
	boundary := &deliveryChatBoundary{
		actual:  models.Execution{ID: run.SourceExecutionID, Status: constants.ExecutionStatusCompleted, CompletedAt: &completedAt},
		records: []eventlog.Record{coreTerminalHTTPResponseRecord(t, run.SourceExecutionID, run.ID)},
	}
	s.chats = boundary
	s.summarizeOutput = func(context.Context, models.WorkflowExecutionDefaults, string) (string, error) {
		return "已交付终态结果", nil
	}
	if err := s.RetryChatDelivery(ctx, run.SourceChatID, run.ID, true); err != nil {
		t.Fatal(err)
	}
	stored, err := s.runs.GetRun(ctx, run.ID)
	if err != nil || stored.ChatSummaryFinishedAt == nil || boundary.published.Summary != "已交付终态结果" {
		t.Fatalf("Core transport observation suppressed terminal delivery: %+v %+v %v", stored, boundary.published, err)
	}
}

func coreTerminalHTTPResponseRecord(t *testing.T, executionID, runID string) eventlog.Record {
	t.Helper()
	data, err := json.Marshal(map[string]string{"commandId": "observation", "operation": "workflow.runs.wait", "completion": constants.AgentCLICompletionImmediate, "observedWorkflowRunId": runID})
	if err != nil {
		t.Fatal(err)
	}
	return eventlog.Record{Type: constants.ChatEventTypeAgentCommandCommitted, ExecutionID: executionID, Data: data}
}

func TestEarlyWorkflowChatFallbackKeepsResultWhenSummaryIsUnsupported(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	conversation := consumerFixtureChat(t, "chat")
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "execution", "original-input")
	response, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{"text": "NeuxBot"}})
	if err != nil {
		t.Fatal(err)
	}
	run := runTicks(t, s, unwrapWorkflow(response)["id"].(string), 8)
	if run.CompletedAt == nil {
		t.Fatal("run has no terminal receipt")
	}
	endedBeforeRun := *run.CompletedAt - 1
	actual.Status, actual.CompletedAt = constants.ExecutionStatusCompleted, &endedBeforeRun
	boundary := &deliveryChatBoundary{actual: actual}
	s.chats = boundary
	s.summarizeOutput = func(context.Context, models.WorkflowExecutionDefaults, string) (string, error) {
		return "", status_error.RuntimePolicyUnsupported
	}
	if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, true); !errors.Is(err, status_error.RuntimePolicyUnsupported) {
		t.Fatalf("unsupported summary did not report the original error: %v", err)
	}
	stored, err := s.runs.GetRun(ctx, run.ID)
	if err != nil || stored.ChatSummaryFinishedAt != nil || !boundary.published.SummaryFallback || !strings.Contains(boundary.published.Summary, "NEUXBOT NEUXBOT!") {
		t.Fatalf("fallback did not deliver confirmed results and preserve the summary error: %+v %+v %v", stored, boundary.published, err)
	}
	s.summarizeOutput = func(context.Context, models.WorkflowExecutionDefaults, string) (string, error) {
		return "补写总结", nil
	}
	if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, true); err != nil {
		t.Fatal(err)
	}
	stored, err = s.runs.GetRun(ctx, run.ID)
	if err != nil || stored.ChatSummaryFinishedAt == nil || boundary.published.SummaryFallback || boundary.published.Summary != "补写总结" {
		t.Fatalf("summary retry did not replace the fallback on the original execution: %+v %+v %v", stored, boundary.published, err)
	}
}

func TestInterruptedOriginAfterWorkflowTerminalGetsSummary(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	conversation := consumerFixtureChat(t, "chat")
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "execution", "original-input")
	response, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{"text": "NeuxBot"}})
	if err != nil {
		t.Fatal(err)
	}
	run := runTicks(t, s, unwrapWorkflow(response)["id"].(string), 8)
	boundary := &deliveryChatBoundary{
		actual:  actual,
		records: []eventlog.Record{coreTerminalHTTPResponseRecord(t, actual.ID, run.ID)},
	}
	s.chats = boundary
	calls := 0
	s.summarizeOutput = func(context.Context, models.WorkflowExecutionDefaults, string) (string, error) {
		calls++
		return "已恢复原回复", nil
	}
	if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, true); !errors.Is(err, status_error.WorkflowStateConflict) {
		t.Fatalf("active turn allowed a manual summary: %v", err)
	}
	if calls != 0 {
		t.Fatal("active turn must retain its own reply")
	}
	later := *run.CompletedAt + 10
	boundary.actual.Status, boundary.actual.CompletedAt = constants.ExecutionStatusCanceled, &later
	if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, true); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || boundary.published.SourceID != run.ID {
		t.Fatalf("late interruption lost terminal summary: calls=%d delivery=%+v", calls, boundary.published)
	}
}

func TestChatOutputBytesStayWithOriginalRun(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "chat")
	path := filepath.Join(conversation.WorkspaceDirectory, "report.csv")
	encodedPath, _ := json.Marshal(path)
	source := strings.Replace(runtimeTestMeta, "phases:", "outputSchema:{type:'string',format:'file'},phases:", 1) + "return " + string(encodedPath) + ";"
	definition := savedTestWorkflow(t, s, source)
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "execution", "original-input")
	if err := os.WriteFile(path, []byte("run 10"), 0600); err != nil {
		t.Fatal(err)
	}
	response, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	run := runTicks(t, s, unwrapWorkflow(response)["id"].(string), 8)
	if run.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("run failed: %+v", run)
	}
	if err := os.WriteFile(path, []byte("run 11"), 0600); err != nil {
		t.Fatal(err)
	}
	boundary := &deliveryChatBoundary{actual: actual}
	s.chats = boundary
	if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, false); err != nil {
		t.Fatal(err)
	}
	if len(boundary.published.Files) != 1 {
		t.Fatalf("missing output: %+v", boundary.published)
	}
	f, err := boundary.published.Files[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil || string(content) != "run 10" {
		t.Fatalf("original message received %q: %v", content, err)
	}
	canvas, name, err := s.OpenOutputFile(ctx, run.DefinitionID, run.ID, outputFileID(""))
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	canvasBytes, err := io.ReadAll(canvas)
	if err != nil || name != "report.csv" || string(canvasBytes) != string(content) {
		t.Fatalf("canvas file differs from reply: %q %q %v", name, canvasBytes, err)
	}
}

func TestFailedChatWorkflowDeliversOnlyConfirmedPartialResults(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "chat")
	path := filepath.Join(conversation.WorkspaceDirectory, "report.csv")
	if err := os.WriteFile(path, []byte("partial bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	source := runtimeTestMeta + `await functionTask({id:'report',label:'报告',phase:'work',language:'javascript',input:{},code:'function main(){return {}}',outputSchema:{type:'object',properties:{file:{type:'string',format:'file'},note:{type:'string'}}}});return await functionTask({id:'fail',label:'失败步骤',phase:'work',language:'javascript',input:{},code:'function main(){throw new Error("end")}'});`
	definition := savedTestWorkflow(t, s, source)
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "execution", "input")
	response, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	id := unwrapWorkflow(response)["id"].(string)
	runTicks(t, s, id, 2)
	state, err := s.runs.LoadRunState(ctx, id)
	if err != nil || len(state.Effects) != 1 {
		t.Fatalf("missing first effect: %v %v", state.Effects, err)
	}
	schema, _ := workflowstate.BoundaryOptions(state.Effects[0].Boundary)["outputSchema"].(map[string]any)
	s.completeDeterministic(ctx, state.Effects[0], map[string]any{"file": path, "note": path}, nil, schema)
	// 后续节点可以覆写业务目录，已经确认的部分成果仍属于本次收据。
	if err := os.WriteFile(path, []byte("different bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, id, 2)
	state, err = s.runs.LoadRunState(ctx, id)
	if err != nil || len(state.Effects) != 2 {
		t.Fatalf("missing second effect: %v %v", state.Effects, err)
	}
	for _, effect := range state.Effects {
		if effect.Boundary.NodeID == "fail" {
			s.completeDeterministic(ctx, effect, map[string]any{"file": path}, status_error.WorkflowBusinessCheckFailed, nil)
		}
	}
	run := runTicks(t, s, id, 5)
	if run.Status != constants.WorkflowRunStatusFailed {
		t.Fatalf("unexpected terminal status: %s", run.Status)
	}
	boundary := &deliveryChatBoundary{actual: actual}
	checkResults := func(want int) {
		t.Helper()
		details, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunGet, RunID: id})
		if err != nil || len(unwrapWorkflow(details)["partialResults"].([]map[string]string)) != want {
			t.Errorf("partial results have the wrong delivery state: want %d, details=%v err=%v", want, details, err)
		}
	}
	checkResults(0)
	boundary.actual.Status, boundary.actual.CompletedAt = constants.ExecutionStatusCompleted, run.CompletedAt
	blocked := &chatBlockedAttachmentDelivery{deliveryChatBoundary: boundary, started: make(chan struct{}), release: make(chan struct{})}
	s.chats = blocked
	fileDone := make(chan error, 1)
	fileContext, cancelFile := context.WithTimeout(ctx, 3*time.Second)
	defer cancelFile()
	go func() { fileDone <- s.RetryChatDelivery(fileContext, conversation.ID, id, false) }()
	select {
	case <-blocked.started:
	case <-fileContext.Done():
		t.Fatal("attachment publication did not start")
	}
	s.summarizeOutput = func(_ context.Context, _ models.WorkflowExecutionDefaults, evidence string) (string, error) {
		var facts struct {
			// Status 是失败运行已经提交的终态。
			Status constants.WorkflowRunStatus `json:"status"`
			// PartialResults 是已经完成交付、可供补写引用的部分文件。
			PartialResults []map[string]string `json:"partialResults"`
		}
		if err := json.Unmarshal([]byte(evidence), &facts); err != nil || facts.Status != constants.WorkflowRunStatusFailed || facts.PartialResults == nil || len(facts.PartialResults) != 0 {
			t.Errorf("pending file changed the terminal summary evidence: %s", evidence)
		}
		return "执行失败", nil
	}
	if err := s.RetryChatDelivery(fileContext, conversation.ID, id, true); err != nil || boundary.published.Summary != "执行失败" {
		t.Fatalf("pending file blocked the terminal summary: %+v %v", boundary.published, err)
	}
	close(blocked.release)
	if err := <-fileDone; !errors.Is(err, status_error.MessageAttachmentStorageError) {
		t.Fatalf("temporary attachment error was not preserved: %v", err)
	}
	checkResults(0)
	s.chats = boundary
	if err := s.RetryChatDelivery(ctx, conversation.ID, id, false); err != nil {
		t.Fatal(err)
	}
	if len(boundary.published.Files) != 1 || !boundary.published.Files[0].Partial || boundary.published.Files[0].Name != "report.csv" {
		t.Fatalf("wrong partial results: %+v", boundary.published)
	}
	f, err := boundary.published.Files[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	bytes, err := io.ReadAll(f)
	if err != nil || string(bytes) != "partial bytes" {
		t.Fatalf("wrong partial bytes: %q %v", bytes, err)
	}
	checkResults(1)
	for _, file := range run.OutputFiles {
		// 交付完成后，工具不能为了列出已交付文件读取原快照的大文件内容。
		if err := os.Truncate(filepath.Join(s.outputDirectory(run), file.Path), constants.WorkflowFileMaximumBytes); err != nil {
			t.Fatal(err)
		}
	}
	checkResults(1)
}

// chatBlockedAttachmentDelivery 将附件发布暂停在结算前，允许同一运行独立发布总结。
type chatBlockedAttachmentDelivery struct {
	// deliveryChatBoundary 提供原回合与总结交付。
	*deliveryChatBoundary
	// started 通知附件进入发布步骤。
	started chan struct{}
	// release 触发临时附件错误。
	release chan struct{}
}

func (b *chatBlockedAttachmentDelivery) PublishExecutionDelivery(ctx context.Context, delivery chat.ExecutionDelivery) error {
	if len(delivery.Files) == 0 {
		return b.deliveryChatBoundary.PublishExecutionDelivery(ctx, delivery)
	}
	close(b.started)
	select {
	case <-b.release:
		return status_error.MessageAttachmentStorageError
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestChatActionFreezesFilesDeclaredByBothOutputSchemas(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "chat")
	for _, name := range []string{"declared.csv", "invocation.csv"} {
		if err := os.WriteFile(filepath.Join(conversation.WorkspaceDirectory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, _ := json.Marshal(map[string]string{
		"declared":   filepath.Join(conversation.WorkspaceDirectory, "declared.csv"),
		"invocation": filepath.Join(conversation.WorkspaceDirectory, "invocation.csv"),
	})
	source := strings.Replace(runtimeTestMeta, "phases:", `actions:[{id:'report',type:'builtin',handler:'report',label:'读取成果',risk:'read',outputSchema:{type:'object',properties:{declared:{type:'string',format:'file'}}}}],phases:`, 1) +
		`const actions={report:async(ctx,input)=>input};await action('report',{id:'report-node',label:'成果',phase:'work',input:` + string(paths) + `,schema:{type:'object',properties:{invocation:{type:'string',format:'file'}}}});return false;`
	definition := savedTestWorkflow(t, s, source)
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, "execution", "input")
	actual.PermissionLevel = constants.PermissionLevelFullAccess
	response, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart,
		WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	id := unwrapWorkflow(response)["id"].(string)
	run := runTicks(t, s, id, 1)
	state, err := s.runs.LoadRunState(ctx, id)
	if err != nil || len(state.Effects) != 1 {
		t.Fatalf("missing action effect: %v %v", state.Effects, err)
	}
	effect := state.Effects[0]
	s.dispatchDeterministic(ctx, run, effect, false)
	run, err = s.runs.GetRun(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"declared", "invocation"} {
		file, ok := run.OutputFiles[effect.ID+":/"+name]
		if !ok || !file.Partial || file.Error != "" || file.Name != name+".csv" {
			t.Fatalf("missing verified partial output %s: %+v", name, run.OutputFiles)
		}
	}
}

func TestBusyChatFileCopyLetsOrdinaryWorkflowFinish(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "chat")
	path := filepath.Join(conversation.WorkspaceDirectory, "result.txt")
	encodedPath, _ := json.Marshal(path)
	if err := os.WriteFile(path, []byte("confirmed"), 0600); err != nil {
		t.Fatal(err)
	}
	fileFlow := savedTestWorkflow(t, s, strings.Replace(runtimeTestMeta, "phases:", "outputSchema:{type:'string',format:'file'},phases:", 1)+"return "+string(encodedPath)+";")
	ordinaryFlow := savedTestWorkflow(t, s, runtimeTestMeta+"return true;")
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stop := make(chan struct{})
	release := sync.OnceFunc(func() { close(stop) })
	defer release()
	entered := make(chan struct{})
	s.runner.copyOutputFiles("earlier-chat-run", func() { close(entered); <-stop })
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("file copy worker did not start")
	}
	actual, lease := bindConsumerFixture(t, s, conversation, fileFlow.ID, "execution", "input")
	reply, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: fileFlow.ID, RevisionID: fileFlow.CurrentRevisionID, RequestID: "chat-file", Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	chatID := unwrapWorkflow(reply)["id"].(string)
	ordinary, err := s.StartRun(ctx, StartRunCommand{DefinitionID: ordinaryFlow.ID, RequestID: "normal"})
	if err != nil {
		t.Fatal(err)
	}
	terminal := func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) }
	result := waitWorkflow(t, s, ordinary.ID, terminal)
	pending, err := s.runs.GetRun(ctx, chatID)
	if err != nil || result.Run.Status != constants.WorkflowRunStatusSucceeded || workflowstate.TerminalRun(pending.Status) {
		t.Fatalf("ordinary=%s pending=%s error=%v", result.Run.Status, pending.Status, err)
	}
	release()
	result = waitWorkflow(t, s, chatID, terminal)
	if result.Run.Status != constants.WorkflowRunStatusSucceeded || result.Run.OutputFiles[""].Digest == "" {
		t.Fatalf("file copy did not resume: %+v", result.Run.OutputFiles)
	}
}

// deliveryChatBoundary 保留原执行和答复，测试独立模型调用的事务外边界。
type deliveryChatBoundary struct {
	// Module 是与交付无关的方法集合。
	chat.Module
	// actual 是本次来源回合最后提交的事实。
	actual models.Execution
	// records 保存来源回合的交付和工具调用记录。
	records []eventlog.Record
	// published 是最后一次交付到原答复的正文。
	published chat.ExecutionDelivery
}

func (b *deliveryChatBoundary) ReadExecution(context.Context, string, string) (models.Execution, error) {
	return b.actual, nil
}

func (b *deliveryChatBoundary) ReadExecutionMessages(context.Context, string, string) ([]messageview.Message, error) {
	return []messageview.Message{{ID: b.actual.UserMessageID, Content: "业务任务"}}, nil
}

func (b *deliveryChatBoundary) ReadExecutionRecords(string, string) ([]eventlog.Record, error) {
	if b.published.Summary == "" {
		return b.records, nil
	}
	payload, _ := json.Marshal(eventlog.ExecutionDeliveryData{SourceID: b.published.SourceID, Summary: b.published.Summary, SummaryFallback: b.published.SummaryFallback})
	return append(append([]eventlog.Record{}, b.records...), eventlog.Record{Type: constants.ChatEventTypeExecutionDelivery, MessageID: b.actual.ID, ExecutionID: b.actual.ID, Data: payload}), nil
}

func (b *deliveryChatBoundary) PublishExecutionDelivery(_ context.Context, delivery chat.ExecutionDelivery) error {
	b.published = delivery
	return nil
}
