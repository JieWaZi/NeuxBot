package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// abTraffic 仅累计生产公开载荷；不保存旧帧或折叠第二份实时消息。
type abTraffic struct {
	// LiveFrames 是实际 transient 帧数。
	LiveFrames int `json:"liveFrames"`
	// DurableFrames 是实际已提交公开帧数。
	DurableFrames int `json:"durableFrames"`
	// LiveBodyBytes 是所有 transient 正文有效字节，包含旧快照内的重复正文。
	LiveBodyBytes int64 `json:"liveBodyBytes"`
	// DurableBodyBytes 是所有持久公开消息中的正文有效字节。
	DurableBodyBytes int64 `json:"durableBodyBytes"`
	// LiveWireBytes 是 transient 完整 JSON 载荷字节。
	LiveWireBytes int64 `json:"liveWireBytes"`
	// DurableWireBytes 是持久公开事件完整 JSON 载荷字节。
	DurableWireBytes int64 `json:"durableWireBytes"`
	// LiveToolParts 是 transient 中实际序列化的完整工具部件数。
	LiveToolParts int64 `json:"liveToolParts"`
	// DurableToolParts 是持久公开消息中的完整工具部件数。
	DurableToolParts int64 `json:"durableToolParts"`
	// LiveSerializeNs 是 transient JSON Marshal 的累计墙钟纳秒。
	LiveSerializeNs int64 `json:"liveSerializeNs"`
	// DurableSerializeNs 是持久公开事件 JSON Marshal 的累计墙钟纳秒。
	DurableSerializeNs int64 `json:"durableSerializeNs"`
}

// abJournal 复用真实 JSONL 和公开 Adapter，不使用项目测试替身的消息重建。
type abJournal struct {
	// Journal 补全此微基准不触及的领域端口。
	chatservice.Journal
	// store 是真实有 fsync 的日志存储。
	store *eventlog.Store
	// adapter 依生产路径把每条耐久事实转换为安全公开事件。
	adapter *eventstream.Adapter
	// mutex 保护异步 timer 的计数与测试主流程快照。
	mutex sync.Mutex
	// traffic 保存当前计量阶段的数字，不保留 wire payload。
	traffic abTraffic
	// lastMessage 仅保留最后一次耐久消息，供计量结束后的语义验证。
	lastMessage *messageview.Message
	// failure 是首次真实序列化或投影错误。
	failure error
}

// ReadExecutionRecords 按真实执行索引建立实时投影基线。
func (j *abJournal) ReadExecutionRecords(chatID, executionID string) ([]eventlog.Record, error) {
	return j.store.ReadMessage(chatID, executionID)
}

// PublishLive 在真实生产公开边界序列化 transient 事件。
func (j *abJournal) PublishLive(_ string, event eventstream.PublicEvent) { j.publish(event) }

// AppendExecutionFacts 先同步保存真实日志，再通过生产 Adapter 公开提交。
func (j *abJournal) AppendExecutionFacts(_ context.Context, chatID string, inputs []eventlog.AppendInput) ([]eventlog.Record, error) {
	records, err := j.store.Append(chatID, inputs)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		event, err := j.adapter.Apply(record)
		if err != nil {
			return nil, err
		}
		j.publish(event)
	}
	return records, nil
}
func (j *abJournal) publish(event eventstream.PublicEvent) {
	started := time.Now()
	payload, err := json.Marshal(event)
	elapsed := time.Since(started).Nanoseconds()
	body, tools := abProtocolCounts(event)
	j.mutex.Lock()
	defer j.mutex.Unlock()
	if err != nil {
		j.failure = err
		return
	}
	if event.Transient {
		j.traffic.LiveFrames++
		j.traffic.LiveBodyBytes += body
		j.traffic.LiveWireBytes += int64(len(payload))
		j.traffic.LiveToolParts += tools
		j.traffic.LiveSerializeNs += elapsed
	} else {
		j.traffic.DurableFrames++
		j.traffic.DurableBodyBytes += body
		j.traffic.DurableWireBytes += int64(len(payload))
		j.traffic.DurableToolParts += tools
		j.traffic.DurableSerializeNs += elapsed
		if event.Message != nil {
			j.lastMessage = event.Message
		}
	}
}
func (j *abJournal) reset() { j.mutex.Lock(); defer j.mutex.Unlock(); j.traffic = abTraffic{} }
func (j *abJournal) snapshot(t *testing.T) abTraffic {
	t.Helper()
	j.mutex.Lock()
	defer j.mutex.Unlock()
	if j.failure != nil {
		t.Fatal(j.failure)
	}
	return j.traffic
}

