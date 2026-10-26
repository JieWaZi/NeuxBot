package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/acp-go/pkg/nativeacp"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestACPNativePressureProcess 是只通过 stdio 发出固定通知的本机测试子进程，不启动真实 CLI 或模型。
func TestACPNativePressureProcess(t *testing.T) {
	if os.Getenv("NEUX_TEST_NATIVE_PRESSURE") != "1" {
		return
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var request struct {
			// ID 关联原始 JSON-RPC 请求，通知时为空。
			ID json.RawMessage `json:"id"`
			// Method 是模拟的标准 ACP 方法。
			Method string `json:"method"`
		}
		if decoder.Decode(&request) != nil {
			os.Exit(0)
		}
		result := any(map[string]any{})
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"mcpCapabilities": map[string]any{"http": true}, "sessionCapabilities": map[string]any{"resume": map[string]any{}, "close": map[string]any{}}}, "agentInfo": map[string]any{"name": "stdio-fixture", "version": "1"}}
		case "session/new":
			result = map[string]any{"sessionId": "native-session", "modes": map[string]any{"currentModeId": "auto", "availableModes": []any{map[string]any{"id": "auto", "name": "Auto"}}}}
		case "session/prompt":
			for i := 0; i < 2200; i++ {
				if encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "native-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": fmt.Sprintf("chunk-%04d;", i)}}}}) != nil {
					os.Exit(0)
				}
			}
			result = map[string]any{"stopReason": "end_turn"}
		}
		if len(request.ID) > 0 {
			if encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}) != nil {
				os.Exit(0)
			}
		}
	}
}

// nativeOrderedTextSink 验证原生通知正文在多层背压中逐项保序，不仅统计条数。
type nativeOrderedTextSink struct {
	// burstPauseSink 提供可控首条背压和已完成交付计数。
	burstPauseSink
}

func (s *nativeOrderedTextSink) Accept(ctx context.Context, event Event) error {
	if event.Type == EventTypeTextDelta {
		var payload struct {
			// Text 是该通知的规范正文。
			Text string `json:"text"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		if payload.Text != fmt.Sprintf("chunk-%04d;", s.count) {
			return fmt.Errorf("native output order/content mismatch at %d: %q", s.count, payload.Text)
		}
	}
	return s.burstPauseSink.Accept(ctx, event)
}

// nativePressureBackend 把实际 nativeacp 和两层 SDK 接到测试 stdio 进程。
func nativePressureBackend(t *testing.T) (*acpBackend, *atomic.Int64) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	backend := newACPBackend()
	calls := &atomic.Int64{}
	replaceACPAgentFactory(backend, RuntimeIDWorkBuddy, func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		calls.Add(1)
		return nativeacp.NewAgent(ctx, nativeacp.Config{Command: binary, Args: []string{"-test.run=^TestACPNativePressureProcess$"}, Environment: append(os.Environ(), "NEUX_TEST_NATIVE_PRESSURE=1"), WorkingDirectory: config.WorkingDirectory, Logger: acpAdapterLogger(ctx)})
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	return backend, calls
}
func nativePressureRequest(t *testing.T, id string, sink EventSink) acpRunRequest {
	t.Helper()
	req := testCodexACPRunRequest(t, id)
	req.Prepared.definition.id = RuntimeIDWorkBuddy
	req.Prepared.definition.baseRuntimeID = RuntimeIDWorkBuddy
	req.Request.Target.RuntimeID = RuntimeIDWorkBuddy
	req.Prepared.defaults.EventDelivery = time.Second
	req.Sink = sink
	return req
}

func TestACPNativeStdioBackpressuresCompleteBurst(t *testing.T) {
	backend, _ := nativePressureBackend(t)
	sink := &nativeOrderedTextSink{burstPauseSink: burstPauseSink{delay: 200 * time.Millisecond}}
	req := nativePressureRequest(t, "stdio-burst", sink)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	begin := time.Now()
	result := backend.Run(ctx, req)
	if result.Failure != nil || sink.count != 2200 {
		t.Fatalf("native stdio burst incomplete: delivered=%d/2200 failure=%+v duration=%v", sink.count, result.Failure, time.Since(begin))
	}
	t.Logf("native stdio delivered=%d duration=%v", sink.count, time.Since(begin))
}

func TestACPNativeStdioCancellationKeepsOtherInvocationUsable(t *testing.T) {
	backend, calls := nativePressureBackend(t)
	blocked := &blockedRuntimeTextSink{entered: make(chan struct{})}
	slow := nativePressureRequest(t, "stdio-slow", blocked)
	healthySink := &nativeOrderedTextSink{}
	healthy := nativePressureRequest(t, "stdio-healthy", healthySink)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	slowCtx, cancelSlow := context.WithCancel(ctx)
	defer cancelSlow()
	slowDone := make(chan Result, 1)
	healthyDone := make(chan Result, 1)
	go func() { slowDone <- backend.Run(slowCtx, slow) }()
	select {
	case <-blocked.entered:
	case result := <-slowDone:
		t.Fatalf("slow invocation did not enter text delivery: %+v", result.Failure)
	case <-ctx.Done():
		t.Fatal("slow invocation did not start")
	}
	go func() { healthyDone <- backend.Run(ctx, healthy) }()
	cancelSlow()
	slowResult, healthyResult := <-slowDone, <-healthyDone
	if slowResult.Failure == nil || slowResult.Failure.Code != FailureCodeContextCanceled {
		t.Fatalf("canceled invocation lost its primary cause: %+v", slowResult.Failure)
	}
	if healthyResult.Failure != nil || healthySink.count != 2200 || calls.Load() != 2 {
		t.Fatalf("independent native invocation failed: result=%+v delivered=%d factories=%d", healthyResult.Failure, healthySink.count, calls.Load())
	}
}
