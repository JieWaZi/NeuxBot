package usage

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"

	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// ParseAmount accepts bounded nonnegative JSON decimals and preserves their value and scale.
func ParseAmount(value string) (*big.Rat, int, bool) {
	if len(value) == 0 || len(value) > 128 || strings.HasPrefix(value, "-") {
		return nil, 0, false
	}
	var number json.Number
	if value[0] < '0' || value[0] > '9' || json.Unmarshal([]byte(value), &number) != nil {
		return nil, 0, false
	}
	mantissa, exponent := strings.ToLower(value), 0
	if before, after, ok := strings.Cut(mantissa, "e"); ok {
		var err error
		exponent, err = strconv.Atoi(after)
		if err != nil || exponent < -128 || exponent > 128 {
			return nil, 0, false
		}
		mantissa = before
	}
	_, fraction, _ := strings.Cut(mantissa, ".")
	rational, ok := new(big.Rat).SetString(value)
	return rational, max(0, len(fraction)-exponent), ok
}

// NativeAmount accepts an amount scoped to this invocation or an observed subset of it.
func NativeAmount(c *acpmeta.UsageCost) (string, bool) {
	if c == nil || c.Aggregation != "turn_delta" || c.Source == "" || len(c.Source) > 128 ||
		len(c.Currency) != 3 || strings.IndexFunc(c.Currency, func(c rune) bool { return c < 'A' || c > 'Z' }) != -1 ||
		(c.Valuation != "reported" && c.Valuation != "estimated") {
		return "", false
	}
	switch c.Scope {
	case "turn", "invocation", "observed_requests", "last_request":
	default:
		return "", false
	}
	amount, digits, ok := ParseAmount(c.Amount.String())
	if !ok {
		return "", false
	}
	return amount.FloatString(digits), c.Scope == "turn" || c.Scope == "invocation"
}

// CodexPriceProof identifies request-level evidence for a published API rate.
type CodexPriceProof struct {
	// Publisher is the verified price publisher for the request.
	Publisher string
	// Model is the exact model used by the request.
	Model string
	// Tier is the service tier confirmed by native evidence.
	Tier string
	// Version identifies the verified rates applicable to the request date.
	Version string
	// RequestInputUpperBound bounds the input of every covered request.
	RequestInputUpperBound *int64
}

const (
	Codex53StandardPriceVersion = "openai-gpt-5.3-codex-standard-2026-11-04"
	Codex53StandardPriceSource  = "https://developers.openai.com/api/docs/pricing"
)

// QuoteCodexAPI estimates usage only when the published rate and request scope are proven.
// Current Codex terminal results lack this evidence. Host settings cannot supply it.
func QuoteCodexAPI(t Tokens, proof CodexPriceProof) (string, bool) {
	if proof.Publisher != "openai" || proof.Model != "gpt-5.3-codex" || proof.Tier != "standard" ||
		proof.Version != Codex53StandardPriceVersion || proof.RequestInputUpperBound == nil ||
		*proof.RequestInputUpperBound < 0 || *proof.RequestInputUpperBound > 272_000 ||
		t.Input == nil || t.CacheRead == nil || t.CacheWrite == nil || *t.CacheWrite != 0 || t.Output == nil ||
		*t.Input < 0 || *t.CacheRead < 0 || *t.Output < 0 {
		return "", false
	}
	// OpenAI rates are 1.75, 0.175 and 14 USD per million tokens.
	// Units of one billionth of a dollar represent all three exactly.
	total := new(big.Int).Mul(big.NewInt(*t.Input), big.NewInt(1750))
	total.Add(total, new(big.Int).Mul(big.NewInt(*t.CacheRead), big.NewInt(175)))
	total.Add(total, new(big.Int).Mul(big.NewInt(*t.Output), big.NewInt(14000)))
	return new(big.Rat).SetFrac(total, big.NewInt(1_000_000_000)).FloatString(9), true
}
