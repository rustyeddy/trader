// Package candlepattern implements categorical price-action pattern
// recognition over a short window of recent candles — the "does THIS bar's
// shape (long wick, small body, close position within the range) match a
// known pattern" question, as distinct from indicator/'s continuous-valued
// Wilder-style accumulators ("how strong / where / what regime, given
// everything seen so far"). This is a standard, separately-named category in
// technical analysis generally (e.g. TA-Lib splits "Pattern Recognition
// Functions" out from its momentum/trend/volatility indicators), not an
// arbitrary split invented for this codebase.
//
// See docs/Plans/candle-merge-and-rejection-pattern-design.org.
package candlepattern

import (
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// RejectionDetector recognizes rejection-candle price action from a short
// window of recent candles. Unlike indicator/'s accumulators, implementations
// hold only a small fixed lookback, not full history.
type RejectionDetector interface {
	Name() string
	Ready() bool                   // true once enough candles seen for lookback
	Update(window []market.Candle) // most-recent-last; caller-maintained
	Matched() bool                 // true if the most recent Update matched
	Side() types.Side              // direction implied by the match (valid iff Matched())
}

// DetectorConfig mirrors the params a rejection-candle EntryConfig carries.
type DetectorConfig struct {
	Kind   string         `json:"kind"   yaml:"kind"`
	Params map[string]any `json:"params" yaml:"params"`
}

// GetRejectionDetector constructs a RejectionDetector from cfg. If cfg.Kind
// is empty, wick-rejection (the only implementation so far) is used.
func GetRejectionDetector(cfg DetectorConfig, scale types.Scale6) (RejectionDetector, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Kind)) {
	case "", "wick-rejection":
		minWickRatio, err := floatParamOrDefault(cfg.Params, "min-wick-ratio", 0.5)
		if err != nil {
			return nil, err
		}
		maxClosePos, err := floatParamOrDefault(cfg.Params, "max-close-pos", 0.3)
		if err != nil {
			return nil, err
		}
		minWickATR, err := floatParamOrDefault(cfg.Params, "min-wick-atr", 0.5)
		if err != nil {
			return nil, err
		}
		lookback, err := intParamOrDefault(cfg.Params, "lookback", 1)
		if err != nil {
			return nil, err
		}
		atrPeriod, err := intParamOrDefault(cfg.Params, "atr-period", 14)
		if err != nil {
			return nil, err
		}
		return NewWickRejection(minWickRatio, maxClosePos, minWickATR, lookback, atrPeriod, scale)

	default:
		return nil, fmt.Errorf("unknown rejection detector %q", strings.TrimSpace(cfg.Kind))
	}
}

// floatParamOrDefault/intParamOrDefault wrap types' general-purpose param
// readers (also used by strategy and elsewhere) with a default-on-absent
// value.
func floatParamOrDefault(params map[string]any, key string, def float64) (float64, error) {
	v, ok, err := types.GetFloat64Param(params, key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return def, nil
	}
	return v, nil
}

func intParamOrDefault(params map[string]any, key string, def int) (int, error) {
	v, ok, err := types.GetIntParam(params, key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return def, nil
	}
	return v, nil
}
