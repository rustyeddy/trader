package analysis

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
)

// RSIRegimeEventStudyConfig groups the parameters one
// RunRSIRegimeEventStudy call runs against, plus the run provenance
// needed to reproduce it (instrument, interval, indicator periods,
// horizons) — mirroring EventStudyConfig's own provenance discipline
// (issue #280's requirement), extended here for issue #319 (EQR-01C).
//
// The frozen EQR-01 protocol values (RSIPeriod=2, EMAPeriod=200, the
// six RSIBucket boundaries, the five day horizons, MinObservations=30)
// are research-protocol decisions made by this config's caller, not by
// this package — the same relationship EventStudyConfig already has to
// MR-01's own frozen ZScorePeriod/horizons.
type RSIRegimeEventStudyConfig struct {
	// Instrument identifies which instrument bars were sourced from.
	// Required (must be non-zero).
	Instrument instrument.ID
	// Interval is the bar cadence bars are in (for example
	// marketdata.D1). Required (must be Valid()).
	Interval marketdata.Interval
	// RSIPeriod is the period of the indicator.RSI used to compute the
	// short-term pullback observable. The EQR-01 protocol pins 2.
	RSIPeriod int
	// EMAPeriod is the period of the indicator.EMA used to compute the
	// long-term regime observable. The EQR-01 protocol pins 200.
	EMAPeriod int
	// Horizons is the set of forward-return measurement points.
	Horizons []Horizon
	// MinObservations is the minimum cell observation count at or above
	// which a (Bucket, Horizon) cell is treated as having enough
	// evidence to interpret. Cells below it are still reported, marked
	// Insufficient. The EQR-01 protocol pins 30.
	MinObservations int
	// Bootstrap configures the nonparametric bootstrap confidence
	// interval computed for each cell's mean forward return.
	Bootstrap BootstrapConfig
}

func (cfg RSIRegimeEventStudyConfig) validate() error {
	if cfg.Instrument.IsZero() {
		return ErrMissingInstrument
	}
	if !cfg.Interval.Valid() {
		return ErrMissingInterval
	}
	if cfg.RSIPeriod <= 0 {
		return ErrInvalidRSIPeriod
	}
	if cfg.EMAPeriod <= 0 {
		return ErrInvalidEMAPeriod
	}
	if len(cfg.Horizons) == 0 {
		return ErrNoHorizons
	}
	for _, h := range cfg.Horizons {
		if h.Bars <= 0 {
			return ErrInvalidHorizon
		}
		// validateHorizonLabel only checks hour-labeled horizons
		// ("<N>h") against a fixed-duration Interval; a day-labeled
		// horizon (as this package's day horizons use) never matches
		// and this call is a no-op for them — see its own doc comment.
		if err := validateHorizonLabel(h, cfg.Interval); err != nil {
			return err
		}
	}
	if cfg.MinObservations <= 0 {
		return ErrInvalidMinObservations
	}
	if err := cfg.Bootstrap.validate(); err != nil {
		return err
	}
	return nil
}

// RSIObservation is one time-T EQR-01 observation: the bar's RSI(2)
// value and bucket, and its long-term trend regime. It carries no
// forward-looking information — see RunRSIRegimeEventStudy's own
// no-lookahead documentation.
type RSIObservation struct {
	// Index is bars[Index]'s position in the input slice
	// RunRSIRegimeEventStudy was called with.
	Index int
	// Time is bars[Index].Time, carried through for provenance.
	Time time.Time
	// RSI is the RSI value computed from bars[0:Index+1].
	RSI float64
	// Bucket is ClassifyRSIBucket(RSI).
	Bucket RSIBucket
	// Regime is ClassifyRegime(Close, EMA) computed from
	// bars[0:Index+1].
	Regime Regime
}

// RSIForwardReturn is one RSIObservation's realized outcome at one
// Horizon: the close-to-close return from the observation bar to the
// bar Horizon.Bars ahead of it.
type RSIForwardReturn struct {
	Observation RSIObservation
	Horizon     Horizon
	// Return is (Close[Index+Horizon.Bars] - Close[Index]) / Close[Index].
	Return float64
}

