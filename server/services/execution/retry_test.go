package execution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// retryTestRuntime 记录重放请求，并将可重试失败作为确定性外部边界。
type retryTestRuntime struct {
	// mutex 保护并发运行和测试读取的请求集合。
	mutex sync.Mutex
	// requests 保存真正交给 Runtime 的输入。
	requests []runtimes.RunRequest
	// result 是每个回合返回的失败事实。
	result runtimes.Result
	// output 是可选的已观察正文，用于证明不能仅相信失败标签。
	output string
	// tool 表示 Runtime 已交付工具调用，但测试故意省略结果中的副作用标记。
	tool bool
}

func (r *retryTestRuntime) Runtimes() []runtimes.RuntimeSummary { return (&fakeRuntime{}).Runtimes() }

// ForkSession 保持测试替身与当前 Runtime 契约一致；重试用例不调用分叉。
func (r *retryTestRuntime) ForkSession(ctx context.Context, request runtimes.ForkSessionRequest) (runtimes.ForkSessionResult, error) {
	return (&fakeRuntime{}).ForkSession(ctx, request)
}

func (r *retryTestRuntime) RunWithLease(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink, release func(context.Context) error) runtimes.Result {
	if release != nil {
		defer func() { _ = release(context.WithoutCancel(ctx)) }()
	}
	r.mutex.Lock()
	r.requests = append(r.requests, request)
	result, output, tool := r.result, r.output, r.tool
	r.mutex.Unlock()
	id := util.New()
	if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeInvocationStarted, Data: marshalData(runtimes.InvocationStartedData{InvocationID: id})}); err != nil {
		return runtimes.Result{Failure: &runtimes.Failure{Code: runtimes.FailureCodeEventDelivery}}
	}
	if output != "" {
		if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: marshalData(map[string]string{"text": output})}); err != nil {
			return runtimes.Result{Failure: &runtimes.Failure{Code: runtimes.FailureCodeEventDelivery}}
		}
	}
	if tool {
		if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeToolUse, Data: marshalData(map[string]any{"id": "tool-1", "name": "Bash"})}); err != nil {
			return runtimes.Result{Failure: &runtimes.Failure{Code: runtimes.FailureCodeEventDelivery}}
		}
	}
	result.Facts = runtimes.InvocationFacts{InvocationID: id, RuntimeID: request.Target.RuntimeID, BaseRuntimeID: runtimes.RuntimeIDCodex}
	result.Session.Mode = request.Session.Mode
	return result
}

// RunSummaryWithLease 通过同一测试宿主执行隐藏会话交接并结束资源使用。
func (r *retryTestRuntime) RunSummaryWithLease(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink, release func(context.Context) error) runtimes.Result {
	return r.RunWithLease(ctx, request, sink, release)
}

// CloseWorkspace 的同步测试消费者已经退出，无需另行持有工作区资源。
func (r *retryTestRuntime) CloseWorkspace(context.Context, string) error { return nil }

// retryableTestResult 构造无输出、无工具且保留可靠会话的限流终态。
func retryableTestResult() runtimes.Result {
	return runtimes.Result{ExitCode: -1, Session: runtimes.SessionResult{OutputID: "native-session", State: runtimes.SessionStateCreated}, Failure: &runtimes.Failure{Code: runtimes.FailureCodeRateLimited, Transience: runtimes.FailureTransienceTransient}}
}

// retryTestIntent 构造直接来自用户且可进入重试流程的原始输入。
func retryTestIntent(t *testing.T) WorkIntent {
	t.Helper()
	return WorkIntent{ClientRequestID: util.New(), Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "user", Content: "原始问题"}, Options: WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, Model: "gpt-5", Reasoning: "high", PermissionLevel: runtimes.PermissionLevelFullAccess, WorkspaceReadOnly: true, SessionPolicy: constants.RuntimeSessionPolicyContinue}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
}

// retryTestStart 提交并等待原始失败完全释放执行占位。
func retryTestStart(t *testing.T, manager messageTestService, intent WorkIntent) AcceptedExecution {
	t.Helper()
	accepted, err := manager.execution.Start(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	core := manager.execution.(*service)
	deadline := time.Now().Add(2 * time.Second)
	for {
		core.executions.mutex.Lock()
		_, active := core.executions.active[accepted.Chat.ID]
		core.executions.mutex.Unlock()
		if !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("执行终态没有释放占位")
		}
		time.Sleep(time.Millisecond)
	}
	return accepted
}

