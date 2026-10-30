package eventlog

import (
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// countedReadAt 统计真实定位读取的字节与调用次数，不替代文件内容。
type countedReadAt struct {
	// source 是实际临时索引或权威日志文件。
	source io.ReaderAt
	// bytes 是实际请求读取的字节总量。
	bytes int
	// calls 是随机读取调用总数。
	calls int
}

func (r *countedReadAt) ReadAt(data []byte, offset int64) (int, error) {
	r.bytes += len(data)
	r.calls++
	return r.source.ReadAt(data, offset)
}

// TestExecutionFactIOBudgetDoesNotGrowWithPrefix 验证精确起点读取不会暗中遍历旧索引或消息正文。
func TestExecutionFactIOBudgetDoesNotGrowWithPrefix(t *testing.T) {
	measure := func(prefix int) (int, int, int) {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		id := "97979797979797979797979797979797"
		inputs := []AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalTestData(t, ChatCreatedData{})}}
		for i := 0; i < prefix; i++ {
			execution := fmt.Sprintf("old-%d", i)
			inputs = append(inputs, AppendInput{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: execution}, AppendInput{Type: constants.ChatEventTypeExecutionCompleted, Data: marshalTestData(t, ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})})
		}
		start := int64(len(inputs) + 1)
		inputs = append(inputs,
			AppendInput{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "wanted", Data: marshalTestData(t, ExecutionCreatedData{SubmissionID: "submission"})},
			AppendInput{Type: constants.ChatEventTypeAssistantMessage, InvocationID: "only-on-message", Data: marshalTestData(t, RuntimeMessageData{MessageRevision: 1})},
			AppendInput{Type: constants.ChatEventTypeInvocationFinished, InvocationID: "only-on-message", Data: marshalTestData(t, InvocationFinishedData{RecoveryStrategy: constants.InvocationRecoveryInitial, TerminationReason: constants.RuntimeTerminationReasonNormal})},
			AppendInput{Type: constants.ChatEventTypeExecutionCompleted, Data: marshalTestData(t, ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		)
		records, err := store.Create(id, inputs)
		if err != nil {
			t.Fatal(err)
		}
		index, err := os.Open(store.indexPath(id))
		if err != nil {
			t.Fatal(err)
		}
		defer index.Close()
		log, err := os.Open(store.path(id))
		if err != nil {
			t.Fatal(err)
		}
		defer log.Close()
		indexReads, logReads := &countedReadAt{source: index}, &countedReadAt{source: log}
		window, err := readExecutionFactWindow(id, ExecutionFactPositions{StartSequence: start, LifecycleSequences: []int64{start + 3}, InvocationFinishedSequence: start + 2, InvocationBoundarySequence: start + 1, LatestSequence: records[len(records)-1].Sequence}, indexReads, logReads)
		if err != nil {
			t.Fatal(err)
		}
		if len(window.Records) != 4 || window.Records[2].InvocationID != "only-on-message" {
			t.Fatalf("window lost scoped invocation: %+v", window)
		}
		return indexReads.bytes, logReads.bytes, indexReads.calls + logReads.calls
	}
	smallIndex, smallLog, smallCalls := measure(8)
	largeIndex, largeLog, largeCalls := measure(1024)
	if largeIndex != smallIndex || largeLog != smallLog || largeCalls != smallCalls {
		t.Fatalf("prefix grew precise I/O: index %d -> %d, log %d -> %d, calls %d -> %d", smallIndex, largeIndex, smallLog, largeLog, smallCalls, largeCalls)
	}
	t.Logf("precise budget: index=%d log=%d bytes, reads=%d", largeIndex, largeLog, largeCalls)
}

