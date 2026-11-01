package execution

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventstream "github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// observedLiveJournal 复用真实日志提交，只观察对外增量帧的数量和最终内容。
type observedLiveJournal struct {
	// message 是测试呈现消费者从增量恢复的消息状态。
	message *messageview.Message
	// Journal 保留真实日志端口的其他行为。
	chatservice.Journal
	// mutex 保护异步快照观察结果。
	mutex sync.Mutex
	// count 是已经交付的增量帧数量。
	count int
	// bodyBytes 累计真实增量信封中的新增文本有效字节。
	bodyBytes int
	// wireBytes 累计真实增量信封的序列化字节，包含协议元数据。
	wireBytes int
	// latest 保留最后一份公开快照。
	latest eventstream.PublicEvent
	// changed 使暂停输入后的界面交付可被有界等待。
	changed chan struct{}
}

// PublishLive 记录真实公开结果，不为每条更新制造无界测试队列。
func (j *observedLiveJournal) PublishLive(_ string, event eventstream.PublicEvent) {
	j.mutex.Lock()
	j.count++
	encoded, _ := json.Marshal(event)
	j.wireBytes += len(encoded)
	for _, update := range event.Live.Updates {
		for _, operation := range update.Parts {
			j.bodyBytes += len(operation.Text)
		}
	}
	j.latest = foldTestLive(&j.message, event)
	j.mutex.Unlock()
	select {
	case j.changed <- struct{}{}:
	default:
	}
}

// TestRuntimeLiveBurstCoalescesBeforeSnapshot 验证突发文本先合并交付窗口，再打包新增操作，最终正文与重放一致。
func TestRuntimeLiveBurstCoalescesBeforeSnapshot(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const chatID = "1234abcd1234abcd1234abcd1234abcd"
	if _, err = store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, _ := newRuntimeEventSinkForTest(store, chatID)
	journal := &observedLiveJournal{Journal: sink.journal, changed: make(chan struct{}, 1)}
	sink.journal = journal
	for range 1000 {
		if err = sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: marshalData(map[string]string{"text": "x"})}); err != nil {
			t.Fatal(err)
		}
	}
	if err = sink.Flush(); err != nil {
		t.Fatal(err)
	}
	journal.mutex.Lock()
	count, latest := journal.count, journal.latest
	journal.mutex.Unlock()
	if count >= 200 {
		t.Fatalf("1000 tokens materialized %d live increment frames; batching after serialization cannot bound producer work", count)
	}
	t.Logf("1000 text deltas produced %d live increment frames", count)
	if latest.Message == nil || latest.Message.Content != strings.Repeat("x", 1000) {
		t.Fatalf("final live text was not flushed: %+v", latest.Message)
	}
	// 终态刷新已取消显示窗口，迟到的定时回调不能重新发布已结束执行的实时快照。
	select {
	case <-journal.changed:
	default:
	}
	select {
	case <-journal.changed:
		t.Fatal("a pending timer published another snapshot after final flush")
	case <-time.After(3 * runtimeLiveBatchInterval):
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := messageview.ReplayPublicMessages(records)
	if err != nil || len(messages) != 1 || messages[0].Content != latest.Message.Content || messages[0].Revision != latest.Message.Revision {
		t.Fatalf("live/replay differ: messages=%+v live=%+v err=%v", messages, latest.Message, err)
	}
}

// TestRuntimeSinkRejectsCanceledDeliveryBeforeMutation 验证已取消的交付不能在收尾之后补写正文或发布快照。
func TestRuntimeSinkRejectsCanceledDeliveryBeforeMutation(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const chatID = "1234abcd1234abcd1234abcd1234abcf"
	if _, err = store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, _ := newRuntimeEventSinkForTest(store, chatID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = sink.acceptInvocation(ctx, "invocation-1", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: marshalData(map[string]string{"text": "late"})})
	if err != status_error.OperationCanceled {
		t.Fatalf("canceled delivery was accepted: %v", err)
	}
	if err = sink.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err := store.ReadAll(chatID)
	if err != nil || len(records) != 3 {
		t.Fatalf("late delivery changed durable history: records=%d err=%v", len(records), err)
	}
}

// TestRuntimeLivePausedProducerStillPublishesLatest 验证没有下一条事件时，最新正文仍会进入界面。
func TestRuntimeLivePausedProducerStillPublishesLatest(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const chatID = "1234abcd1234abcd1234abcd1234abce"
	if _, err = store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, _ := newRuntimeEventSinkForTest(store, chatID)
	journal := &observedLiveJournal{Journal: sink.journal, changed: make(chan struct{}, 1)}
	sink.journal = journal
	if err = sink.acceptInvocation(context.Background(), "invocation-1", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: marshalData(map[string]string{"text": "等待工具期间也要显示"})}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-journal.changed:
	case <-time.After(time.Second):
		t.Fatal("paused stream never published its latest snapshot")
	}
	if err = sink.Flush(); err != nil {
		t.Fatal(err)
	}
}

// TestRuntimeCheckpointBodyBudget 验证单次持久检查点的正文计量，不包含随后执行终态的第二次完整公开下发。
// 完整公开链路的正文预算由 scripts/performance/backend-ab 的旧新源码对照计量，不能由本例推导为三倍。
func TestRuntimeCheckpointBodyBudget(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const chatID = "1234abcd1234abcd1234abcd1234abcd"
	if _, err = store.Create(chatID, activeChatInputs()); err != nil {
		t.Fatal(err)
	}
	sink, _ := newRuntimeEventSinkForTest(store, chatID)
	journal := &observedLiveJournal{Journal: sink.journal, changed: make(chan struct{}, 1)}
	sink.journal = journal
	chunk := strings.Repeat("a", 512)
	for range 100 {
		if err = sink.acceptInvocation(context.Background(), "body-budget", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: marshalData(map[string]string{"text": chunk})}); err != nil {
			t.Fatal(err)
		}
	}
	if err = sink.Flush(); err != nil {
		t.Fatal(err)
	}
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := messageview.ReplayPublicMessages(records)
	if err != nil || len(messages) != 1 {
		t.Fatalf("replay: %v", err)
	}
	durableBody := len(messages[0].Content)
	for _, part := range messages[0].DisplayParts {
		durableBody += len(part.Text)
	}
	journal.mutex.Lock()
	liveBody, frames, wireBytes := journal.bodyBytes, journal.count, journal.wireBytes
	journal.mutex.Unlock()
	if liveBody != 50*1024 || durableBody != 2*50*1024 || liveBody+durableBody > 3*50*1024 {
		t.Fatalf("body payload live=%d durable=%d", liveBody, durableBody)
	}
	t.Logf("checkpoint-only body: final=%d live=%d durable=%d frames=%d live_wire=%d", len(messages[0].Content), liveBody, durableBody, frames, wireBytes)
}