func TestRuntimeFailureRetryableRequiresSafeReplay(t *testing.T) {
	cases := []struct {
		// name 描述导致不安全重放的条件。
		name string
		// change 注入单一风险信号。
		change func(*runtimes.Result)
	}{
		{"visible output", func(r *runtimes.Result) { r.SideEffects.VisibleOutput = true }},
		{"reasoning", func(r *runtimes.Result) { r.SideEffects.ReasoningOutput = true }},
		{"tool", func(r *runtimes.Result) { r.Failure.SideEffects.ToolUse = true }},
		{"write", func(r *runtimes.Result) { r.SideEffects.FileWrite = true }},
		{"artifact", func(r *runtimes.Result) { r.SideEffects.Artifact = true }},
		{"unknown session", func(r *runtimes.Result) { r.Session.State = runtimes.SessionStateUnknown }},
		{"missing session", func(r *runtimes.Result) { r.Session.OutputID = "" }},
		{"quota", func(r *runtimes.Result) { r.Failure.Code = runtimes.FailureCodeQuotaExhausted }},
		{"auth", func(r *runtimes.Result) { r.Failure.Code = runtimes.FailureCodeAuthRequired }},
		{"undelivered events", func(r *runtimes.Result) { r.UndeliveredEvents = 1 }},
		{"undelivered bytes", func(r *runtimes.Result) { r.UndeliveredBytes = 1 }},
	}
	if !runtimeFailureRetryable(retryableTestResult()) {
		t.Fatal("可靠限流未允许重试")
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := retryableTestResult()
			test.change(&result)
			if runtimeFailureRetryable(result) {
				t.Fatal("允许了不安全重放")
			}
		})
	}
}

// retryConnectorController 只提供重试输入恢复所需的已启用连接器集合。
type retryConnectorController struct {
	// MCPConnectorController 保留测试不会调用的仓储操作。
	controllers.MCPConnectorController
	// enabled 保存当前伙伴启用的连接器。
	enabled []models.MCPConnector
}

func (c *retryConnectorController) ListEnabled(context.Context, string) ([]models.MCPConnector, error) {
	return c.enabled, nil
}

