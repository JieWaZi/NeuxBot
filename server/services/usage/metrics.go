package usage

import (
	ledger "github.com/JieWaZi/NeuxBot/server/internal/usage"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

type accumulator struct {
	// tokens 汇总原生调用的用量计数。
	tokens ledger.TokenAccumulator
	// unknownAttribution 表示存在来源或账号归属未知的调用。
	unknownAttribution bool
	// cacheRead 汇总输入、读取与写入均有计数的观测中的缓存读取量。
	cacheRead float64
	// cacheableInput 汇总同一批观测中的普通输入、缓存写入和缓存读取。
	cacheableInput float64
}

func (a *accumulator) add(row models.UsageRecord) error {
	if row.Source == "" || row.AccountID == "" {
		a.unknownAttribution = true
	}
	if err := a.tokens.Add(ledger.Tokens{Input: row.Input, ReportedInput: row.ReportedInput, CacheRead: row.CacheRead, CacheWrite: row.CacheWrite,
		Output: row.Output, Reasoning: row.Reasoning}, row.Complete); err != nil {
		return err
	}
	if row.Input != nil && row.CacheRead != nil && row.CacheWrite != nil {
		read := float64(*row.CacheRead)
		a.cacheRead += read
		a.cacheableInput += float64(*row.Input) + float64(*row.CacheWrite) + read
	}
	return nil
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
	summary := Summary{Range: filter, Snapshot: token, Coverage: coverage, Metrics: metrics}
	if totals.cacheableInput > 0 {
		rate := totals.cacheRead / totals.cacheableInput
		summary.ObservedCacheHitRate = &rate
	}
	return summary, nil
}
