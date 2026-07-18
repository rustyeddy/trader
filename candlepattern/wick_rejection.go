package candlepattern

import (
	"fmt"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// WickRejection is the first concrete RejectionDetector: a single (or,
// with lookback > 1, multi-bar aggregate) pin-bar / rejection-wick pattern
// — a long wick on one side of the bar rejecting that direction's prices,
// with the close sitting near the opposite extreme — filtered by wick
// length relative to recent volatility (ATR) so noise-scale wicks on quiet
// bars don't qualify.
//
// Four tunable parameters (design established 2026-07-15):
//   - min-wick-ratio: wick length / total bar range, minimum to count as a wick
//   - max-close-pos: close's distance from the opposite extreme, as a
//     fraction of the bar's range, maximum allowed (close must sit near the
//     end opposite the rejected wick)
//   - min-wick-atr: minimum wick length, in ATR multiples — filters out
//     noise-scale wicks on quiet bars
//   - lookback: >1 judges the pattern against an aggregate of the most
//     recent lookback candles (as if they were one combined bar) rather
//     than a single bar in isolation
type WickRejection struct {
	minWickRatio float64
	maxClosePos  float64
	minWickATR   float64
	lookback     int

	// atr provides the volatility reference for min-wick-atr. It is fed
	// the newest candle on every Update call regardless of lookback — ATR
	// is a full-history accumulator that needs continuous data to stay
	// warm — while the pattern-matching window below only ever holds the
	// most recent lookback candles. These are two different internal
	// cadences inside the same type; do not "simplify" the window
	// trimming into feeding ATR only the trimmed window, or ATR will warm
	// up far slower than intended whenever lookback > 1.
	atr *indicator.ATR

	window  []market.Candle // most-recent-last, len <= lookback
	matched bool
	side    types.Side
}

// NewWickRejection constructs a WickRejection detector. atrPeriod/scale
// configure its internal ATR (see min-wick-atr above).
func NewWickRejection(minWickRatio, maxClosePos, minWickATR float64, lookback, atrPeriod int, scale types.Scale6) (*WickRejection, error) {
	if minWickRatio <= 0 || minWickRatio > 1 {
		return nil, fmt.Errorf("wick-rejection: min-wick-ratio must be in (0,1], got %v", minWickRatio)
	}
	if maxClosePos < 0 || maxClosePos > 1 {
		return nil, fmt.Errorf("wick-rejection: max-close-pos must be in [0,1], got %v", maxClosePos)
	}
	if minWickATR < 0 {
		return nil, fmt.Errorf("wick-rejection: min-wick-atr must be >= 0, got %v", minWickATR)
	}
	if lookback < 1 {
		return nil, fmt.Errorf("wick-rejection: lookback must be >= 1, got %d", lookback)
	}
	atr, err := indicator.NewATR(atrPeriod, scale)
	if err != nil {
		return nil, fmt.Errorf("wick-rejection: %w", err)
	}
	return &WickRejection{
		minWickRatio: minWickRatio,
		maxClosePos:  maxClosePos,
		minWickATR:   minWickATR,
		lookback:     lookback,
		atr:          atr,
	}, nil
}

func (w *WickRejection) Name() string { return "wick-rejection" }

func (w *WickRejection) Ready() bool { return w.atr.Ready() && len(w.window) >= w.lookback }

// Update feeds the newest candle to ATR and re-evaluates the pattern
// against the (possibly multi-bar) window. window is most-recent-last;
// only its last element is treated as "the new bar this call represents"
// for ATR purposes — passing the same window twice (e.g. an unchanged
// caller-side buffer) does not double-count.
//
// An empty window clears all pattern-window state (matched and the stored
// window itself, so Ready() correctly goes back to false) without
// resetting ATR — ATR is a continuous market-volatility read, not
// per-episode state, so callers (e.g. EntryTrigger.Reset implementations)
// can use Update(nil) to fully clear pattern state across an episode
// boundary while keeping ATR warm.
func (w *WickRejection) Update(window []market.Candle) {
	w.matched = false
	w.window = window
	if len(window) == 0 {
		return
	}

	w.atr.Update(window[len(window)-1])

	if len(w.window) > w.lookback {
		w.window = w.window[len(w.window)-w.lookback:]
	}
	if !w.Ready() {
		return
	}

	w.matched, w.side = w.evaluate(aggregateWindow(w.window))
}

func (w *WickRejection) Matched() bool    { return w.matched }
func (w *WickRejection) Side() types.Side { return w.side }

// evaluate judges a single (possibly aggregated) candle against the
// configured thresholds. Ratios and the ATR-multiple comparison are all
// dimensionless, so raw fixed-point Price differences are compared/divided
// directly — the fixed-point scale cancels out, no float64 price ever
// leaves this function's local math.
func (w *WickRejection) evaluate(c market.Candle) (bool, types.Side) {
	rangeP := float64(c.High - c.Low)
	if rangeP <= 0 {
		return false, types.Flat
	}
	atrPx := float64(w.atr.Price())
	if atrPx <= 0 {
		return false, types.Flat
	}

	bodyHigh, bodyLow := float64(c.Open), float64(c.Close)
	if bodyLow > bodyHigh {
		bodyHigh, bodyLow = bodyLow, bodyHigh
	}
	upperWick := float64(c.High) - bodyHigh
	lowerWick := bodyLow - float64(c.Low)
	closePos := (float64(c.Close) - float64(c.Low)) / rangeP // 0 at low, 1 at high

	// Bullish rejection: long lower wick (downside rejected), close near
	// the high.
	if lowerWick/rangeP >= w.minWickRatio &&
		(1-closePos) <= w.maxClosePos &&
		lowerWick/atrPx >= w.minWickATR {
		return true, types.Long
	}

	// Bearish rejection: long upper wick (upside rejected), close near
	// the low.
	if upperWick/rangeP >= w.minWickRatio &&
		closePos <= w.maxClosePos &&
		upperWick/atrPx >= w.minWickATR {
		return true, types.Short
	}

	return false, types.Flat
}

// aggregateWindow folds window into one synthetic OHLC candle: open of the
// first bar, close of the last, high/low extremes across all of them —
// what "judge the pattern against the last lookback bars as if they were
// one combined bar" means for lookback > 1.
func aggregateWindow(window []market.Candle) market.Candle {
	agg := window[0]
	for _, c := range window[1:] {
		if c.High > agg.High {
			agg.High = c.High
		}
		if c.Low < agg.Low {
			agg.Low = c.Low
		}
	}
	agg.Close = window[len(window)-1].Close
	return agg
}
