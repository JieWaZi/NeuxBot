package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestSlowChatSummaryDoesNotBlockOtherWorkflowAttachments(t *testing.T) {
	s, _ := workflowTestService(t)
	first := consumerFixtureChat(t, "summary-chat")
	firstRun, actual := startChatDeliveryFixture(t, s, first, "summary-execution", savedTestWorkflow(t, s, runtimeTestMeta+"return true;"))
	firstRun = runTicks(t, s, firstRun.ID, 5)
	if firstRun.CompletedAt == nil {
		t.Fatal("summary run did not finish")
	}
	ended := *firstRun.CompletedAt - 1
	actual.Status, actual.CompletedAt = constants.ExecutionStatusFailed, &ended
	boundary := &chatDeliverySignals{deliveryChatBoundary: &deliveryChatBoundary{actual: actual}, attachments: make(chan chat.ExecutionDelivery, 1)}
	s.chats = boundary
	summaryBegan := make(chan struct{})
	s.summarizeOutput = func(ctx context.Context, _ models.WorkflowExecutionDefaults, _ string) (string, error) {
		close(summaryBegan)
		<-ctx.Done()
		return "", ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	summaryDone := make(chan struct{})
	go func() {
		defer close(summaryDone)
		_ = s.RetryChatDelivery(ctx, first.ID, firstRun.ID, true)
	}()
	lifecycle := &runnerLifecycle{ctx: ctx, deliveryWake: make(chan struct{}, 1)}
	lifecycle.workers.Add(1)
	go s.runChatDeliveryLoop(lifecycle)
	defer func() {
		cancel()
		lifecycle.workers.Wait()
		<-summaryDone
	}()
	select {
	case <-summaryBegan:
	case <-time.After(3 * time.Second):
		t.Fatal("summary model did not start")
	}
	second := consumerFixtureChat(t, "attachment-chat")
	path := filepath.Join(second.WorkspaceDirectory, "report.txt")
	if err := os.WriteFile(path, []byte("confirmed report"), 0600); err != nil {
		t.Fatal(err)
	}
	encodedPath, _ := json.Marshal(path)
	fileSource := strings.Replace(runtimeTestMeta, "phases:", "outputSchema:{type:'string',format:'file'},phases:", 1) + "return " + string(encodedPath) + ";"
	secondRun, secondActual := startChatDeliveryFixture(t, s, second, "attachment-execution", savedTestWorkflow(t, s, fileSource))
	secondRun = runTicks(t, s, secondRun.ID, 5)
	lifecycle.deliveryWake <- struct{}{}
	select {
	case delivery := <-boundary.attachments:
		if delivery.SourceID != secondRun.ID || delivery.ChatID != second.ID || delivery.ExecutionID != secondActual.ID || len(delivery.Files) != 1 {
			t.Fatalf("unexpected attachment delivery: %+v", delivery)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a blocked summary model held up another chat's file")
	}
}

func TestChatSummaryDeadlinePublishesConfirmedFacts(t *testing.T) {
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "summary-chat")
	run, actual := startChatDeliveryFixture(t, s, conversation, "summary-execution", savedTestWorkflow(t, s, runtimeTestMeta+"return 'confirmed';"))
	run = runTicks(t, s, run.ID, 5)
	if run.CompletedAt == nil {
		t.Fatal("run did not finish")
	}
	actual.Status, actual.CompletedAt = constants.ExecutionStatusFailed, run.CompletedAt
	boundary := &deliveryChatBoundary{actual: actual}
	s.chats = boundary
	s.summarizeOutput = func(ctx context.Context, _ models.WorkflowExecutionDefaults, _ string) (string, error) {
		if _, present := ctx.Deadline(); !present {
			t.Error("detached summary has no bounded model deadline")
		}
		return "", context.DeadlineExceeded
	}
	if err := s.RetryChatDelivery(t.Context(), conversation.ID, run.ID, true); !errors.Is(err, status_error.OperationTimeout) {
		t.Fatalf("summary deadline did not produce a retryable delivery status: %v", err)
	}
	stored, err := s.runs.GetRun(t.Context(), run.ID)
	if err != nil || stored.ChatSummaryFinishedAt != nil || !boundary.published.SummaryFallback || !strings.Contains(boundary.published.Summary, "confirmed") {
		t.Fatalf("summary timeout lost confirmed facts: stored=%+v delivery=%+v err=%v", stored, boundary.published, err)
	}
}

func TestSuccessfulChatDeliveryReleasesOnlyUnusedPartialSnapshots(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "attachment-chat")
	path := filepath.Join(conversation.WorkspaceDirectory, "report.txt")
	if err := os.WriteFile(path, []byte("original user file"), 0600); err != nil {
		t.Fatal(err)
	}
	encodedPath, _ := json.Marshal(path)
	source := strings.Replace(runtimeTestMeta, "phases:", "outputSchema:{type:'string',format:'file'},phases:", 1) + `await functionTask({id:'report',label:'报告',phase:'work',language:'javascript',input:{},code:'function main(){return ""}',outputSchema:{type:'string',format:'file'}});return ` + string(encodedPath) + ";"
	definition := savedTestWorkflow(t, s, source)
	run, actual := startChatDeliveryFixture(t, s, conversation, "attachment-execution", definition)
	runTicks(t, s, run.ID, 2)
	state, err := s.runs.LoadRunState(ctx, run.ID)
	if err != nil || len(state.Effects) != 1 {
		t.Fatalf("missing report effect: %+v %v", state.Effects, err)
	}
	effect := state.Effects[0]
	schema, _ := workflowstate.BoundaryOptions(effect.Boundary)["outputSchema"].(map[string]any)
	s.completeDeterministic(ctx, effect, path, nil, schema)
	run = runTicks(t, s, run.ID, 5)
	if run.Status != constants.WorkflowRunStatusSucceeded || len(run.ChatFiles) != 2 {
		t.Fatalf("missing final or partial file receipt: %+v", run)
	}
	partial := filepath.Join(s.chatOutputDirectory(run), run.ChatFiles[effect.ID+":"].Path)
	if _, err := os.Stat(partial); err != nil {
		t.Fatal(err)
	}
	boundary := &deliveryChatBoundary{actual: actual}
	s.chats = chatAttachmentFailure{deliveryChatBoundary: boundary}
	if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, false); !errors.Is(err, status_error.MessageAttachmentStorageError) {
		t.Fatalf("missing attachment failure: %v", err)
	}
	if _, err := os.Stat(partial); err != nil {
		t.Fatalf("attachment failure deleted a pending snapshot: %v", err)
	}
	s.chats = boundary
	for range 2 {
		if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("redundant run snapshot was retained: %v", err)
	}
	if err := s.removeUnusedChatPartialFiles(ctx, run); err != nil {
		t.Fatalf("already removed snapshot could not be cleaned again: %v", err)
	}
	if contents, err := os.ReadFile(path); err != nil || string(contents) != "original user file" {
		t.Fatalf("cleanup touched the source file: %q %v", contents, err)
	}
	file, _, err := s.OpenOutputFile(ctx, run.DefinitionID, run.ID, outputFileID(""))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if contents, err := io.ReadAll(file); err != nil || string(contents) != "original user file" {
		t.Fatalf("cleanup affected the canvas result: %q %v", contents, err)
	}
}