// abPhase 计量原生产方法与真实序列化；没有包含后置语义哈希或 JSON 解码。
type abPhase struct {
	// Traffic 是本阶段实际公开事件。
	Traffic abTraffic `json:"traffic"`
	// ElapsedMs 包含固定源事件节奏与真实同步落盘等待。
	ElapsedMs float64 `json:"elapsedMs"`
	// UserCPUMs 是进程用户态 CPU，包含 GC 与公开计数遥测。
	UserCPUMs float64 `json:"userCpuMs"`
	// SystemCPUMs 是进程内核态 CPU。
	SystemCPUMs float64 `json:"systemCpuMs"`
	// AllocBytes 是阶段 TotalAlloc 增量，不是存活堆或峰值堆。
	AllocBytes uint64 `json:"allocBytes"`
	// Mallocs 是阶段分配次数。
	Mallocs uint64 `json:"mallocs"`
	// GCs 是阶段完成的 GC 次数。
	GCs uint32 `json:"gcs"`
	// HeapBeforeBytes 是阶段开始时的存活堆计量值。
	HeapBeforeBytes uint64 `json:"heapBeforeBytes"`
	// HeapAfterBytes 是阶段结束时的堆计量值，不宣称峰值。
	HeapAfterBytes uint64 `json:"heapAfterBytes"`
}

// abSample 记录一场实际生产路径的有节奏重放。
type abSample struct {
	// Variant 区分由实际旧/新源码编译的测试二进制。
	Variant string `json:"variant"`
	// Scenario 区分纯正文与完成工具后的正文。
	Scenario string `json:"scenario"`
	// Repeat 是外部调度器赋予的重复编号。
	Repeat int `json:"repeat"`
	// InputBodyBytes 是固定新增正文总量。
	InputBodyBytes int `json:"inputBodyBytes"`
	// SourceEvents 是主阶段正文事件数量。
	SourceEvents int `json:"sourceEvents"`
	// SourceIntervalMs 是相同的计划输入间隔。
	SourceIntervalMs int `json:"sourceIntervalMs"`
	// MaxSourceDelayMs 是相对计划时间的最大输入迟到量。
	MaxSourceDelayMs float64 `json:"maxSourceDelayMs"`
	// Warmup 是建立一百个完成工具的真实生产开销，单独列出且不混入后续正文阶段。
	Warmup *abPhase `json:"warmup,omitempty"`
	// Measured 从完成工具后的稳定起点开始，包含正文、Flush 提交和真实终态公开。
	Measured abPhase `json:"measured"`
	// FinalMessageSHA256 是仅去除时间、Duration 与新增 liveRevision 后的完整安全消息指纹。
	FinalMessageSHA256 string `json:"finalMessageSha256"`
	// FinalContentSHA256 独立校验最后正文。
	FinalContentSHA256 string `json:"finalContentSha256"`
	// FinalDisplayParts 是最终稳定部件数。
	FinalDisplayParts int `json:"finalDisplayParts"`
	// FinalCompletedTools 是最终真实完成工具数。
	FinalCompletedTools int `json:"finalCompletedTools"`
}

