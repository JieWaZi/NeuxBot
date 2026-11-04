package usage

import (
	"testing"

	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

func TestNativeAmountPreservesClaudeQueryPipelineBasis(t *testing.T) {
	cost := acpmeta.UsageCost{Amount: "0.001", Currency: "USD", Source: "claude.result.cost", Scope: "query_pipeline",
		Aggregation: "turn_delta", Valuation: "estimated"}
	if amount, complete := NativeAmount("claude", &cost); amount != "0.001" || !complete {
		t.Fatalf("valid Claude estimate = %q, complete=%t", amount, complete)
	}
	if amount, _ := NativeAmount("cursor", &cost); amount != "" {
		t.Fatalf("unrelated provider acquired Claude pricing basis: %q", amount)
	}
	cost.Valuation = "reported"
	if amount, _ := NativeAmount("claude", &cost); amount != "" {
		t.Fatalf("list-price estimate accepted as a reported charge: %q", amount)
	}
}

func TestCodexPriceRequiresVerifiedRequestEvidence(t *testing.T) {
	input, cache, output, write, upper := int64(7), int64(3), int64(11), int64(0), int64(10)
	tokens := Tokens{Input: &input, CacheRead: &cache, CacheWrite: &write, Output: &output}
	proof := CodexPriceProof{Publisher: "openai", Model: "gpt-5.3-codex", Tier: "standard",
		Version: Codex53StandardPriceVersion, RequestInputUpperBound: &upper}
	got, ok := QuoteCodexAPI(tokens, proof)
	if !ok || got != "0.000166775" {
		t.Fatalf("exact published Standard rate = %s, %v", got, ok)
	}
	for _, mutate := range []func(*CodexPriceProof){
		func(p *CodexPriceProof) { p.Publisher = "" },
		func(p *CodexPriceProof) { p.Model = "gpt-6.1-sol" },
		func(p *CodexPriceProof) { p.Tier = "" },
		func(p *CodexPriceProof) { p.Tier = "priority" },
		func(p *CodexPriceProof) { p.Version = "" },
		func(p *CodexPriceProof) { p.RequestInputUpperBound = nil },
		func(p *CodexPriceProof) { long := int64(300_000); p.RequestInputUpperBound = &long },
	} {
		changed := proof
		mutate(&changed)
		if got, ok := QuoteCodexAPI(tokens, changed); ok {
			t.Fatalf("unproven price was produced: %s", got)
		}
	}
}