// TestCoveredExecutionFactIOBudgetIgnoresGrowingCurrentBody 验证同一活跃执行正文增长不扩大已覆盖状态的读取成本。
func TestCoveredExecutionFactIOBudgetIgnoresGrowingCurrentBody(t *testing.T) {
	measure := func(count int) (int, int, int) {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		id := "96969696969696969696969696969696"
		inputs := []AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalTestData(t, ChatCreatedData{})}, {Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "current", Data: marshalTestData(t, ExecutionCreatedData{})}}
		for i := 0; i < count; i++ {
			inputs = append(inputs, AppendInput{Type: constants.ChatEventTypeAssistantMessage, InvocationID: "body-invocation", Data: marshalTestData(t, RuntimeMessageData{MessageRevision: int64(i + 1)})})
		}
		inputs = append(inputs, AppendInput{Type: constants.ChatEventTypeInvocationFinished, InvocationID: "body-invocation", Data: marshalTestData(t, InvocationFinishedData{RecoveryStrategy: constants.InvocationRecoveryInitial, TerminationReason: constants.RuntimeTerminationReasonNormal})})
		records, err := store.Create(id, inputs)
		if err != nil {
			t.Fatal(err)
		}
		index, err := os.Open(store.indexPath(id))
		if err != nil {
			t.Fatal(err)
		}
		defer index.Close()
		log, err := os.Open(store.path(id))
		if err != nil {
			t.Fatal(err)
		}
		defer log.Close()
		idxReads, logReads := &countedReadAt{source: index}, &countedReadAt{source: log}
		window, err := readExecutionFactWindow(id, ExecutionFactPositions{StartSequence: 2, InvocationFinishedSequence: records[len(records)-1].Sequence, InvocationBoundarySequence: 3, LatestSequence: records[len(records)-1].Sequence}, idxReads, logReads)
		if err != nil {
			t.Fatal(err)
		}
		if window.Records[len(window.Records)-1].InvocationID != "body-invocation" {
			t.Fatal("raw body invocation identity lost")
		}

		missIndex, missLog := &countedReadAt{source: index}, &countedReadAt{source: log}
		missing, err := readExecutionFactWindow(id, ExecutionFactPositions{LatestSequence: records[len(records)-1].Sequence}, missIndex, missLog)
		if err != nil || len(missing.Records) != 1 || missIndex.bytes != int(indexEntrySize) || missLog.bytes != int(records[0].Length) || missIndex.calls+missLog.calls != 2 {
			t.Fatalf("covered miss read body/index: records=%d index=%d log=%d reads=%d err=%v", len(missing.Records), missIndex.bytes, missLog.bytes, missIndex.calls+missLog.calls, err)
		}
		return idxReads.bytes, logReads.bytes, idxReads.calls + logReads.calls
	}
	si, sl, sc := measure(8)
	li, ll, lc := measure(1024)
	if li != si || ll != sl || lc != sc {
		t.Fatalf("covered growing-current status I/O: index %d -> %d, log %d -> %d, calls %d -> %d", si, li, sl, ll, sc, lc)
	}
	t.Logf("growing-current covered budget: index=%d log=%d bytes reads=%d", li, ll, lc)
}

// TestCompletedHistoryCandidateIOBudgetSkipsGrowingExecutionBodies 验证成功轮定位整体越过失败及工具正文，只读取入口与终态。
func TestCompletedHistoryCandidateIOBudgetSkipsGrowingExecutionBodies(t *testing.T) {
	measure := func(count int) (int, int, int) {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		id := "95959595959595959595959595959595"
		inputs := []AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalTestData(t, ChatCreatedData{})}}
		for _, execution := range []string{"success", "failed"} {
			inputs = append(inputs, AppendInput{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "input-" + execution, Data: marshalTestData(t, MessageCreatedData{Role: constants.MessageRoleUser, Actor: "user", Content: execution})}, AppendInput{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: execution, Data: marshalTestData(t, ExecutionCreatedData{UserMessageID: "input-" + execution})})
			for i := 0; i < count; i++ {
				inputs = append(inputs, AppendInput{Type: constants.ChatEventTypeToolMessage, InvocationID: execution, Data: marshalTestData(t, RuntimeMessageData{MessageRevision: int64(i + 1)})})
			}
			status, kind := constants.ExecutionStatusCompleted, constants.ChatEventTypeExecutionCompleted
			if execution == "failed" {
				status, kind = constants.ExecutionStatusFailed, constants.ChatEventTypeExecutionFailed
			}
			inputs = append(inputs, AppendInput{Type: kind, Data: marshalTestData(t, ExecutionFinishedData{Status: status})})
		}
		records, err := store.Create(id, inputs)
		if err != nil {
			t.Fatal(err)
		}
		index, err := os.Open(store.indexPath(id))
		if err != nil {
			t.Fatal(err)
		}
		defer index.Close()
		log, err := os.Open(store.path(id))
		if err != nil {
			t.Fatal(err)
		}
		defer log.Close()
		indexReads, logReads := &countedReadAt{source: index}, &countedReadAt{source: log}
		page, err := readCompletedHistoryCandidates(id, 0, 1, records[len(records)-1].Sequence, indexReads, logReads)
		if err != nil || len(page.Resources) != 1 || page.Resources[0].MessageID != "success" {
			t.Fatalf("successful candidate=%+v %v", page, err)
		}
		return indexReads.bytes, logReads.bytes, indexReads.calls + logReads.calls
	}
	si, sl, sc := measure(8)
	li, ll, lc := measure(1024)
	if li != si || ll != sl || lc != sc {
		t.Fatalf("candidate body-count cost grew: index %d -> %d, log %d -> %d, reads %d -> %d", si, li, sl, ll, sc, lc)
	}
	t.Logf("completed-history candidate budget: index=%d log=%d bytes reads=%d", li, ll, lc)
}
