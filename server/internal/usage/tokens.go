// Package usage aggregates canonical token evidence.
package usage

import (
	"fmt"
	"math"

	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// Tokens 保存已规范化计数和原生输入；ReportedInput 不参与互斥计数求和。
type Tokens struct {
	// 普通输入
	Input *int64 `json:"input"`
	// 原生输入
	ReportedInput *int64 `json:"reportedInput,omitempty"`
	// 缓存读取
	CacheRead *int64 `json:"cacheRead"`
	// 缓存写入
	CacheWrite *int64 `json:"cacheWrite"`
	// 输出用量
	Output *int64 `json:"output"`
	// 推理用量
	Reasoning *int64 `json:"reasoning"`
}

// ReadTokens 读取规范计数，并仅保留与主观测来源一致的原生输入。
func ReadTokens(snapshot *acpmeta.TurnUsage) Tokens {
	if snapshot == nil {
		return Tokens{}
	}
	tokens := ReadModelTokens(acpmeta.ModelUsage{
		InputTokens: snapshot.InputTokens, CacheReadTokens: snapshot.CacheReadTokens, CacheWriteTokens: snapshot.CacheWriteTokens,
		OutputTokens: snapshot.OutputTokens, ReasoningTokens: snapshot.ReasoningTokens,
	})
	if tokens.Input != nil || snapshot.Source == "" {
		return tokens
	}
	var reported *int64
	for _, observation := range snapshot.Observations {
		if observation.Source != snapshot.Source || observation.InputTotalTokens == nil {
			continue
		}
		switch observation.Coverage {
		case "query_pipeline_raw", "observed_requests_raw", "observed_requests", "provider_input_total":
		default:
			continue
		}
		if *observation.InputTotalTokens < 0 || reported != nil && *reported != *observation.InputTotalTokens {
			return tokens
		}
		reported = observation.InputTotalTokens
	}
	tokens.ReportedInput = reported
	return tokens
}

// ReadModelTokens validates the same canonical counters for a model's share of the invocation.
func ReadModelTokens(snapshot acpmeta.ModelUsage) Tokens {
	valid := func(value *int64) *int64 {
		if value == nil || *value < 0 {
			return nil
		}
		return value
	}
	t := Tokens{Input: valid(snapshot.InputTokens), CacheRead: valid(snapshot.CacheReadTokens), CacheWrite: valid(snapshot.CacheWriteTokens),
		Output: valid(snapshot.OutputTokens), Reasoning: valid(snapshot.ReasoningTokens)}
	if t.Output != nil && t.Reasoning != nil && *t.Reasoning > *t.Output {
		t.Reasoning = nil
	}
	return t
}

// TokenMetrics keeps complete counts and known subtotals separately.
type TokenMetrics struct {
	// Tokens contains the counters proven complete across the range.
	Tokens Tokens `json:"tokens"`
	// KnownTokens contains subtotals of available counters.
	KnownTokens Tokens `json:"knownTokens"`
	// TotalWithCacheRead is the complete total including cache reads.
	TotalWithCacheRead *int64 `json:"totalWithCacheRead"`
	// TotalWithoutCacheRead is the complete total excluding cache reads.
	TotalWithoutCacheRead *int64 `json:"totalWithoutCacheRead"`
	// KnownTotalWithCacheRead is the known subtotal including cache reads.
	KnownTotalWithCacheRead *int64 `json:"knownTotalWithCacheRead"`
	// KnownTotalWithoutCacheRead is the known subtotal excluding cache reads.
	KnownTotalWithoutCacheRead *int64 `json:"knownTotalWithoutCacheRead"`
	// Invocations counts canonical native invocations.
	Invocations int64 `json:"invocations"`
	// MissingUsageInvocations counts invocations without token evidence.
	MissingUsageInvocations int64 `json:"missingUsageInvocations"`
	// IncompleteUsageInvocations counts observations with incomplete scope or additive counters.
	IncompleteUsageInvocations int64 `json:"incompleteUsageInvocations"`
}

// TokenAccumulator combines disjoint primary observations. Callers deduplicate by canonical identity.
type TokenAccumulator struct {
	// metrics holds the running complete counts and known subtotals.
	metrics TokenMetrics
}

func (a *TokenAccumulator) Add(t Tokens, complete bool) error {
	m := &a.metrics
	m.Invocations++
	if t == (Tokens{}) {
		m.MissingUsageInvocations++
	} else if !complete || t.Input == nil || t.CacheRead == nil || t.CacheWrite == nil || t.Output == nil {
		m.IncompleteUsageInvocations++
	}
	pairs := []struct {
		// value is the counter from this invocation.
		value *int64
		// known points to the known subtotal.
		known **int64
		// full points to the complete count.
		full **int64
	}{
		{t.Input, &m.KnownTokens.Input, &m.Tokens.Input},
		{t.ReportedInput, &m.KnownTokens.ReportedInput, &m.Tokens.ReportedInput},
		{t.CacheRead, &m.KnownTokens.CacheRead, &m.Tokens.CacheRead},
		{t.CacheWrite, &m.KnownTokens.CacheWrite, &m.Tokens.CacheWrite},
		{t.Output, &m.KnownTokens.Output, &m.Tokens.Output},
		{t.Reasoning, &m.KnownTokens.Reasoning, &m.Tokens.Reasoning},
	}
	for _, pair := range pairs {
		var err error
		*pair.known, err = sumKnown(*pair.known, pair.value)
		if err != nil {
			return err
		}
		switch {
		case !complete || pair.value == nil:
			*pair.full = nil
		case m.Invocations == 1:
			*pair.full = pair.value
		case *pair.full != nil:
			*pair.full = *pair.known
		}
	}
	return nil
}

func (a *TokenAccumulator) Metrics() (TokenMetrics, error) {
	m := a.metrics
	var err error
	m.KnownTotalWithoutCacheRead, err = sumKnown(m.KnownTokens.Input, m.KnownTokens.CacheWrite, m.KnownTokens.Output)
	if err != nil {
		return m, err
	}
	m.KnownTotalWithCacheRead, err = sumKnown(m.KnownTotalWithoutCacheRead, m.KnownTokens.CacheRead)
	if err != nil {
		return m, err
	}
	if m.Tokens.Input != nil && m.Tokens.CacheWrite != nil && m.Tokens.Output != nil {
		m.TotalWithoutCacheRead = m.KnownTotalWithoutCacheRead
		if m.Tokens.CacheRead != nil {
			m.TotalWithCacheRead = m.KnownTotalWithCacheRead
		}
	}
	return m, nil
}

func sumKnown(values ...*int64) (*int64, error) {
	var found bool
	var total int64
	for _, value := range values {
		if value != nil {
			if *value < 0 || *value > math.MaxInt64-total {
				return nil, fmt.Errorf("usage token total exceeds representable range")
			}
			total += *value
			found = true
		}
	}
	if !found {
		return nil, nil
	}
	return &total, nil
}
