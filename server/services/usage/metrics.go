package usage

import (
	"math/big"
	"slices"

	ledger "github.com/JieWaZi/NeuxBot/server/internal/usage"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Amount is a known subtotal in one currency, valuation kind, and source.
type Amount struct {
	// Amount is an exact decimal string.
	Amount string `json:"amount"`
	// Currency is the original currency.
	Currency string `json:"currency"`
	// Valuation distinguishes reported amounts from estimates.
	Valuation string `json:"valuation"`
	// Origin distinguishes native valuations from host API-equivalent estimates.
	Origin string `json:"origin"`
	// Source identifies the valuation evidence.
	Source string `json:"source"`
	// PriceVersions lists the rates used in this subtotal.
	PriceVersions []string `json:"priceVersions"`
	// Invocations counts the calls included in the amount.
	Invocations int64 `json:"invocations"`
	// IncompleteInvocations counts the partially priced calls in this amount.
	IncompleteInvocations int64 `json:"incompleteInvocations"`
}

// Metrics reports token and cost coverage independently.
type Metrics struct {
	// TokenMetrics contains complete counters and known subtotals.
	ledger.TokenMetrics
	// Amounts separates currencies and valuation sources.
	Amounts []Amount `json:"amounts"`
	// ComparisonAmounts contains estimates excluded from the main subtotal.
	ComparisonAmounts []Amount `json:"comparisonAmounts"`
	// UnpricedInvocations counts calls without an attributable amount.
	UnpricedInvocations int64 `json:"unpricedInvocations"`
	// IncompleteCostInvocations counts calls with a partial amount.
	IncompleteCostInvocations int64 `json:"incompleteCostInvocations"`
}

type amountKey struct {
	// currency is the amount currency.
	currency string
	// valuation identifies the valuation kind.
	valuation string
	// origin identifies the valuation origin.
	origin string
	// source identifies the original valuation evidence.
	source string
}

type amountSum struct {
	// total keeps the exact amount.
	total big.Rat
	// digits is the decimal scale of the subtotal.
	digits int
	// versions lists rate versions in the subtotal.
	versions map[string]bool
	// invocations counts the calls contributing to the subtotal.
	invocations int64
	// incomplete counts the calls with partial price coverage.
	incomplete int64
}

type accumulator struct {
	// tokens accumulates canonical token usage.
	tokens ledger.TokenAccumulator
	// amounts separates currencies and valuation sources.
	amounts map[amountKey]*amountSum
	// unpriced counts calls without usable valuation evidence.
	unpriced int64
	// incompleteCost counts calls with partial price coverage.
	incompleteCost int64
	// unknownAttribution reports an unverified entry point or account.
	unknownAttribution bool
}

func newAccumulator() *accumulator { return &accumulator{amounts: make(map[amountKey]*amountSum)} }

func (a *accumulator) add(row models.UsageRecord) error {
	if err := a.tokens.Add(ledger.Tokens{Input: row.Input, CacheRead: row.CacheRead, CacheWrite: row.CacheWrite,
		Output: row.Output, Reasoning: row.Reasoning}, row.Complete); err != nil {
		return err
	}
	if row.Source == "" || row.AccountID == "" {
		a.unknownAttribution = true
	}
	value, digits, valid := ledger.ParseAmount(row.Amount)
	if !valid {
		a.unpriced++
		return nil
	}
	key := amountKey{row.Currency, row.Valuation, row.CostOrigin, row.CostSource}
	sum := a.amounts[key]
	if sum == nil {
		sum = &amountSum{versions: make(map[string]bool)}
		a.amounts[key] = sum
	}
	sum.total.Add(&sum.total, value)
	sum.digits = max(sum.digits, digits)
	sum.invocations++
	if !row.CostComplete {
		sum.incomplete++
		a.incompleteCost++
	}
	if row.PriceVersion != "" {
		sum.versions[row.PriceVersion] = true
	}
	return nil
}

func (a *accumulator) metrics() (Metrics, error) {
	tokens, err := a.tokens.Metrics()
	if err != nil {
		return Metrics{}, err
	}
	metrics := Metrics{TokenMetrics: tokens, Amounts: []Amount{}, ComparisonAmounts: []Amount{},
		UnpricedInvocations: a.unpriced, IncompleteCostInvocations: a.incompleteCost}
	for key, sum := range a.amounts {
		versions := []string{}
		for version := range sum.versions {
			versions = append(versions, version)
		}
		slices.Sort(versions)
		metrics.Amounts = append(metrics.Amounts, Amount{Amount: sum.total.FloatString(sum.digits), Currency: key.currency,
			Valuation: key.valuation, Origin: key.origin, Source: key.source, PriceVersions: versions,
			Invocations: sum.invocations, IncompleteInvocations: sum.incomplete})
	}
	slices.SortFunc(metrics.Amounts, func(a, b Amount) int {
		return slices.Compare([]string{a.Currency, a.Valuation, a.Origin, a.Source}, []string{b.Currency, b.Valuation, b.Origin, b.Source})
	})
	return metrics, nil
}

func finishSummary(filter Filter, token string, snapshot snapshot, totals *accumulator) (Summary, error) {
	metrics, err := totals.metrics()
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
	if metrics.UnpricedInvocations > 0 || metrics.IncompleteCostInvocations > 0 {
		coverage.Reasons = append(coverage.Reasons, "incomplete_cost")
	}
	return Summary{Range: filter, Snapshot: token, Coverage: coverage, Metrics: metrics}, nil
}
