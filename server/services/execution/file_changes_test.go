package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/changes"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/gofrs/flock"
)

// snapshotWaitContext 仅在冻结屏障读取 Done 时通知，忽略发送链路中数据库等其他消费者。
type snapshotWaitContext struct {
	// Context 保留不会主动取消的测试执行上下文。
	context.Context
	// waiting 在 Done 首次被读取时关闭，证明执行已到达屏障等待。
	waiting chan struct{}
	// once 防止后续 Context 消费者重复关闭通知。
	once sync.Once
}

func (c *snapshotWaitContext) Done() <-chan struct{} {
	callers := make([]uintptr, 8)
	count := runtime.Callers(2, callers)
	frames := runtime.CallersFrames(callers[:count])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, ".waitForFileSnapshots") {
			c.once.Do(func() { close(c.waiting) })
			break
		}
		if !more {
			break
		}
	}
	return c.Context.Done()
}

func TestNextRuntimeWaitsForPreviousAfterSnapshotAndPreservesFrozenDiff(t *testing.T) {
	ctx := context.Background()
	root, owned := t.TempDir(), t.TempDir()
	target := filepath.Join(root, "result.txt")
	if err := os.WriteFile(target, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	capture, err := changes.Begin(ctx, owned, "first", []string{root}, changes.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	if err := os.WriteFile(target, []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	registry := newExecutionRegistry()
	registry.start(ctx)
	defer registry.close()
	defer registry.release("chat", "first")
	defer registry.release("chat", "second")
	if _, err := registry.reserve("chat", "first"); err != nil {
		t.Fatal(err)
	}
	journal := &terminalJournal{published: make(chan struct{}, 2)}
	core := &service{journal: journal, executions: registry, elicitations: newElicitationCoordinator(journal)}
	core.finishExecution(models.Chat{ID: "chat"}, models.Execution{ID: "first", ChatID: "chat"}, constants.ExecutionStatusCompleted, "", "", 0, "", nil, 0, false)
	if _, err := registry.reserve("chat", "second"); err != nil {
		t.Fatalf("real terminal did not release admission: %v", err)
	}
	waiting := &snapshotWaitContext{Context: ctx, waiting: make(chan struct{})}
	ran, done := make(chan struct{}), make(chan struct{})
	prepared := &synchronousPreparation{run: func(context.Context, recordedRun, agentapi.EventSink) agentapi.Result {
		if err := os.WriteFile(target, []byte("second\n"), 0600); err != nil {
			panic(err)
		}
		close(ran)
		return agentapi.Result{}
	}}
	go func() {
		defer close(done)
		core.runExecution(waiting, messageExecution{preparedRun: prepared, state: messageChat{chat: models.Chat{ID: "chat"}}, execution: models.Execution{ID: "second", ChatID: "chat"}, files: &preparedMessageFiles{}, precommitted: true})
	}()
	select {
	case <-waiting.waiting:
	case <-done:
		t.Fatal("next Runtime ran without waiting for the previous snapshot")
	case <-time.After(5 * time.Second):
		t.Fatal("next execution did not reach the snapshot barrier")
	}
	select {
	case <-ran:
		t.Fatal("next Runtime wrote before the previous after snapshot froze")
	default:
	}
	if err := capture.Freeze(ctx); err != nil {
		t.Fatal(err)
	}
	registry.freezeFileSnapshots("chat", "first")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot barrier did not release the next Runtime")
	}
	select {
	case <-ran:
	default:
		t.Fatal("next Runtime was not invoked after the snapshot froze")
	}
	// 此时第二轮已经写入，而第一轮尚未比较；差异只能读取冻结的 first。
	summary, err := capture.Finish(ctx)
	if err != nil || summary.TotalFiles != 1 {
		t.Fatalf("first result missing: %+v %v", summary, err)
	}
	diff, err := changes.ReadDiff(ctx, owned, summary.ID, summary.Files[0].RootID, "result.txt")
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(diff)
	if err != nil || !strings.Contains(string(content), `"text":"first"`) || strings.Contains(string(content), `"text":"second"`) {
		t.Fatalf("next Runtime contaminated the previous immutable result: %s %v", content, err)
	}
}

// fileSnapshotBlockingJournal 在首轮基线后持有真实私有库锁，确定性延迟其结束侧采集。
type fileSnapshotBlockingJournal struct {
	// Journal 沿用真实 Chat 提交、终态发布和查询行为。
	chatservice.Journal
	// recorder 沿用真实文件基线、结束侧和成果引用实现。
	recorder chatservice.FileChangesRecorder
	// store 只定位同一 Chat 的私有库锁文件。
	store *eventlog.Store
	// once 只阻塞首轮结束侧，第二轮继续使用真实采集行为。
	once sync.Once
	// lock 由测试在确认第二轮等待后释放。
	lock *flock.Flock
}

func (j *fileSnapshotBlockingJournal) BeginFileChanges(ctx context.Context, chat models.Chat, executionID, threadID string, sources []eventlog.ProjectSourceSnapshot) (*chatservice.FileChangesRecording, error) {
	recording, err := j.recorder.BeginFileChanges(ctx, chat, executionID, threadID, sources)
	if err != nil {
		return nil, err
	}
	j.once.Do(func() {
		var directory string
		directory, err = j.store.FileChangesDirectory(chat.ID)
		if err != nil {
			return
		}
		j.lock = flock.New(filepath.Join(directory, "changes", "write.lock"))
		err = j.lock.Lock()
	})
	if err != nil {
		_ = recording.Close()
		return nil, err
	}
	return recording, nil
}

func TestNextAttachmentPreparationWaitsForPreviousAfterSnapshot(t *testing.T) {
	ctx := context.Background()
	storage, project := t.TempDir(), t.TempDir()
	runner := &fileWritingRuntime{directory: project}
	manager := newMessageTestService(t, storage, runner)
	core := manager.execution.(*service)
	store, err := eventlog.NewStore(storage)
	if err != nil {
		t.Fatal(err)
	}
	journal := &fileSnapshotBlockingJournal{Journal: core.journal, recorder: core.journal.(chatservice.FileChangesRecorder), store: store}
	core.journal = journal
	defer func() {
		if journal.lock != nil {
			_ = journal.lock.Unlock()
			_ = journal.lock.Close()
		}
	}()
	first, err := manager.sendTestMessage(ctx, testMessageInput{NewChat: &testChatInput{WakerID: "waker-1", WorkingDirectory: project}, Message: userMessage{Text: "first"}, Runtime: runtimeSelection{RuntimeID: agentapi.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if journal.lock == nil || !journal.lock.Locked() {
		t.Fatal("first after snapshot was not blocked by the fixture")
	}
	source := filepath.Join(t.TempDir(), "next-user-input.txt")
	if err := os.WriteFile(source, []byte("next user attachment\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waiting := &snapshotWaitContext{Context: ctx, waiting: make(chan struct{})}
	var second AcceptedExecution
	var sendErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		second, sendErr = manager.sendTestMessage(waiting, testMessageInput{ChatID: first.Chat.ID, Message: userMessage{Text: "second", Attachments: []AttachmentInput{{SourcePath: source}}}, Runtime: runtimeSelection{RuntimeID: agentapi.RuntimeIDCodex}})
	}()
	select {
	case <-waiting.waiting:
	case <-done:
		t.Fatalf("next attachment preparation crossed the snapshot barrier: %v", sendErr)
	case <-time.After(5 * time.Second):
		t.Fatal("next attachment send did not reach the snapshot barrier")
	}
	findAttachment := func() string {
		t.Helper()
		found := ""
		if err := filepath.WalkDir(first.Chat.WorkspaceDirectory, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && entry.Name() == filepath.Base(source) {
				found = path
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return found
	}
	if path := findAttachment(); path != "" {
		t.Fatalf("next attachment was copied before the previous after snapshot froze: %s", path)
	}
	if err := journal.lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("next attachment send did not resume after snapshot freezing")
	}
	if sendErr != nil || second.Execution.ID == "" {
		t.Fatalf("next attachment send failed: %+v %v", second, sendErr)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := core.CancelAndWait(wait, first.Chat.ID); err != nil {
		t.Fatal(err)
	}
	if path := findAttachment(); path == "" {
		t.Fatal("real attachment preparation did not copy the next input")
	}
	summary, err := manager.chats.GetMessageFileChanges(ctx, first.Chat.ID, first.Execution.ID)
	if err != nil || summary.State != "ready" || summary.TotalFiles != 2 {
		t.Fatalf("next input changed the previous file result: %+v %v", summary, err)
	}
	for _, file := range summary.Files {
		if strings.Contains(file.Path, filepath.Base(source)) {
			t.Fatalf("next attachment contaminated the previous immutable result: %+v", file)
		}
	}
}

func TestCanceledNextRuntimeReleasesPreparationWithoutCrossingSnapshotBarrier(t *testing.T) {
	registry := newExecutionRegistry()
	registry.start(context.Background())
	defer registry.close()
	defer registry.release("chat", "first")
	defer registry.release("chat", "second")
	if _, err := registry.reserve("chat", "first"); err != nil {
		t.Fatal(err)
	}
	registry.complete("chat", "first", nil)
	ctx, err := registry.reserve("chat", "second")
	if err != nil {
		t.Fatal(err)
	}
	registry.cancel("chat")
	journal := &terminalJournal{published: make(chan struct{}, 1)}
	core := &service{journal: journal, executions: registry, elicitations: newElicitationCoordinator(journal)}
	closed := 0
	prepared := &synchronousPreparation{
		run: func(context.Context, recordedRun, agentapi.EventSink) agentapi.Result {
			t.Error("canceled next Runtime crossed the previous snapshot barrier")
			return agentapi.Result{}
		},
		close: func(context.Context) error { closed++; return nil },
	}
	core.runExecution(ctx, messageExecution{preparedRun: prepared, state: messageChat{chat: models.Chat{ID: "chat"}}, execution: models.Execution{ID: "second", ChatID: "chat"}, files: &preparedMessageFiles{}, precommitted: true})
	if closed != 1 || journal.terminal.Status != constants.ExecutionStatusCanceled {
		t.Fatalf("canceled wait retained preparation or lost terminal: closed=%d terminal=%s", closed, journal.terminal.Status)
	}
}

// fileWritingRuntime 通过真实 shell 修改临时目录，不提供工具 diff 或文件结果事件。
type fileWritingRuntime struct {
	// fakeRuntime 提供测试运行时目录和冻结资源装配。
	fakeRuntime
	// directory 是已授权的真实临时项目根。
	directory string
	// failure 指定在落盘之后报告的模型终态。
	failure string
	// written 在真实文件落盘后允许测试取消执行 Context。
	written chan struct{}
}

func (r *fileWritingRuntime) PrepareSession(ctx context.Context, plan agentapi.SessionPlan, directive agentapi.SessionDirective) (agentapi.PreparedChatRun, error) {
	return prepareSynchronousRuntime(ctx, plan, directive, r.Run)
}
func (r *fileWritingRuntime) Run(ctx context.Context, request recordedRun, sink agentapi.EventSink) agentapi.Result {
	_ = sink.Accept(ctx, agentapi.Event{Type: agentapi.EventTypeInvocationStarted, Data: marshalData(agentapi.InvocationStartedData{InvocationID: "file-capture"})})
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "printf 'after\\n' > tracked.txt; printf 'new\\n' > generated.txt")
	command.Dir = r.directory
	if err := command.Run(); err != nil {
		return agentapi.Result{Failure: &agentapi.Failure{Code: agentapi.FailureCodeProcessExited, Message: err.Error()}}
	}
	if r.written != nil {
		close(r.written)
		<-ctx.Done()
	}
	result := agentapi.Result{Facts: agentapi.InvocationFacts{InvocationID: "file-capture", RuntimeID: request.Target.RuntimeID, BaseRuntimeID: agentapi.RuntimeIDCodex}}
	if r.failure != "" {
		code := agentapi.FailureCodeProcessExited
		if r.failure == "cancel" {
			code = agentapi.FailureCodeContextCanceled
		}
		result.Failure = &agentapi.Failure{Code: code, Message: "test stop after writes"}
	}
	return result
}

func TestExecutionFileCaptureUsesActualShellWritesAndKeepsRealTerminal(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			project := t.TempDir()
			if err := os.WriteFile(filepath.Join(project, "tracked.txt"), []byte("before\n"), 0600); err != nil {
				t.Fatal(err)
			}
			runner := &fileWritingRuntime{directory: project}
			if outcome != "success" {
				runner.failure = outcome
				if outcome == "cancel" {
					runner.written = make(chan struct{})
				}
			}
			manager := newMessageTestService(t, t.TempDir(), runner)
			accepted, err := manager.sendTestMessage(context.Background(), testMessageInput{NewChat: &testChatInput{WakerID: "waker-1", WorkingDirectory: project}, Message: userMessage{Text: "write without CLI diff"}, Runtime: runtimeSelection{RuntimeID: agentapi.RuntimeIDCodex, PermissionLevel: agentapi.PermissionLevelDefault}})
			if err != nil {
				t.Fatal(err)
			}
			if runner.written != nil {
				select {
				case <-runner.written:
				case <-time.After(3 * time.Second):
					t.Fatal("shell did not finish writes before cancellation")
				}
				if err := manager.execution.Cancel(accepted.Chat.ID); err != nil {
					t.Fatal(err)
				}
			}
			waitForChatTerminal(t, manager, accepted.Chat.ID)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := manager.execution.CancelAndWait(ctx, accepted.Chat.ID); err != nil {
				t.Fatal(err)
			}
			module := manager.chats.(chatservice.Module)
			summary, err := module.GetMessageFileChanges(ctx, accepted.Chat.ID, accepted.Execution.ID)
			if err != nil {
				t.Fatal(err)
			}
			if summary.State != "ready" || summary.TotalFiles != 2 {
				t.Fatalf("shell net changes=%+v", summary)
			}
			target := ""
			for _, file := range summary.Files {
				if filepath.Base(file.Path) == "tracked.txt" {
					target = file.AbsolutePath
				}
			}
			diff, err := module.GetMessageFileChange(ctx, accepted.Chat.ID, accepted.Execution.ID, target)
			if err != nil || diff.Comparison == nil {
				t.Fatalf("immutable diff: %+v %v", diff, err)
			}
			records, err := module.ReadExecutionRecords(accepted.Chat.ID, accepted.Execution.ID)
			if err != nil {
				t.Fatal(err)
			}
			terminal, ready := int64(0), int64(0)
			for _, record := range records {
				switch record.Type {
				case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
					terminal = record.Sequence
				case constants.ChatEventTypeFileChanges:
					var reference eventlog.FileChangesData
					if json.Unmarshal(record.Data, &reference) == nil && reference.State == "ready" {
						ready = record.Sequence
					}
				}
			}
			if terminal == 0 || ready <= terminal {
				t.Fatalf("file result delayed/reopened model terminal: terminal=%d ready=%d", terminal, ready)
			}
			chat, err := module.GetChatSummary(ctx, accepted.Chat.ID)
			if err != nil || chat.Chat.ActiveExecutionID != "" {
				t.Fatalf("file result reopened execution: %+v %v", chat, err)
			}
		})
	}
}

// captureStoredObjects 对照实际配额枚举，同时观察去重身份、条目数和逻辑字节。
type captureStoredObjects struct {
	// objects 是原私有 Git 中的对象身份与大小。
	objects map[string]int64
	// entries 包含枚举返回的每一个对象条目，重复对象也不能逃过配额核验。
	entries int
	// bytes 与采集器相同，累计对象声明的逻辑字节。
	bytes int64
}

// captureObjectFootprint 只读私有对象身份及逻辑字节，证明未接纳尝试不占采集配额。
func captureObjectFootprint(t *testing.T, owned string) captureStoredObjects {
	t.Helper()
	repository, err := git.PlainOpen(filepath.Join(owned, "changes", "repository.git"))
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := repository.Storer.IterEncodedObjects(plumbing.AnyObject)
	if err != nil {
		t.Fatal(err)
	}
	defer iterator.Close()
	result := captureStoredObjects{objects: map[string]int64{}}
	if err := iterator.ForEach(func(object plumbing.EncodedObject) error {
		result.objects[object.Hash().String()] = object.Size()
		result.entries++
		result.bytes += object.Size()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestExistingChatUnacceptedCaptureDoesNotAccumulateRecordsOrQuota(t *testing.T) {
	ctx := context.Background()
	storage, project := t.TempDir(), t.TempDir()
	tracked := filepath.Join(project, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("accepted baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRuntime{}
	manager := newMessageTestService(t, storage, runner)
	accepted, err := manager.sendTestMessage(ctx, testMessageInput{NewChat: &testChatInput{WakerID: "waker-1", WorkingDirectory: project}, Message: userMessage{Text: "accepted"}, Runtime: runtimeSelection{RuntimeID: agentapi.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := manager.execution.CancelAndWait(wait, accepted.Chat.ID); err != nil {
		t.Fatal(err)
	}
	store, err := eventlog.NewStore(storage)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := store.FileChangesDirectory(accepted.Chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.ReadAll(accepted.Chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	recordIDs := func() map[string]bool {
		t.Helper()
		ids := map[string]bool{}
		if err := changes.WalkSummaries(owned, func(summary changes.Summary) error { ids[summary.ID] = true; return nil }); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	beforeIDs, beforeObjects := recordIDs(), captureObjectFootprint(t, owned)
	if len(beforeIDs) != 1 || len(beforeObjects.objects) == 0 {
		t.Fatalf("accepted snapshot missing: records=%d objects=%d", len(beforeIDs), len(beforeObjects.objects))
	}
	runner.failureStage = "prepare"
	for attempt := range 3 {
		body := strings.Repeat(fmt.Sprintf("unaccepted-%d\n", attempt), 4096)
		if err := os.WriteFile(tracked, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.sendTestMessage(ctx, testMessageInput{ChatID: accepted.Chat.ID, Message: userMessage{Text: fmt.Sprintf("rejected %d", attempt)}, Runtime: runtimeSelection{RuntimeID: agentapi.RuntimeIDCodex}}); !errors.Is(err, status_error.RuntimeUnavailable) {
			t.Fatalf("unaccepted result=%v", err)
		}
		if err := manager.execution.CancelAndWait(wait, accepted.Chat.ID); err != nil {
			t.Fatal(err)
		}
		if ids := recordIDs(); !reflect.DeepEqual(ids, beforeIDs) {
			t.Fatalf("attempt %d retained unreferenced records: %+v", attempt, ids)
		}
		if objects := captureObjectFootprint(t, owned); !reflect.DeepEqual(objects, beforeObjects) {
			t.Fatalf("attempt %d changed private object quota: before=%v after=%v", attempt, beforeObjects, objects)
		}
		after, err := store.ReadAll(accepted.Chat.ID)
		if err != nil || len(after) != len(records) {
			t.Fatalf("unaccepted attempt changed durable messages: records=%d err=%v", len(after), err)
		}
	}
}
