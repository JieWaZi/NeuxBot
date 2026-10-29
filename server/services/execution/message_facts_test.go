package execution

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// messageFactTestJournal 观察真实 Journal 的实时交付和初始化读取次数，不替代持久化与协调器。
type messageFactTestJournal struct {
	// Journal 执行真实 JSONL 与临时 SQLite 提交。
	chatservice.Journal
	// live 保存真实 Journal 发布的 transient 快照。
	live chan eventstream.PublicEvent
	// reads 记录完整执行事实读取，防止用逐增量重放掩盖状态同步缺口。
	reads atomic.Int64
}

func (j *messageFactTestJournal) PublishLive(chatID string, event eventstream.PublicEvent) {
	j.Journal.PublishLive(chatID, event)
	j.live <- event
}
func (j *messageFactTestJournal) ReadExecutionRecords(chatID, executionID string) ([]eventlog.Record, error) {
	j.reads.Add(1)
	return j.Journal.ReadExecutionRecords(chatID, executionID)
}

// approvalFactRuntime 经真实执行接纳、审批协调器和 Journal 形成确定性的审批后增量。
type approvalFactRuntime struct {
	// fakeRuntime 复用测试所需的 Runtime 目录与会话能力。
	fakeRuntime
	// prefixSnapshots 在审批前形成已经公开但尚未持久化的高修订工具快照。
	prefixSnapshots int
	// nextKind 是审批后交付的工具输入或执行进度；空值表示后续没有 Runtime 事件。
	nextKind runtimecontract.EventType
	// continued 表示审批决定已返回且后续增量已经进入执行层。
	continued chan struct{}
	// finish 让测试在读取实时与权威快照之后释放执行。
	finish chan struct{}
}

func (r *approvalFactRuntime) RunWithLease(ctx context.Context, request runtimecontract.RunRequest, sink runtimecontract.EventSink, release func(context.Context) error) runtimecontract.Result {
	if release != nil {
		defer func() { _ = release(context.WithoutCancel(ctx)) }()
	}
	invocationID := "approval-fact-invocation"
	send := func(kind runtimecontract.EventType, data any) {
		_ = sink.Accept(ctx, runtimecontract.Event{Type: kind, Data: marshalData(data)})
	}
	send(runtimecontract.EventTypeInvocationStarted, runtimecontract.InvocationStartedData{InvocationID: invocationID})
	send(runtimecontract.EventTypeToolUse, map[string]any{"toolCallId": "tool", "name": "bash", "input": map[string]any{"command": "echo allowed"}})
	for range r.prefixSnapshots {
		send(runtimecontract.EventTypeToolInputSnapshot, map[string]any{"toolCallId": "tool", "input": map[string]any{"command": "echo allowed"}})
	}
	approval := approvalRequest("tool")
	approval.InvocationID = invocationID
	_, _ = request.ApprovalHandler.Request(ctx, approval)
	if r.nextKind != "" {
		send(r.nextKind, map[string]any{"toolCallId": "tool", "input": map[string]any{"command": "echo allowed"}})
	}
	close(r.continued)
	select {
	case <-r.finish:
	case <-ctx.Done():
	}
	send(runtimecontract.EventTypeToolResult, map[string]any{"toolCallId": "tool", "status": "completed"})
	return runtimecontract.Result{Facts: runtimecontract.InvocationFacts{InvocationID: invocationID, RuntimeID: request.Target.RuntimeID, BaseRuntimeID: runtimecontract.RuntimeIDCodex}}
}

