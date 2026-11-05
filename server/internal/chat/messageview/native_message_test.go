package messageview

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

func TestNativeUsagePublicMetricsRequireCompleteCoverage(t *testing.T) {
	const id = "execution"
	builder := NewPublicMessageViewBuilder()
	records := []eventlog.Record{
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: id, Sequence: 1, RecordedAt: 90, Data: json.RawMessage(`{}`)},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: id, Sequence: 2, RecordedAt: 100},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: id, Sequence: 3, RecordedAt: 250, Data: json.RawMessage(`{"status":"completed"}`)},
	}
	for _, record := range records {
		if _, err := builder.Apply(record); err != nil {
			t.Fatal(err)
		}
	}
	input, output, cache, knownZero := int64(81), int64(17), int64(8), int64(0)
	partial := &acpmeta.TurnUsage{Coverage: "last_request", InputTokens: &input, OutputTokens: &output}
	complete := &acpmeta.TurnUsage{Coverage: "invocation", Complete: true, InputTokens: &input, CacheReadTokens: &cache, ReasoningTokens: &knownZero}
	wantTime := map[string]int64{"startedAt": 100, "durationMs": 150}
	check := func(usage *acpmeta.TurnUsage, want map[string]int64) {
		t.Helper()
		builder.SetNativeUsage(id, usage)
		raw, err := json.Marshal(builder.PublicMessage(id))
		if err != nil {
			t.Fatal(err)
		}
		var public struct {
			// RunMetrics is the existing public whole-turn projection.
			RunMetrics map[string]int64 `json:"runMetrics"`
		}
		if err := json.Unmarshal(raw, &public); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(public.RunMetrics, want) {
			t.Fatalf("serialized metrics = %v, want %v", public.RunMetrics, want)
		}
	}
	check(partial, wantTime)
	check(complete, map[string]int64{"startedAt": 100, "durationMs": 150, "inputTokens": 81, "cacheReadTokens": 8})
	check(partial, wantTime)
	check(complete, map[string]int64{"startedAt": 100, "durationMs": 150, "inputTokens": 81, "cacheReadTokens": 8})
	check(nil, wantTime)
}

func TestNativePartialUsageIncludesKnownSubtotal(t *testing.T) {
	const id = "execution"
	builder := NewPublicMessageViewBuilder()
	_, err := builder.Apply(eventlog.Record{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: id,
		Sequence: 1, RecordedAt: 90, Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	input, output, reasoning := int64(5), int64(3), int64(1)
	result := &agent.NativeResult{Key: agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "s", SessionID: "n"}, InvocationID: "p"},
		Turn: acpmeta.NativeTurn{Usage: &acpmeta.TurnUsage{Coverage: "last_request", InputTokens: &input, OutputTokens: &output, ReasoningTokens: &reasoning}}}
	builder.SetNativeResult(id, result)
	usage := builder.PublicMessage(id).RunMetrics.TokenUsage
	if usage == nil || usage.InvocationID == "" || usage.Complete || usage.TotalWithCacheRead != nil ||
		usage.KnownTotalWithCacheRead == nil || *usage.KnownTotalWithCacheRead != 8 || usage.IncompleteUsageInvocations != 1 {
		t.Fatalf("unexpected public partial metrics: %+v", usage)
	}
}