func TestRetryExecutionRestoresInputAndHasStableChild(t *testing.T) {
	runner := &retryTestRuntime{result: retryableTestResult()}
	skills := &trackingSkillController{enabled: []models.SkillWithState{{SkillPackage: models.SkillPackage{ID: "review", Name: "review"}, CurrentVersion: models.SkillVersion{ID: "version-1", Name: "review", Version: "1.0.0", ContentPath: "skills/review"}, Enabled: true}}}
	manager := newMessageTestService(t, t.TempDir(), runner, messageTestOptions{skills: skills, skillDataDirectory: t.TempDir()})
	core := manager.execution.(*service)
	core.connectorController = &retryConnectorController{enabled: []models.MCPConnector{{ID: "connector", Name: "lookup", RuntimeKey: "lookup", Transport: constants.MCPTransportStreamableHTTP, URL: "http://localhost/mcp"}}}
	file := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(file, []byte("frozen attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	intent := retryTestIntent(t)
	intent.Input.Attachments = []AttachmentInput{{SourcePath: file}}
	intent.Input.SkillIDs = []string{"review"}
	intent.Input.ConnectorIDs = []string{"connector"}
	intent.Input.CodeReferences = []CodeReferenceInput{{Path: "/tmp/source.go", StartLine: 2, EndLine: 3, Content: "snapshot"}}
	original := retryTestStart(t, manager, intent)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	retried, err := core.retryExecution(context.Background(), original.Chat.ID, original.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, original.Chat.ID)
	repeated, err := core.retryExecution(context.Background(), original.Chat.ID, original.Execution.ID)
	if err != nil || !repeated.Replayed || repeated.Execution.ID != retried.Execution.ID || repeated.Execution.ParentExecutionID != original.Execution.ID {
		t.Fatalf("重试不幂等：%+v %v", repeated, err)
	}
	runner.mutex.Lock()
	requests := append([]runtimes.RunRequest(nil), runner.requests...)
	runner.mutex.Unlock()
	if len(requests) != 2 {
		t.Fatalf("重复派发：%d", len(requests))
	}
	first, second := requests[0], requests[1]
	if second.Message.Text != first.Message.Text || !reflect.DeepEqual(second.Message.CodeReferences, first.Message.CodeReferences) || !reflect.DeepEqual(second.Skills, first.Skills) || !reflect.DeepEqual(second.Connectors, first.Connectors) || !reflect.DeepEqual(second.Policy, first.Policy) {
		t.Fatalf("重试改变了输入选项：\nfirst=%+v\nsecond=%+v", first, second)
	}
	if second.Session.Mode != runtimes.SessionModeResume || second.Session.ID != "native-session" {
		t.Fatalf("重试重新播种了会话：%+v", second.Session)
	}
	if len(second.Message.Attachments) != 1 {
		t.Fatalf("附件丢失：%+v", second.Message.Attachments)
	}
	data, err := os.ReadFile(filepath.Join(second.Workspace.Path, second.Message.Attachments[0].Path))
	if err != nil || string(data) != "frozen attachment" {
		t.Fatalf("未恢复受管附件：%q %v", data, err)
	}
}

func TestRetryExecutionRejectsOutputAndClearedSession(t *testing.T) {
	for _, condition := range []string{"output", "tool", "cleared session", "newer execution"} {
		t.Run(condition, func(t *testing.T) {
			runner := &retryTestRuntime{result: retryableTestResult()}
			if condition == "output" {
				runner.output = "already answered"
			}
			if condition == "tool" {
				runner.tool = true
			}
			manager := newMessageTestService(t, t.TempDir(), runner)
			original := retryTestStart(t, manager, retryTestIntent(t))
			core := manager.execution.(*service)
			if condition == "cleared session" {
				if _, err := core.journal.AppendExecutionFacts(context.Background(), original.Chat.ID, []eventlog.AppendInput{{Type: constants.ChatEventTypeNativeSessionCleared, ExecutionID: original.Execution.ID, Data: marshalData(eventlog.RuntimeSessionData{RuntimeID: runtimes.RuntimeIDCodex})}}); err != nil {
					t.Fatal(err)
				}
			}
			if condition == "newer execution" {
				intent := retryTestIntent(t)
				intent.Chat = ChatTarget{ChatID: original.Chat.ID}
				intent.ClientRequestID = ""
				intent.Input.Content = "new input"
				retryTestStart(t, manager, intent)
			}
			if _, err := core.retryExecution(context.Background(), original.Chat.ID, original.Execution.ID); !errors.Is(err, status_error.ExecutionRetryUnavailable) {
				t.Fatalf("允许重放 %s：%v", condition, err)
			}
		})
	}
}

// retryRaceKey 标识需要在获取旧快照后暂停的测试请求。
type retryRaceKey struct{}

// retryRaceJournal 在重试调用的第二次读取后暂停，构造普通发送先完成的真实竞争窗口。
type retryRaceJournal struct {
	// Journal 复用真实权威存储和提交行为。
	chatservice.Journal
	// loads 只统计被标记的重试请求读取次数。
	loads atomic.Int64
	// entered 表示旧快照已经取得。
	entered chan struct{}
	// proceed 由测试在普通消息完成后释放旧快照。
	proceed chan struct{}
}

func (j *retryRaceJournal) LoadExecutionContext(ctx context.Context, chatID string) (chatservice.ExecutionContext, error) {
	snapshot, err := j.Journal.LoadExecutionContext(ctx, chatID)
	if ctx.Value(retryRaceKey{}) == true && j.loads.Add(1) == 2 {
		close(j.entered)
		select {
		case <-j.proceed:
		case <-ctx.Done():
			return chatservice.ExecutionContext{}, ctx.Err()
		}
	}
	return snapshot, err
}

func TestRetryExecutionRevalidatesAfterNormalSendWins(t *testing.T) {
	runner := &retryTestRuntime{result: retryableTestResult()}
	manager := newMessageTestService(t, t.TempDir(), runner)
	core := manager.execution.(*service)
	gate := &retryRaceJournal{Journal: core.journal, entered: make(chan struct{}), proceed: make(chan struct{})}
	core.journal = gate
	original := retryTestStart(t, manager, retryTestIntent(t))
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), retryRaceKey{}, true), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := core.retryExecution(ctx, original.Chat.ID, original.Execution.ID); result <- err }()
	select {
	case <-gate.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	intent := retryTestIntent(t)
	intent.Chat = ChatTarget{ChatID: original.Chat.ID}
	intent.ClientRequestID = ""
	intent.Input.Content = "newer normal message"
	retryTestStart(t, manager, intent)
	close(gate.proceed)
	if err := <-result; !errors.Is(err, status_error.ExecutionRetryUnavailable) {
		t.Fatalf("普通发送完成后重试了旧消息：%v", err)
	}
	runner.mutex.Lock()
	count := len(runner.requests)
	runner.mutex.Unlock()
	if count != 2 {
		t.Fatalf("额外派发了旧输入：%d", count)
	}
}