func TestDurableApprovalRemainsInSubsequentLiveToolSnapshot(t *testing.T) {
	for _, kind := range []runtimecontract.EventType{runtimecontract.EventTypeToolInputSnapshot, runtimecontract.EventTypeToolUpdate} {
		t.Run(string(kind), func(t *testing.T) {
			runtime := &approvalFactRuntime{nextKind: kind, continued: make(chan struct{}), finish: make(chan struct{})}
			defer close(runtime.finish)
			manager := newMessageTestService(t, t.TempDir(), runtime)
			core := manager.execution.(*service)
			observed := &messageFactTestJournal{Journal: core.journal, live: make(chan eventstream.PublicEvent, 16)}
			core.journal = observed
			accepted, err := manager.sendTestMessage(context.Background(), testMessageInput{NewChat: &testChatInput{WakerID: "waker-1", WorkingDirectory: t.TempDir()}, Message: userMessage{Text: "执行已授权测试"}, Runtime: runtimeSelection{RuntimeID: runtimecontract.RuntimeIDCodex, PermissionLevel: runtimecontract.PermissionLevelDefault}})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-observed.live:
			case <-time.After(2 * time.Second):
				t.Fatal("tool start was not published")
			}
			var approvalID string
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				pending := core.approvals.pendingProjection(accepted.Chat.ID)
				if len(pending) == 1 {
					approvalID = pending[0].ApprovalID
					break
				}
				time.Sleep(time.Millisecond)
			}
			if approvalID == "" {
				t.Fatal("real coordinator did not persist an approval request")
			}
			if err := core.ResolveApproval(context.Background(), accepted.Chat.ID, approvalID, "allow-once"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-runtime.continued:
			case <-time.After(2 * time.Second):
				t.Fatal("approval decision did not resume runtime")
			}
			var live *messageview.Message
			select {
			case event := <-observed.live:
				live = event.Message
			case <-time.After(2 * time.Second):
				t.Fatal("post-approval live snapshot was not published")
			}
			records, err := observed.ReadRecords(accepted.Chat.ID)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := messageview.ReplayPublicMessages(records)
			if err != nil {
				t.Fatal(err)
			}
			var durable *messageview.ToolApprovalResult
			for _, message := range replay {
				if message.ID == accepted.Execution.ID {
					durable = message.DisplayParts[0].ToolApprovalResult
				}
			}
			if durable == nil || durable.Result != constants.ApprovalResultSelected {
				t.Fatalf("real durable approval missing: %+v", durable)
			}
			if live == nil || len(live.DisplayParts) == 0 || live.DisplayParts[0].ToolApprovalResult == nil || live.DisplayParts[0].ToolApprovalResult.Result != durable.Result {
				t.Fatalf("committed approval is lost in next transient: live=%+v durable=%+v", live, durable)
			}
			if observed.reads.Load() != 1 {
				t.Fatalf("runtime reread full execution %d times, want initialization only", observed.reads.Load())
			}
		})
	}
}

