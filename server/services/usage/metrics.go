package usage

import (
	ledger "github.com/JieWaZi/NeuxBot/server/internal/usage"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

type accumulator struct {
	// tokens aggregates the counters from native invocations.
	tokens ledger.TokenAccumulator
	// unknownAttribution marks an unverified invocation source or account.
	unknownAttribution bool
}

func (a *accumulator) add(row models.UsageRecord) error {
	if row.Source == "" || row.AccountID == "" {
		a.unknownAttribution = true
	}
	return a.tokens.Add(ledger.Tokens{Input: row.Input, ReportedInput: row.ReportedInput, CacheRead: row.CacheRead, CacheWrite: row.CacheWrite,
		Output: row.Output, Reasoning: row.Reasoning}, row.Complete)
}

func finishSummary(filter Filter, token string, snapshot snapshot, totals *accumulator) (Summary, error) {
	metrics, err := totals.tokens.Metrics()
	if err != nil {
		return Summary{}, err
	}
	coverage := Coverage{State: "partial", EarliestRetainedAt: snapshot.EarliestAt, LegacyBackgroundUnknown: true,
		Reasons: []string{"legacy_usage_unavailable"}}
	if snapshot.ProjectionPartial {
		coverage.Reasons = append(coverage.Reasons, "native_projection_incomplete")
	}
	if totals.unknownAttribution {
		coverage.Reasons = append(coverage.Reasons, "attribution_unknown")
	}
	if metrics.MissingUsageInvocations > 0 {
		coverage.Reasons = append(coverage.Reasons, "missing_tokens")
	}
	if metrics.IncompleteUsageInvocations > 0 {
		coverage.Reasons = append(coverage.Reasons, "incomplete_tokens")
	}
	return Summary{Range: filter, Snapshot: token, Coverage: coverage, Metrics: metrics}, nil
}