// RSICellStats aggregates every RSIForwardReturn sharing one (Bucket,
// Horizon) pair — the primary unit of evidence the EQR-01 protocol
// requires, computed separately for the positive-regime (primary) and
// all-regime (secondary/exploratory) views — see
// RSIRegimeEventStudyResult's own doc comment on why those two are
// never blended.
type RSICellStats struct {
	Bucket  RSIBucket `json:"bucket"`
	Horizon Horizon   `json:"horizon"`
	// Count is the number of observations contributing to this cell.
	Count int `json:"count"`
	// MeanReturn is the arithmetic mean of Return across the cell's
	// observations. Zero (with Count == 0) when the cell is empty.
	MeanReturn float64 `json:"mean_return"`
	// MedianReturn is the median of Return across the cell's
	// observations.
	MedianReturn float64 `json:"median_return"`
	// PositiveFraction is the fraction of the cell's observations with
	// Return > 0 — the protocol's "positive-return percentage". Zero
	// when Count == 0.
	PositiveFraction float64 `json:"positive_fraction"`
	// StdDevReturn is the population standard deviation of Return
	// across the cell's observations (dividing by Count, not
	// Count-1), matching EventStudyConfig's own convention.
	StdDevReturn float64 `json:"stddev_return"`
	// CILower and CIUpper are the 2.5th and 97.5th percentiles of the
	// bootstrap distribution of the cell's mean Return — the
	// protocol's 95% bootstrap confidence interval for the mean. Both
	// zero when Count == 0.
	CILower float64 `json:"ci_lower"`
	CIUpper float64 `json:"ci_upper"`
	// Insufficient is true when Count is below the configured
	// MinObservations: the protocol's own rule that such a cell must be
	// reported, but never interpreted as evidence.
	Insufficient bool `json:"insufficient"`
}

// RSIRegimeEventStudyResult is the complete output of one
// RunRSIRegimeEventStudy call.
type RSIRegimeEventStudyResult struct {
	Config RSIRegimeEventStudyConfig `json:"config"`
	// BarCount is len(bars) as supplied to RunRSIRegimeEventStudy.
	BarCount int `json:"bar_count"`
	// Start and End are bars[0].Time and bars[len(bars)-1].Time — empty
	// if bars was empty.
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// Observations is every valid observation, in ascending Index
	// order. An observation is valid once both the RSI and EMA
	// indicators are Ready.
	Observations []RSIObservation `json:"observations"`
	// ForwardReturns is every RSIForwardReturn that could be computed:
	// one per (valid Observation, configured Horizon) pair for which
	// bars[Index+Horizon.Bars] existed in the input. Because bars is
	// exactly the caller-supplied partition, a horizon whose future bar
	// would fall outside it is simply never computed — this is what
	// keeps forward labels from crossing a partition boundary (see the
	// package-level no-lookahead documentation on RunRSIRegimeEventStudy).
	ForwardReturns []RSIForwardReturn `json:"forward_returns"`
	// PositiveRegimeStats is the primary confirmatory result: one
	// RSICellStats per (Bucket, Horizon) combination computed only from
	// ForwardReturns whose Observation.Regime is RegimePositive.
	PositiveRegimeStats []RSICellStats `json:"positive_regime_stats"`
	// AllRegimeStats is the secondary/exploratory comparison: the same
	// aggregation computed across every regime, unfiltered. The
	// protocol requires these two never be blended into one number —
	// callers must keep them in these two separate fields/tables, never
	// merge them.
	AllRegimeStats []RSICellStats `json:"all_regime_stats"`
}

// RunRSIRegimeEventStudy computes a deterministic RSI(2)/regime
// forward-return event study over bars, per
// docs/research/eqr-01-research-protocol.org and issue #319 (EQR-01C).
//
// # No lookahead
//
// bars are fed one at a time, in order, into a freshly constructed
// indicator.RSI(cfg.RSIPeriod) and indicator.EMA(cfg.EMAPeriod): the
// observation at index i uses only bars[0:i+1]. Bars after i are used
// only to compute that observation's RSIForwardReturn at each
// configured Horizon — never to influence the observation itself.
//
// # Partition-boundary exclusion
//
// bars must be exactly the caller's chosen partition (for example the
// EQR-01 development partition) — RunRSIRegimeEventStudy has no
// partition concept of its own. Because a forward return is only
// computed when bars[Index+Horizon.Bars] exists within the supplied
// slice, an observation near the end of bars simply contributes fewer
// (or zero) ForwardReturns rather than reaching past the slice's end —
// this is the exact mechanism that keeps a forward label from crossing
// a partition boundary, provided the caller never supplies bars beyond
// the partition itself.
//
// RunRSIRegimeEventStudy returns an error if cfg is malformed. An
// empty or too-short bars slice is not an error: it simply produces a
// Result with no Observations.
func RunRSIRegimeEventStudy(bars []marketdata.Bar, cfg RSIRegimeEventStudyConfig) (RSIRegimeEventStudyResult, error) {
	if err := cfg.validate(); err != nil {
		return RSIRegimeEventStudyResult{}, err
	}

	result := RSIRegimeEventStudyResult{
		Config:   cfg,
		BarCount: len(bars),
	}
	if len(bars) == 0 {
		return result, nil
	}
	result.Start = bars[0].Time
	result.End = bars[len(bars)-1].Time

	rsi, err := indicator.NewRSI(cfg.RSIPeriod)
	if err != nil {
		return RSIRegimeEventStudyResult{}, err
	}
	ema, err := indicator.NewEMA(cfg.EMAPeriod)
	if err != nil {
		return RSIRegimeEventStudyResult{}, err
	}

	closes := make([]float64, len(bars))
	for i, bar := range bars {
		closes[i] = bar.Close.Float64()
	}

	for i, closePrice := range closes {
		if err := rsi.Update(closePrice); err != nil {
			return RSIRegimeEventStudyResult{}, err
		}
		if err := ema.Update(closePrice); err != nil {
			return RSIRegimeEventStudyResult{}, err
		}
		if !rsi.Ready() || !ema.Ready() {
			continue
		}

		obs := RSIObservation{
			Index:  i,
			Time:   bars[i].Time,
			RSI:    rsi.Value(),
			Bucket: ClassifyRSIBucket(rsi.Value()),
			Regime: ClassifyRegime(closePrice, ema.Value()),
		}
		result.Observations = append(result.Observations, obs)

		// A zero observation close cannot label a meaningful forward
		// return (division by zero) — skip every horizon for this
		// observation rather than fabricating a value; the observation
		// itself is unaffected and still recorded above.
		if closePrice == 0 {
			continue
		}

		for _, h := range cfg.Horizons {
			future := i + h.Bars
			if future >= len(closes) {
				continue
			}
			ret := (closes[future] - closePrice) / closePrice
			result.ForwardReturns = append(result.ForwardReturns, RSIForwardReturn{
				Observation: obs,
				Horizon:     h,
				Return:      ret,
			})
		}
	}

	var positiveOnly []RSIForwardReturn
	for _, fr := range result.ForwardReturns {
		if fr.Observation.Regime == RegimePositive {
			positiveOnly = append(positiveOnly, fr)
		}
	}

	result.PositiveRegimeStats = aggregateRSI(positiveOnly, cfg.Horizons, cfg.MinObservations, cfg.Bootstrap)
	result.AllRegimeStats = aggregateRSI(result.ForwardReturns, cfg.Horizons, cfg.MinObservations, cfg.Bootstrap)
	return result, nil
}

