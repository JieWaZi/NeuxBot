package usage

import (
	"encoding/json"
	"testing"

	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

func TestReportedInputRemainsSeparateFromAdditiveCounters(t *testing.T) {
	input, output, cache := int64(1200), int64(20), int64(500)
	tokens := ReadTokens(&acpmeta.TurnUsage{Source: "qoder.history.usage", OutputTokens: &output, CacheReadTokens: &cache,
		Observations: []acpmeta.UsageObservation{{Source: "qoder.history.usage", Coverage: "observed_requests_raw", InputTotalTokens: &input}}})
	var totals TokenAccumulator
	if err := totals.Add(tokens, false); err != nil {
		t.Fatal(err)
	}
	metrics, err := totals.Metrics()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(metrics.KnownTokens)
	if err != nil {
		t.Fatal(err)
	}
	var known map[string]*int64
	if err := json.Unmarshal(raw, &known); err != nil {
		t.Fatal(err)
	}
	if known["reportedInput"] == nil || *known["reportedInput"] != 1200 || known["input"] != nil ||
		metrics.KnownTotalWithCacheRead == nil || *metrics.KnownTotalWithCacheRead != 520 ||
		metrics.TotalWithCacheRead != nil || metrics.IncompleteUsageInvocations != 1 {
		t.Fatalf("reported input was hidden or mixed into additive counters: known=%s metrics=%+v", raw, metrics)
	}
}

func TestReportedInputRejectsConflictingAndUnrelatedObservations(t *testing.T) {
	one, two := int64(1), int64(2)
	for _, observations := range [][]acpmeta.UsageObservation{
		{{Source: "other", Coverage: "observed_requests_raw", InputTotalTokens: &one}},
		{{Source: "native", Coverage: "observed_requests_raw", InputTotalTokens: &one}, {Source: "native", Coverage: "observed_requests_raw", InputTotalTokens: &two}},
		{{Source: "native", Coverage: "latest_model_request", InputTotalTokens: &one}},
		{{Source: "native", Coverage: "unknown", InputTotalTokens: &one}},
		{{Source: "native", Coverage: "session_cumulative", InputTotalTokens: &one}},
	} {
		raw, err := json.Marshal(ReadTokens(&acpmeta.TurnUsage{Source: "native", Observations: observations}))
		if err != nil {
			t.Fatal(err)
		}
		var known map[string]*int64
		if err := json.Unmarshal(raw, &known); err != nil {
			t.Fatal(err)
		}
		if known["reportedInput"] != nil {
			t.Fatalf("unrelated or conflicting input promoted: %s", raw)
		}
	}
}

func TestReportedInputKeepsMeasuredZeroAndPrefersUncachedInput(t *testing.T) {
	zero := int64(0)
	native := &acpmeta.TurnUsage{Source: "native", Observations: []acpmeta.UsageObservation{
		{Source: "native", Coverage: "observed_requests_raw", InputTotalTokens: &zero},
	}}
	tokens := ReadTokens(native)
	if tokens.ReportedInput == nil || *tokens.ReportedInput != 0 {
		t.Fatal("measured native zero was lost")
	}
	var totals TokenAccumulator
	if err := totals.Add(tokens, false); err != nil {
		t.Fatal(err)
	}
	metrics, err := totals.Metrics()
	if err != nil || metrics.MissingUsageInvocations != 0 || metrics.IncompleteUsageInvocations != 1 || metrics.TotalWithCacheRead != nil {
		t.Fatalf("native-only evidence was lost or completed: %+v %v", metrics, err)
	}
	native.InputTokens = &zero
	if ReadTokens(native).ReportedInput != nil {
		t.Fatal("native input duplicated the known uncached input")
	}
}