func TestChatOutputMetadataUsesSnapshotWithoutReadingItsContents(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "metadata-chat")
	path := filepath.Join(conversation.WorkspaceDirectory, "report.txt")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	encodedPath, _ := json.Marshal(path)
	source := strings.Replace(runtimeTestMeta, "phases:", "outputSchema:{type:'string',format:'file'},phases:", 1) + "return " + string(encodedPath) + ";"
	definition := savedTestWorkflow(t, s, source)
	run, _ := startChatDeliveryFixture(t, s, conversation, "metadata-execution", definition)
	run = runTicks(t, s, run.ID, 5)
	if run.Status != constants.WorkflowRunStatusSucceeded || len(run.ChatFiles) != 1 {
		t.Fatalf("missing final file receipt: %+v", run)
	}
	snapshot := filepath.Join(s.chatOutputDirectory(run), run.ChatFiles[""].Path)
	if err := os.WriteFile(snapshot, []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := s.OutputFiles(ctx, run)
	if err != nil || len(files) != 1 || !files[0].Available || files[0].Size != 8 || files[0].Name != "report.txt" {
		t.Fatalf("metadata query did not return the present snapshot's file properties: %+v %v", files, err)
	}
	download, _, err := s.OpenOutputFile(ctx, run.DefinitionID, run.ID, files[0].ID)
	if download != nil {
		download.Close()
	}
	if !errors.Is(err, status_error.WorkflowFileInvalid) {
		t.Fatalf("download accepted bytes that differ from the frozen receipt: %v", err)
	}
	if err := os.Remove(snapshot); err != nil {
		t.Fatal(err)
	}
	files, err = s.OutputFiles(ctx, run)
	if err != nil || len(files) != 1 || files[0].Available {
		t.Fatalf("metadata query reported a missing snapshot as available: %+v %v", files, err)
	}
}