// aggregateRSI groups frs by (Bucket, Horizon) and computes one
// RSICellStats for every (Bucket, Horizon) combination in RSIBuckets x
// horizons — including combinations with zero observations — in
// RSIBuckets order then horizons order. Every cell is reported, per
// the frozen protocol's own requirement that a cell below the minimum
// observation count "is reported as insufficient evidence, not as
// positive or negative evidence, and [is] still shown in the results
// table rather than omitted" (docs/research/eqr-01-research-protocol.org).
// A zero-observation cell is the most extreme case of that same rule,
// not a special case to be dropped.
func aggregateRSI(frs []RSIForwardReturn, horizons []Horizon, minObservations int, bootstrap BootstrapConfig) []RSICellStats {
	type key struct {
		bucket  RSIBucket
		horizon Horizon
	}
	groups := make(map[key][]float64)

	for _, fr := range frs {
		k := key{bucket: fr.Observation.Bucket, horizon: fr.Horizon}
		groups[k] = append(groups[k], fr.Return)
	}

	var stats []RSICellStats
	for _, bucket := range RSIBuckets {
		for _, h := range horizons {
			k := key{bucket: bucket, horizon: h}
			returns := groups[k] // nil, and therefore len 0, when the cell has no observations.
			lower, upper := bootstrapMeanCI(returns, bootstrap)
			stats = append(stats, RSICellStats{
				Bucket:           bucket,
				Horizon:          h,
				Count:            len(returns),
				MeanReturn:       mean(returns),
				MedianReturn:     median(returns),
				PositiveFraction: positiveFraction(returns),
				StdDevReturn:     stddev(returns),
				CILower:          lower,
				CIUpper:          upper,
				Insufficient:     len(returns) < minObservations,
			})
		}
	}
	return stats
}

func positiveFraction(returns []float64) float64 {
	if len(returns) == 0 {
		return 0
	}
	var positive int
	for _, r := range returns {
		if r > 0 {
			positive++
		}
	}
	return float64(positive) / float64(len(returns))
}

// NewDayHorizon returns the Horizon for days D1 bars ahead, labeled
// "<days>d". days must be positive.
func NewDayHorizon(days int) (Horizon, error) {
	if days <= 0 {
		return Horizon{}, ErrInvalidHorizon
	}
	return Horizon{Label: fmt.Sprintf("%dd", days), Bars: days}, nil
}

// EQR01Horizons returns the five forward-return horizons
// docs/research/eqr-01-research-protocol.org pins: 1, 2, 3, 5, and 10
// trading days against D1 bars, in that order.
func EQR01Horizons() []Horizon {
	// NewDayHorizon cannot fail for these fixed, positive literals.
	d1, _ := NewDayHorizon(1)
	d2, _ := NewDayHorizon(2)
	d3, _ := NewDayHorizon(3)
	d5, _ := NewDayHorizon(5)
	d10, _ := NewDayHorizon(10)
	return []Horizon{d1, d2, d3, d5, d10}
}
