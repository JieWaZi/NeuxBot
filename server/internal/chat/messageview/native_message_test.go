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

func TestRunDurationIncludesRequestPreparationAfterReplay(t *testing.T) {
	for _, data := range []string{`{"requestedAt":100}`, `{}`, `{"requestedAt":600}`} {
		builder := NewPublicMessageViewBuilder()
		for _, record := range []eventlog.Record{
			{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "turn", Sequence: 1, RecordedAt: 500, Data: json.RawMessage(data)},
			{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "turn", Sequence: 2, RecordedAt: 500},
			{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "turn", Sequence: 3, RecordedAt: 900, Data: json.RawMessage(`{"status":"completed"}`)},
		} {
			if _, err := builder.Apply(record); err != nil {
				t.Fatal(err)
			}
		}
		metrics := builder.PublicMessage("turn").RunMetrics
		wantStart, wantDuration := int64(500), int64(400)
		if data == `{"requestedAt":100}` {
			wantStart, wantDuration = 100, 800
		}
		if metrics == nil || metrics.StartedAt != wantStart || metrics.DurationMs != wantDuration {
			t.Fatalf("data=%s metrics=%+v, want start=%d duration=%d", data, metrics, wantStart, wantDuration)
		}
	}
}

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

func TestNativeReportedInputIsVisibleWithoutInventingUncachedInput(t *testing.T) {
	builder := NewPublicMessageViewBuilder()
	builder.SeedNativeLiveMessage("reported-input", 1)
	input, output := int64(1200), int64(20)
	builder.SetNativeResult("reported-input", &agent.NativeResult{
		Key: agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "qoder", Subject: "s", SessionID: "n"}, InvocationID: "p"},
		Turn: acpmeta.NativeTurn{Usage: &acpmeta.TurnUsage{Source: "qoder.history.usage", OutputTokens: &output,
			Observations: []acpmeta.UsageObservation{{Source: "qoder.history.usage", Coverage: "observed_requests_raw", InputTotalTokens: &input}}}},
	})
	raw, err := json.Marshal(builder.PublicMessage("reported-input").RunMetrics.TokenUsage.KnownTokens)
	if err != nil {
		t.Fatal(err)
	}
	var known map[string]*int64
	if err := json.Unmarshal(raw, &known); err != nil {
		t.Fatal(err)
	}
	if known["reportedInput"] == nil || *known["reportedInput"] != 1200 || known["input"] != nil {
		t.Fatalf("reported input missing or invented as uncached input: %s", raw)
	}
}

func TestNativeResultShowsQoderSubtotalAndWorkBuddySampleSeparately(t *testing.T) {
	const id = "execution"
	builder := NewPublicMessageViewBuilder()
	builder.SeedNativeLiveMessage(id, 1)
	output := int64(31)
	result := &agent.NativeResult{
		Key:  agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "qoder", Subject: "s", SessionID: "n"}, InvocationID: "p"},
		Turn: acpmeta.NativeTurn{Usage: &acpmeta.TurnUsage{Coverage: "query_pipeline", OutputTokens: &output}},
	}
	builder.SetNativeResult(id, result)
	metrics := builder.PublicMessage(id).RunMetrics
	if metrics == nil || metrics.TokenUsage == nil || metrics.TokenUsage.KnownTokens.Output == nil ||
		*metrics.TokenUsage.KnownTokens.Output != output || metrics.TokenUsage.Complete || metrics.OutputTokens != 0 {
		t.Fatalf("Qoder should expose the known subtotal: %+v", metrics)
	}
	result.Key.Session.Provider = "workbuddy"
	result.Turn.Usage = &acpmeta.TurnUsage{Observations: []acpmeta.UsageObservation{{
		Source: "codebuddy/usage_update", Coverage: "latest_model_request", Tokens: acpmeta.ModelUsage{OutputTokens: &output},
	}}}
	builder.SetNativeResult(id, result)
	metrics = builder.PublicMessage(id).RunMetrics
	if metrics == nil || metrics.LatestRequestOutputTokens == nil || *metrics.LatestRequestOutputTokens != output ||
		metrics.TokenUsage != nil || metrics.OutputTokens != 0 {
		t.Fatalf("WorkBuddy should expose the sample separately: %+v", metrics)
	}
}

func TestNativeResultPreservesExplicitFailureSource(t *testing.T) {
	result := &agent.NativeResult{Failure: &acpmeta.NativeTurnFailure{Message: "Native quota exhausted"}}
	for _, source := range []constants.ErrorMessageSource{constants.ErrorMessageSourceUnspecified, constants.ErrorMessageSourceLocalized, constants.ErrorMessageSourcePublic} {
		builder := NewPublicMessageViewBuilder()
		for _, record := range []eventlog.Record{
			{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution", Sequence: 1, Data: json.RawMessage(`{}`)},
			{Type: constants.ChatEventTypeExecutionFailed, ExecutionID: "execution", Sequence: 2, Data: json.RawMessage(`{"status":"failed","errorCode":"RuntimeQuotaExhausted","errorMessage":"Native quota exhausted","errorMessageSource":"` + string(source) + `"}`)},
		} {
			if _, err := builder.Apply(record); err != nil {
				t.Fatal(err)
			}
		}
		builder.SetNativeResult("execution", result)
		message := builder.PublicMessage("execution")
		if message == nil || message.ErrorMessageSource != source || message.ErrorMessage != "Native quota exhausted" {
			t.Fatalf("source=%q projection=%+v", source, message)
		}
	}
}
