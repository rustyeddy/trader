package indicator

import "math"

// RSI is a streaming Wilder Relative Strength Index over a fixed
// period (issue #317, EQR-01B) — the primary short-term oversold
// observable docs/research/eqr-01-research-protocol.org's frozen
// EQR-01 hypothesis needs (RSI(2), period pinned by that protocol, not
// by this package). RSI is a general-purpose primitive: nothing here
// is specific to SPY, equities, or the EQR-01 bucket/horizon logic
// that will eventually consume it.
//
// # Wilder smoothing, not a rolling simple average
//
// RSI accumulates the average gain and average loss of consecutive
// price changes using Wilder's original recursive smoothing, not a
// simple moving average recomputed over a sliding window (the two
// diverge after the very first post-warmup update, and only Wilder's
// recursion is "the" RSI Wilder defined):
//
//	avgGain[t] = ((period-1)*avgGain[t-1] + gain[t]) / period
//	avgLoss[t] = ((period-1)*avgLoss[t-1] + loss[t]) / period
//
// seeded once, on the period-th price change, by the arithmetic mean
// of the first `period` gains/losses — the conventional Wilder
// initialization, and the same "arithmetic-mean seed, then a
// different recurrence applies from the next sample on" shape EMA's
// own SMA-seeding already establishes for this package (see EMA's own
// doc comment) — RSI is not a parallel convention invented for this
// one indicator.
//
// # Warmup
//
// RSI needs `period` price changes, which needs `period + 1` price
// samples: the very first Update only ever establishes the previous
// close a delta can be computed against and never itself produces a
// delta. Ready becomes true on the exact Update call that supplies the
// period-th delta (the (period+1)-th price sample overall), matching
// EMA/SMA's own "Ready becomes true on the Period-th call, not before
// and not after" convention.
//
// # Boundary values
//
// Once Ready, Value is always defined — RSI's own algebra only breaks
// down (average loss of zero) at exactly the boundary the standard
// definition already assigns a value to:
//
//   - avgLoss == 0 and avgGain > 0: RSI = 100 (every recent change was
//     a gain);
//   - avgGain == 0 and avgLoss > 0: RSI = 0 (every recent change was a
//     loss);
//   - avgGain == 0 and avgLoss == 0: RSI = 50 (no price movement at
//     all in the averaging window — neither overbought nor oversold).
//
// The zero value is not usable; construct an RSI with NewRSI.
type RSI struct {
	period int

	haveClose bool
	prevClose float64

	seedGainSum float64
	seedLossSum float64
	deltaCount  int

	avgGain float64
	avgLoss float64
	ready   bool
}

// NewRSI returns an RSI of the given period. period must be positive;
// NewRSI returns ErrInvalidPeriod otherwise. EQR-01 uses period 2
// (docs/research/eqr-01-research-protocol.org); that value is a
// research-protocol decision made by this constructor's caller, not by
// this package.
func NewRSI(period int) (*RSI, error) {
	if period <= 0 {
		return nil, ErrInvalidPeriod
	}
	return &RSI{period: period}, nil
}

// Period returns the period this RSI was constructed with.
func (r *RSI) Period() int {
	return r.period
}

// Update advances r by one new price sample. The first call after
// construction only records sample as the previous close — it
// produces no price change and cannot advance Ready, exactly matching
// the "period + 1 samples needed" warmup contract. Every later call
// computes delta = sample - previousClose, splits it into a gain
// (max(delta, 0)) and a loss (max(-delta, 0)), and folds it into the
// seed average (before Ready) or the Wilder recurrence (after).
//
// Update rejects a non-finite sample (NaN, +Inf, -Inf) with
// ErrNonFiniteSample and leaves r's state completely unchanged — the
// same contract EMA.Update/SMA.Update already establish, for the
// identical reason: silently accepting one would permanently poison
// the accumulated averages, and every later Value would silently stay
// wrong with no way for a caller to detect it.
func (r *RSI) Update(sample float64) error {
	if math.IsNaN(sample) || math.IsInf(sample, 0) {
		return ErrNonFiniteSample
	}

	if !r.haveClose {
		r.prevClose = sample
		r.haveClose = true
		return nil
	}

	delta := sample - r.prevClose
	r.prevClose = sample
	gain := math.Max(delta, 0)
	loss := math.Max(-delta, 0)

	if !r.ready {
		r.seedGainSum += gain
		r.seedLossSum += loss
		r.deltaCount++
		if r.deltaCount == r.period {
			r.avgGain = r.seedGainSum / float64(r.period)
			r.avgLoss = r.seedLossSum / float64(r.period)
			r.ready = true
		}
		return nil
	}

	n := float64(r.period)
	r.avgGain = ((n-1)*r.avgGain + gain) / n
	r.avgLoss = ((n-1)*r.avgLoss + loss) / n
	return nil
}

// Ready reports whether r has received enough price samples
// (period + 1) to produce a meaningful value — see the package doc
// comment's Warmup section. Callers must check Ready before trusting
// Value, the same convention every other indicator in this package
// establishes.
func (r *RSI) Ready() bool {
	return r.ready
}

// Value returns r's current RSI, always in [0, 100]. It is meaningful
// only once Ready reports true; before then it returns 0, which is
// otherwise indistinguishable from a genuine zero-gain/zero-loss
// (flat) average — callers must consult Ready, not infer readiness
// from Value, exactly as EMA.Value/SMA.Value already document.
//
// Once Ready, Value is always defined — see the package doc comment's
// Boundary values section for the three cases where dividing average
// gain by average loss would otherwise be undefined or infinite.
func (r *RSI) Value() float64 {
	if !r.ready {
		return 0
	}
	switch {
	case r.avgGain == 0 && r.avgLoss == 0:
		return 50
	case r.avgLoss == 0:
		return 100
	case r.avgGain == 0:
		return 0
	default:
		rs := r.avgGain / r.avgLoss
		return 100 - 100/(1+rs)
	}
}