// newMessageFactFixture 保留真实 Chat Journal 与审批协调器，只让测试直接控制单个 Sink 的窗口边界。
func newMessageFactFixture(t *testing.T) (*service, *runtimeEventSink, *messageFactTestJournal, string) {
	t.Helper()
	manager := newMessageTestService(t, t.TempDir(), &fakeRuntime{})
	core := manager.execution.(*service)
	chatID := "89898989898989898989898989898989"
	workspace := t.TempDir()
	module := manager.chats.(chatservice.Module)
	if _, err := module.CreateDormantChats(context.Background(), []chatservice.DormantChat{{Chat: models.Chat{ID: chatID, WakerID: "waker-1", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.journal.AppendExecutionFacts(context.Background(), chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution", Data: marshalData(eventlog.ExecutionCreatedData{})}, {Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "execution", InvocationID: "invocation-1"}}); err != nil {
		t.Fatal(err)
	}
	observed := &messageFactTestJournal{Journal: core.journal, live: make(chan eventstream.PublicEvent, 32)}
	sink := &runtimeEventSink{journal: observed, chatID: chatID, executionID: "execution", messageFacts: &executionMessageFacts{}}
	sink.messageFacts.start(sink.scheduleMessageFacts)
	t.Cleanup(func() { sink.messageFacts.stopDelivery(); _ = sink.Flush() })
	return core, sink, observed, chatID
}

func requestMessageFactApproval(t *testing.T, core *service, sink *runtimeEventSink, chatID, invocation string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	request := approvalRequest("tool")
	request.InvocationID = invocation
	go func() { _, _ = core.approvals.handler(chatID, "execution", sink.messageFacts).Request(ctx, request) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending := core.approvals.pendingProjection(chatID)
		if len(pending) == 1 {
			return pending[0].ApprovalID
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("real coordinator did not register approval")
	return ""
}

func TestLiveWindowAndFlushConsumeDurableApprovalWithoutAnotherRuntimeEvent(t *testing.T) {
	for _, boundary := range []string{"timer", "flush"} {
		t.Run(boundary, func(t *testing.T) {
			core, sink, journal, chatID := newMessageFactFixture(t)
			if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimecontract.Event{Type: runtimecontract.EventTypeToolUse, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "read"})}); err != nil {
				t.Fatal(err)
			}
			<-journal.live
			id := requestMessageFactApproval(t, core, sink, chatID, "invocation-1")
			if boundary == "timer" {
				sink.mutex.Lock()
				err := sink.acceptLocked("invocation-1", runtimecontract.Event{Type: runtimecontract.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "tool", "input": map[string]any{"path": "main.go"}})})
				if err != nil {
					sink.mutex.Unlock()
					t.Fatal(err)
				}
				resolved := make(chan error, 1)
				go func() { resolved <- core.ResolveApproval(context.Background(), chatID, id, "allow-once") }()
				select {
				case err := <-resolved:
					if err != nil {
						sink.mutex.Unlock()
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					sink.mutex.Unlock()
					t.Fatal("coordinator commit waited for Sink lock")
				}
				sink.mutex.Unlock()
			} else {
				if err := core.ResolveApproval(context.Background(), chatID, id, "allow-once"); err != nil {
					t.Fatal(err)
				}
				if err := sink.Flush(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case event := <-journal.live:
				if event.Message.DisplayParts[0].ToolApprovalResult == nil {
					t.Fatalf("%s published stale state after durable approval: %+v", boundary, event.Message)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%s did not publish its current state", boundary)
			}
			if journal.reads.Load() != 1 {
				t.Fatalf("%s reread execution %d times", boundary, journal.reads.Load())
			}
		})
	}
}

func TestLateApprovalKeepsOriginatingInvocationWithReusedNativeToolID(t *testing.T) {
	core, sink, journal, chatID := newMessageFactFixture(t)
	for _, invocation := range []string{"invocation-1", "invocation-2"} {
		sink.messageFacts.setInvocation(invocation)
		if err := sink.acceptInvocation(context.Background(), invocation, runtimecontract.Event{Type: runtimecontract.EventTypeToolUse, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "read"})}); err != nil {
			t.Fatal(err)
		}
		<-journal.live
	}
	// 回调迟于第二次调用到达，但原生 Bridge 明确提供第一次调用身份。
	id := requestMessageFactApproval(t, core, sink, chatID, "invocation-1")
	if err := core.ResolveApproval(context.Background(), chatID, id, "allow-once"); err != nil {
		t.Fatal(err)
	}
	if err := sink.Flush(); err != nil {
		t.Fatal(err)
	}
	var live *messageview.Message
	select {
	case event := <-journal.live:
		live = event.Message
	case <-time.After(2 * time.Second):
		t.Fatal("flush did not publish approval fact")
	}
	if len(live.DisplayParts) != 2 || live.DisplayParts[0].ToolApprovalResult == nil || live.DisplayParts[1].ToolApprovalResult != nil {
		t.Fatalf("late approval contaminated the latest reused tool ID: %+v", live.DisplayParts)
	}
	records, err := journal.ReadRecords(chatID)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Type == constants.ChatEventTypeApprovalRequested || record.Type == constants.ChatEventTypeApprovalResolved {
			if record.InvocationID != "invocation-1" {
				t.Fatalf("durable approval scope=%q, want originating invocation", record.InvocationID)
			}
		}
	}
}

func TestReconnectLiveCacheCatchesUpDurableApprovalWithoutRuntimeOutput(t *testing.T) {
	runtime := &approvalFactRuntime{prefixSnapshots: 32, continued: make(chan struct{}), finish: make(chan struct{})}
	defer close(runtime.finish)
	manager := newMessageTestService(t, t.TempDir(), runtime)
	core := manager.execution.(*service)
	observed := &messageFactTestJournal{Journal: core.journal, live: make(chan eventstream.PublicEvent, 32)}
	core.journal = observed
	accepted, err := manager.sendTestMessage(context.Background(), testMessageInput{NewChat: &testChatInput{WakerID: "waker-1", WorkingDirectory: t.TempDir()}, Message: userMessage{Text: "执行后等候"}, Runtime: runtimeSelection{RuntimeID: runtimecontract.RuntimeIDCodex, PermissionLevel: runtimecontract.PermissionLevelDefault}})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-observed.live:
		case <-time.After(2 * time.Second):
			t.Fatal("uncheckpointed tool snapshot was not published")
		}
	}
	var id string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending := core.approvals.pendingProjection(accepted.Chat.ID)
		if len(pending) == 1 {
			id = pending[0].ApprovalID
			break
		}
		time.Sleep(time.Millisecond)
	}
	if id == "" {
		t.Fatal("coordinator did not persist request")
	}
	if err := core.ResolveApproval(context.Background(), accepted.Chat.ID, id, "allow-once"); err != nil {
		t.Fatal(err)
	}
	<-runtime.continued
	subscription, err := manager.chats.(chatservice.Module).SubscribeEvents(context.Background(), accepted.Chat.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	timeout := time.NewTimer(500 * time.Millisecond)
	defer timeout.Stop()
	for {
		select {
		case event := <-subscription.Events:
			if !event.Transient || event.Message == nil || len(event.Message.DisplayParts) == 0 {
				continue
			}
			if result := event.Message.DisplayParts[0].ToolApprovalResult; result != nil && result.Result == constants.ApprovalResultSelected {
				if observed.reads.Load() != 1 {
					t.Fatalf("external fact wake reread execution %d times", observed.reads.Load())
				}
				return
			}
		case <-timeout.C:
			t.Fatal("reconnected subscriber only received the stale high-revision transient without durable approval")
		}
	}
}