func TestChatDeliverySkipsUnavailableOriginalsAndRetriesIndependentCopies(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "attachment-chat")
	references := map[string]string{}
	for _, name := range []string{"capture-failed", "corrupted", "good", "retry"} {
		path := filepath.Join(conversation.WorkspaceDirectory, name+".txt")
		references[name] = path
		if name != "capture-failed" {
			if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	schema := `{type:'object',properties:{'capture-failed':{type:'string',format:'file'},corrupted:{type:'string',format:'file'},good:{type:'string',format:'file'},retry:{type:'string',format:'file'}}}`
	paths, _ := json.Marshal(references)
	source := strings.Replace(runtimeTestMeta, "phases:", "outputSchema:"+schema+",phases:", 1) + "return " + string(paths) + ";"
	run, _ := startChatDeliveryFixture(t, s, conversation, "attachment-execution", savedTestWorkflow(t, s, source))
	run = runTicks(t, s, run.ID, 5)
	if run.Status != constants.WorkflowRunStatusSucceeded || run.ChatFiles["/capture-failed"].Error == "" || run.ChatFiles["/corrupted"].Path == "" {
		t.Fatalf("unexpected original file receipts: status=%s files=%+v", run.Status, run.ChatFiles)
	}
	if err := os.WriteFile(filepath.Join(s.chatOutputDirectory(run), run.ChatFiles["/corrupted"].Path), []byte("changed bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	boundary := &recordingAttachmentCopies{seen: map[string]int{}, copied: map[string]string{}, failTarget: true}
	s.chats = boundary
	if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, false); !errors.Is(err, status_error.MessageAttachmentStorageError) {
		t.Fatalf("first delivery result = %v, want target copy failure", err)
	}
	stored, err := s.runs.GetRun(ctx, run.ID)
	if err != nil || stored.ChatFiles["/corrupted"].Error != status_error.WorkflowFileInvalid.Key() || stored.ChatDeliveryError != status_error.MessageAttachmentStorageError.Key() || boundary.copied["good.txt"] != "good" {
		t.Fatalf("mixed failures lost verified results: files=%+v copyError=%s copies=%v error=%v", stored.ChatFiles, stored.ChatDeliveryError, boundary.copied, err)
	}
	boundary.failTarget = false
	if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, false); err != nil {
		t.Fatal(err)
	}
	stored, err = s.runs.GetRun(ctx, run.ID)
	if err != nil || stored.ChatFiles["/corrupted"].Error != status_error.WorkflowFileInvalid.Key() || stored.ChatDeliveryError != "" || boundary.copied["retry.txt"] != "retry" ||
		boundary.seen["capture-failed.txt"] != 0 || boundary.seen["corrupted.txt"] != 1 || boundary.seen["retry.txt"] != 2 {
		t.Fatalf("retry re-opened an unavailable original or skipped the recoverable copy: files=%+v copyError=%s opens=%v copies=%v error=%v", stored.ChatFiles, stored.ChatDeliveryError, boundary.seen, boundary.copied, err)
	}
}

func TestChatDeliveryRetriesTemporarySnapshotStorageFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires POSIX read permissions")
	}
	ctx := t.Context()
	s, _ := workflowTestService(t)
	conversation := consumerFixtureChat(t, "attachment-chat")
	path := filepath.Join(conversation.WorkspaceDirectory, "report.txt")
	if err := os.WriteFile(path, []byte("confirmed report"), 0o600); err != nil {
		t.Fatal(err)
	}
	encodedPath, _ := json.Marshal(path)
	source := strings.Replace(runtimeTestMeta, "phases:", "outputSchema:{type:'string',format:'file'},phases:", 1) + "return " + string(encodedPath) + ";"
	run, _ := startChatDeliveryFixture(t, s, conversation, "attachment-execution", savedTestWorkflow(t, s, source))
	run = runTicks(t, s, run.ID, 5)
	file := run.ChatFiles[""]
	if file.Path == "" || file.Error != "" {
		t.Fatalf("not a confirmed frozen file: %+v", file)
	}
	snapshot := filepath.Join(s.chatOutputDirectory(run), file.Path)
	if err := os.Chmod(snapshot, 0); err != nil {
		t.Fatal(err)
	}
	boundary := &recordingAttachmentCopies{seen: map[string]int{}, copied: map[string]string{}}
	s.chats = boundary
	if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, false); !errors.Is(err, status_error.WorkflowFileStorageError) {
		t.Fatalf("unreadable snapshot did not return a retryable storage error: %v", err)
	}
	stored, err := s.runs.GetRun(ctx, run.ID)
	if err != nil || stored.ChatFiles[""].Error != "" || stored.ChatDeliveryError != status_error.WorkflowFileStorageError.Key() {
		t.Fatalf("temporary failure was recorded as an unavailable original: %+v, %v", stored, err)
	}
	if err := os.Chmod(snapshot, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.RetryChatDelivery(ctx, conversation.ID, run.ID, false); err != nil || boundary.copied["report.txt"] != "confirmed report" {
		t.Fatalf("original bytes did not arrive after retry: %v, %v", boundary.copied, err)
	}
}