// TestExecutionFromRecordsKeepsParent 验证从 durable 事实恢复同一个重试子执行身份。
func TestExecutionFromRecordsKeepsParent(t *testing.T) {
	data, err := json.Marshal(eventlog.ExecutionCreatedData{ParentExecutionID: "parent", UserMessageID: "message"})
	if err != nil {
		t.Fatal(err)
	}
	execution, latest, err := executionFromRecords([]eventlog.Record{{ChatID: "chat", ExecutionID: "child", Type: constants.ChatEventTypeExecutionCreated, Data: data}}, "child")
	if err != nil || execution.ParentExecutionID != "parent" || latest != "child" {
		t.Fatalf("恢复结果错误：%+v %s %v", execution, latest, err)
	}
}

// TestRetryExecutionConcurrentRequestsShareOneChild 验证并发重试只有一次派发，旧父执行不会被替换为新失败。
func TestRetryExecutionConcurrentRequestsShareOneChild(t *testing.T) {
	runner := &retryTestRuntime{result: retryableTestResult()}
	manager := newMessageTestService(t, t.TempDir(), runner)
	original := retryTestStart(t, manager, retryTestIntent(t))
	type outcome struct {
		// accepted 是重复请求返回的同一个子执行。
		accepted AcceptedExecution
		// err 是尚在接纳中的可重复请求状态。
		err error
	}
	outcomes := make(chan outcome, 8)
	start := make(chan struct{})
	for range cap(outcomes) {
		go func() {
			<-start
			accepted, err := manager.execution.Start(context.Background(), WorkIntent{Chat: ChatTarget{ChatID: original.Chat.ID}, RetryExecutionID: original.Execution.ID})
			outcomes <- outcome{accepted: accepted, err: err}
		}()
	}
	close(start)
	childID := ""
	for range cap(outcomes) {
		result := <-outcomes
		if errors.Is(result.err, status_error.ExecutionRetryInProgress) {
			continue
		}
		if result.err != nil {
			t.Fatal(result.err)
		}
		if childID != "" && childID != result.accepted.Execution.ID {
			t.Fatalf("创建多个子执行：%s %s", childID, result.accepted.Execution.ID)
		}
		childID = result.accepted.Execution.ID
		if result.accepted.Execution.ParentExecutionID != original.Execution.ID {
			t.Fatalf("父执行改变：%+v", result.accepted)
		}
	}
	if childID == "" {
		t.Fatal("没有任何重试成功接纳")
	}
	waitForChatTerminal(t, manager, original.Chat.ID)
	next := retryTestIntent(t)
	next.Chat = ChatTarget{ChatID: original.Chat.ID}
	next.ClientRequestID = ""
	next.Input.Content = "later normal input"
	retryTestStart(t, manager, next)
	replayed, err := manager.execution.Start(context.Background(), WorkIntent{Chat: ChatTarget{ChatID: original.Chat.ID}, RetryExecutionID: original.Execution.ID})
	if err != nil || !replayed.Replayed || replayed.Execution.ID != childID {
		t.Fatalf("旧父执行重复请求指向新回合：%+v %v", replayed, err)
	}
	runner.mutex.Lock()
	count := len(runner.requests)
	runner.mutex.Unlock()
	if count != 3 {
		t.Fatalf("并发请求被重复派发：%d", count)
	}
}