func TestFactWakeWaitsForFirstRuntimeMessageBeforeInitializingLiveState(t *testing.T) {
	core, sink, journal, chatID := newMessageFactFixture(t)
	id := requestMessageFactApproval(t, core, sink, chatID, "invocation-1")
	sink.messageFacts.stopDelivery()
	if journal.reads.Load() != 0 || sink.liveAdapter != nil {
		t.Fatal("interaction wake initialized live state before the first Runtime event")
	}
	if err := core.ResolveApproval(context.Background(), chatID, id, "allow-once"); err != nil {
		t.Fatal(err)
	}
	if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimecontract.Event{Type: runtimecontract.EventTypeToolUse, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "read"})}); err != nil {
		t.Fatal(err)
	}
	event := <-journal.live
	if event.Message.DisplayParts[0].ToolApprovalResult == nil {
		t.Fatal("initial authority and queued duplicate facts lost the resolved approval")
	}
	if journal.reads.Load() != 1 {
		t.Fatalf("first Runtime event performed %d full reads", journal.reads.Load())
	}
}

func TestFlushFactErrorInvalidatesPendingLiveWindow(t *testing.T) {
	_, sink, journal, chatID := newMessageFactFixture(t)
	if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimecontract.Event{Type: runtimecontract.EventTypeToolUse, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "read"})}); err != nil {
		t.Fatal(err)
	}
	<-journal.live
	sink.messageFacts.stopDelivery()
	// 事实本身已进入真实 JSONL，但属于另一个执行，当前实时端口必须拒绝。
	foreign, err := journal.AppendApprovalFact(chatID, eventlog.AppendInput{Type: constants.ChatEventTypeApprovalResolved, ExecutionID: "another-execution", Data: marshalData(eventlog.ApprovalResolvedData{ToolCallID: "tool", Result: constants.ApprovalResultSelected})})
	if err != nil {
		t.Fatal(err)
	}
	sink.mutex.Lock()
	err = sink.acceptLocked("invocation-1", runtimecontract.Event{Type: runtimecontract.EventTypeToolInputSnapshot, Data: marshalData(map[string]any{"toolCallId": "tool", "input": map[string]any{"path": "main.go"}})})
	sink.messageFacts.push(foreign)
	sink.mutex.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Flush(); err == nil {
		t.Fatal("Flush accepted another execution's durable fact")
	}
	select {
	case event := <-journal.live:
		t.Fatalf("old live window published after Flush failed: %+v", event)
	case <-time.After(3 * runtimeLiveSnapshotInterval):
	}
}

