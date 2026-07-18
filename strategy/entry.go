package strategy

import (
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// EntryTrigger decides, once a scanner episode has become eligible for
// entry (its signal date has passed and no lot is open), which specific
// bar is the actual entry bar. It is orthogonal to selection (episode
// collapsing, already done upstream by signalreplay/review) and to exit
// (ExitStrategy) — it answers only "has this bar earned an entry",
// replacing a hardcoded "first eligible bar" rule with a pluggable,
// config-driven, backtestable choice. See
// docs/Plans/entry-strategy-design.org.
type EntryTrigger interface {
	// Name returns a human-readable description for reports.
	Name() string

	// Ready reports whether the trigger has enough history to evaluate.
	Ready() bool

	// Tick updates internal indicators/pattern state. Called every bar,
	// whether or not an episode is currently pending, so indicators can
	// warm up — mirrors ExitStrategy.Tick's every-bar-regardless contract.
	Tick(c market.Candle)

	// Triggered reports whether c is the entry bar for a pending episode
	// of the given bias that became eligible at episodeStart. Called only
	// once an episode is already eligible (signal date passed, no lot
	// open); the caller is responsible for episode identity and for
	// calling Reset when a *different* episode becomes pending (e.g. the
	// prior one aged out or flipped) so trigger state does not leak
	// across episodes.
	Triggered(side types.Side, episodeStart time.Time, c market.Candle) bool

	// Reset clears any per-episode state (e.g. a bar counter or a
	// candle-pattern lookback window). Called whenever the pending episode
	// changes identity, and from the owning Strategy's own Reset().
	Reset()
}

// EntryConfig mirrors the entry: section of a YAML backtest config — same
// shape as ExitConfig/RegimeConfig.
type EntryConfig struct {
	Kind   string         `json:"kind"   yaml:"kind"`
	Params map[string]any `json:"params" yaml:"params"`
}

// GetEntryTrigger constructs an EntryTrigger from cfg. If cfg.Kind is
// empty, NextOpenEntry is returned — the pre-existing "fire on the first
// eligible bar" behavior — so existing configs and regression tests keep
// working unchanged.
//
// scale is threaded through the same way GetExitStrategy/GetRegimeFilter
// take it: entry kinds backed by a price-scale-aware indicator (e.g.
// rejection-candle's internal ATR) need it to construct that indicator.
// entry-strategy-design.org's original sketch omitted scale because it
// predates the concrete rejection-candle spec that introduced the ATR
// dependency; this signature reflects the codebase's actual established
// factory convention (GetExitStrategy, GetRegimeFilter both take scale).
func GetEntryTrigger(cfg EntryConfig, scale types.Scale6) (EntryTrigger, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Kind)) {
	case "", "next-open":
		return NextOpenEntry{}, nil

	case "rejection-candle":
		minWickRatio, err := positiveFloat64ParamOrDefault(cfg.Params, "min-wick-ratio", 0.5)
		if err != nil {
			return nil, err
		}
		// max-close-pos: not positiveFloat64ParamOrDefault — 0 is a valid,
		// meaningful value (close must sit exactly at the opposite
		// extreme) that must not be silently replaced by the default.
		maxClosePos, err := float64ParamOrDefault(cfg.Params, "max-close-pos", 0.3)
		if err != nil {
			return nil, err
		}
		minWickATR, err := float64ParamOrDefault(cfg.Params, "min-wick-atr", 0.5)
		if err != nil {
			return nil, err
		}
		lookback, err := positiveIntParamOrDefault(cfg.Params, "lookback", 1)
		if err != nil {
			return nil, err
		}
		atrPeriod, err := positiveIntParamOrDefault(cfg.Params, "atr-period", 14)
		if err != nil {
			return nil, err
		}
		return NewWickRejectionEntry(minWickRatio, maxClosePos, minWickATR, lookback, atrPeriod, scale)

	default:
		return nil, fmt.Errorf("unknown entry trigger %q", strings.TrimSpace(cfg.Kind))
	}
}

// NextOpenEntry is a pass-through trigger: any bar in an already-eligible
// episode triggers immediately. This is byte-for-byte the pre-EntryTrigger
// v1 hardcoded rule, expressed as the default implementation of the new
// interface — swapping signalreplay onto EntryTrigger with Kind:"" or
// "next-open" must not change a single existing outcome-CSV row.
type NextOpenEntry struct{}

func (NextOpenEntry) Name() string { return "next-open" }
func (NextOpenEntry) Ready() bool  { return true }
func (NextOpenEntry) Tick(_ market.Candle) {
}
func (NextOpenEntry) Triggered(_ types.Side, _ time.Time, _ market.Candle) bool {
	return true
}
func (NextOpenEntry) Reset() {}