// abDocument 描述同一环境与同一测量边界。
type abDocument struct {
	// Variant 是实际编译的源码标识。
	Variant string `json:"variant"`
	// GoVersion 是实际执行运行时版本。
	GoVersion string `json:"goVersion"`
	// OS 是真实运行平台。
	OS string `json:"os"`
	// Architecture 是真实运行架构。
	Architecture string `json:"architecture"`
	// CPUs 是宿主可见逻辑 CPU 数。
	CPUs int `json:"cpus"`
	// GOMAXPROCS 是固定的 Go 调度并行度。
	GOMAXPROCS int `json:"gomaxprocs"`
	// Samples 包含实际原始样本，不以模板或推算替代运行。
	Samples []abSample `json:"samples"`
}

func abData(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := eventlog.MarshalData(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func abInteger(t *testing.T, key string, fallback int) int {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 1 {
		t.Fatalf("invalid %s", key)
	}
	return number
}
func abCPU(t *testing.T) syscall.Rusage {
	t.Helper()
	var value syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func abTimevalMS(value syscall.Timeval) float64 {
	return float64(value.Sec)*1000 + float64(value.Usec)/1000
}
func abMeasure(t *testing.T, j *abJournal, run func()) abPhase {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	cpuBefore := abCPU(t)
	started := time.Now()
	run()
	elapsed := time.Since(started)
	cpuAfter := abCPU(t)
	runtime.ReadMemStats(&after)
	return abPhase{Traffic: j.snapshot(t), ElapsedMs: float64(elapsed) / float64(time.Millisecond), UserCPUMs: abTimevalMS(cpuAfter.Utime) - abTimevalMS(cpuBefore.Utime), SystemCPUMs: abTimevalMS(cpuAfter.Stime) - abTimevalMS(cpuBefore.Stime), AllocBytes: after.TotalAlloc - before.TotalAlloc, Mallocs: after.Mallocs - before.Mallocs, GCs: after.NumGC - before.NumGC, HeapBeforeBytes: before.HeapAlloc, HeapAfterBytes: after.HeapAlloc}
}

func abRun(t *testing.T, scenario string, interval, repeat int) abSample {
	t.Helper()
	const chatID = "abababababababababababababababab"
	const executionID = "execution-1"
	const invocationID = "ab-invocation"
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.Create(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: abData(t, eventlog.ChatCreatedData{WakerID: "ab-waker", WorkspaceDirectory: "/workspace", Title: "controlled performance fixture"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: abData(t, eventlog.ExecutionCreatedData{SubmissionID: "ab-submission"})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: executionID, InvocationID: invocationID},
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := eventstream.Replay(initial)
	if err != nil {
		t.Fatal(err)
	}
	adapter.ReleaseHistory()
	journal := &abJournal{store: store, adapter: adapter}
	sink := &runtimeEventSink{journal: journal, chatID: chatID, executionID: executionID}
	accept := func(kind agentapi.EventType, data json.RawMessage) {
		t.Helper()
		if err := sink.acceptInvocation(context.Background(), invocationID, agentapi.Event{Type: kind, Data: data}); err != nil {
			t.Fatal(err)
		}
	}
	var warmup *abPhase
	if scenario == "tools100" {
		phase := abMeasure(t, journal, func() {
			for index := range 100 {
				id := fmt.Sprintf("ab-tool-%03d", index)
				accept(agentapi.EventTypeToolUse, abData(t, map[string]any{"toolCallId": id, "name": "web_search", "input": map[string]any{"query": "fixed search"}}))
				accept(agentapi.EventTypeToolResult, abData(t, map[string]any{"toolCallId": id, "status": "completed"}))
			}
			if err := sink.Flush(); err != nil {
				t.Fatal(err)
			}
		})
		warmup = &phase
	}
	journal.reset()
	chunk := strings.Repeat("x", 512)
	payload := abData(t, map[string]string{"text": chunk})
	maxDelay := time.Duration(0)
	phase := abMeasure(t, journal, func() {
		schedule := time.Now()
		for index := range 100 {
			deadline := schedule.Add(time.Duration(index*interval) * time.Millisecond)
			if until := time.Until(deadline); until > 0 {
				time.Sleep(until)
			}
			if late := time.Since(deadline); late > maxDelay {
				maxDelay = late
			}
			accept(agentapi.EventTypeTextDelta, payload)
		}
		if until := time.Until(schedule.Add(time.Duration(100*interval) * time.Millisecond)); until > 0 {
			time.Sleep(until)
		}
		if err := sink.Flush(); err != nil {
			t.Fatal(err)
		}
		// 不能把两次真实持久公开快照缩减为一次：Flush 的 assistant_message 与执行终态均经过 Adapter。
		if _, err := journal.AppendExecutionFacts(context.Background(), chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: executionID, Data: abData(t, eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted, MessageRevision: sink.MessageRevision(), Usage: sink.Usage()})}}); err != nil {
			t.Fatal(err)
		}
	})
	message := journal.lastMessage
	if message == nil || message.Content != strings.Repeat(chunk, 100) || message.Status != constants.ExecutionStatusCompleted {
		t.Fatal("production path did not preserve complete final message")
	}
	tools := 0
	for _, part := range message.DisplayParts {
		if part.Type == constants.MessageDisplayPartTool && part.ToolStatus == constants.MessageToolStatusCompleted {
			tools++
		}
	}
	if scenario == "tools100" && tools != 100 {
		t.Fatalf("completed tools=%d, want100", tools)
	}
	canonical, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]any
	if err = json.Unmarshal(canonical, &normalized); err != nil {
		t.Fatal(err)
	}
	delete(normalized, "createdAt")
	delete(normalized, "updatedAt")
	delete(normalized, "liveRevision")
	if metrics, ok := normalized["runMetrics"].(map[string]any); ok {
		delete(metrics, "startedAt")
		delete(metrics, "durationMs")
	}
	canonical, err = json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("NEUX_CHAT_AB_CANONICAL_OUTPUT"); path != "" {
		if err := os.WriteFile(path, canonical, 0600); err != nil {
			t.Fatal(err)
		}
	}
	messageHash, contentHash := sha256.Sum256(canonical), sha256.Sum256([]byte(message.Content))
	return abSample{Variant: abVariant, Scenario: scenario, Repeat: repeat, InputBodyBytes: len(message.Content), SourceEvents: 100, SourceIntervalMs: interval, MaxSourceDelayMs: float64(maxDelay) / float64(time.Millisecond), Warmup: warmup, Measured: phase, FinalMessageSHA256: hex.EncodeToString(messageHash[:]), FinalContentSHA256: hex.EncodeToString(contentHash[:]), FinalDisplayParts: len(message.DisplayParts), FinalCompletedTools: tools}
}

// TestBackendPerformanceAB 仅由显式性能脚本选择；普通定向测试不会启动节奏重放。
func TestBackendPerformanceAB(t *testing.T) {
	if os.Getenv("NEUX_CHAT_AB_ENABLE") != "1" {
		t.Skip("explicit backend A/B measurement only")
	}
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	interval, repeat := abInteger(t, "NEUX_CHAT_AB_INTERVAL_MS", 20), abInteger(t, "NEUX_CHAT_AB_REPEAT", 1)
	scenario := os.Getenv("NEUX_CHAT_AB_SCENARIO")
	if scenario != "plain" && scenario != "tools100" {
		t.Fatal("scenario must be plain or tools100")
	}
	document := abDocument{Variant: abVariant, GoVersion: runtime.Version(), OS: runtime.GOOS, Architecture: runtime.GOARCH, CPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), Samples: []abSample{abRun(t, scenario, interval, repeat)}}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("NEUX_CHAT_AB_OUTPUT"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("backend AB %s %s: %+v", abVariant, scenario, document.Samples[0].Measured)
}