func TestApprovalFactsKeepPendingBatchAndNextToolRevisionsOrdered(t *testing.T) {
	for _, pendingKind := range []runtimecontract.EventType{runtimecontract.EventTypeTextDelta, runtimecontract.EventTypeToolInputSnapshot} {
		for _, boundary := range []string{"flush", "next-tool"} {
			t.Run(string(pendingKind)+"/"+boundary, func(t *testing.T) {
				core, sink, journal, chatID := newMessageFactFixture(t)
				sink.messageFacts.stopDelivery()
				if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimecontract.Event{Type: runtimecontract.EventTypeToolUse, Data: marshalData(map[string]any{"toolCallId": "tool", "name": "read"})}); err != nil {
					t.Fatal(err)
				}
				<-journal.live
				data := map[string]any{"text": "正在处理"}
				if pendingKind == runtimecontract.EventTypeToolInputSnapshot {
					data = map[string]any{"toolCallId": "tool", "input": map[string]any{"path": "main.go"}}
				}
				if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimecontract.Event{Type: pendingKind, Data: marshalData(data)}); err != nil {
					t.Fatal(err)
				}
				id := requestMessageFactApproval(t, core, sink, chatID, "invocation-1")
				if err := core.ResolveApproval(context.Background(), chatID, id, "allow-once"); err != nil {
					t.Fatal(err)
				}
				if boundary == "next-tool" {
					if err := sink.acceptInvocation(context.Background(), "invocation-1", runtimecontract.Event{Type: runtimecontract.EventTypeToolUse, Data: marshalData(map[string]any{"toolCallId": "tool-2", "name": "read"})}); err != nil {
						t.Fatal(err)
					}
				}
				if err := sink.Flush(); err != nil {
					t.Fatal(err)
				}
				var live *messageview.Message
				for len(journal.live) > 0 {
					live = (<-journal.live).Message
				}
				if live == nil {
					t.Fatal("boundary did not publish current live state")
				}
				records, err := journal.ReadRecords(chatID)
				if err != nil {
					t.Fatal(err)
				}
				messages, err := messageview.ReplayPublicMessages(records)
				if err != nil {
					t.Fatal(err)
				}
				if len(messages) != 1 || messages[0].Revision != live.Revision || !reflect.DeepEqual(messages[0].DisplayParts, live.DisplayParts) {
					t.Fatalf("fact boundary diverged: live revision=%d replay=%+v", live.Revision, messages)
				}
				if boundary == "next-tool" {
					var revisions []int64
					for _, record := range records {
						if record.Type == constants.ChatEventTypeAssistantMessage {
							var message eventlog.RuntimeMessageData
							if err := json.Unmarshal(record.Data, &message); err != nil {
								t.Fatal(err)
							}
							revisions = append(revisions, message.MessageRevision)
						}
					}
					if len(revisions) < 3 || revisions[len(revisions)-2] >= revisions[len(revisions)-1] {
						t.Fatalf("pending segment and next tool share or reverse revision: %v", revisions)
					}
				}
			})
		}
	}
}