type recordingAttachmentCopies struct {
	// Module 保留与此文件交付无关的 Chat 方法。
	chat.Module
	// seen 计数交付请求中取得原件的尝试。
	seen map[string]int
	// copied 保存已成功交付的会话副本。
	copied map[string]string
	// failTarget 控制目标副本写入故障。
	failTarget bool
}

func (b *recordingAttachmentCopies) PublishExecutionDelivery(_ context.Context, delivery chat.ExecutionDelivery) error {
	var copyError error
	for _, file := range delivery.Files {
		if _, committed := b.copied[file.Name]; committed {
			continue
		}
		b.seen[file.Name]++
		if file.Name == "retry.txt" && b.failTarget {
			if copyError == nil {
				copyError = status_error.MessageAttachmentStorageError
			}
			continue
		}
		f, err := file.Open()
		if err != nil {
			if file.HandleSourceError != nil && file.HandleSourceError(err) {
				continue
			}
			if copyError == nil {
				copyError = err
			}
			continue
		}
		content, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return err
		}
		b.copied[file.Name] = string(content)
	}
	return copyError
}

type chatAttachmentFailure struct {
	// deliveryChatBoundary 提供可读取的原回合，其附件暂时写入失败。
	*deliveryChatBoundary
}

func (chatAttachmentFailure) PublishExecutionDelivery(context.Context, chat.ExecutionDelivery) error {
	return status_error.MessageAttachmentStorageError
}

func startChatDeliveryFixture(t *testing.T, s *Service, conversation models.Chat, executionID string, definition models.WorkflowDefinition) (models.WorkflowRun, models.Execution) {
	t.Helper()
	ctx := t.Context()
	actual, lease := bindConsumerFixture(t, s, conversation, definition.ID, executionID, "input")
	response, err := s.handleConsumerTool(ctx, lease, actual, conversation, workflowtool.Input{Action: constants.WorkflowActionRunStart, WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.runs.GetRun(ctx, unwrapWorkflow(response)["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return run, actual
}

type chatDeliverySignals struct {
	// deliveryChatBoundary 提供固定的已中断回合，异步测试使用独立的发布通知。
	*deliveryChatBoundary
	// attachments 保存另外一个回合发布的附件。
	attachments chan chat.ExecutionDelivery
}

func (b *chatDeliverySignals) PublishExecutionDelivery(ctx context.Context, delivery chat.ExecutionDelivery) error {
	if len(delivery.Files) == 0 {
		return nil
	}
	select {
	case b.attachments <- delivery:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
