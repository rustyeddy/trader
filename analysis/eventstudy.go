package analysis

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/rustyeddy/trader/indicator"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
)

// EventStudyConfig groups the pinned MR-01 parameters an event study
// runs against, plus the run provenance issue #280's acceptance
// criteria requires (instrument, interval, span, parameters): a
// Result must be self-describing enough that a caller cannot silently
// mislabel which instrument or bar cadence it came from. Every field
// here is a frozen research parameter (see
// docs/research/mr-01-experiment-definition.org) or run identity, not
// a value RunEventStudy itself tunes.
type EventStudyConfig struct {
	// Instrument identifies which instrument bars were sourced from.
	// Required (must be non-zero) — RunEventStudy never inspects bars
	// to infer instrument identity itself.
	Instrument instrument.ID
	// Interval is the bar cadence bars are in (for example
	// marketdata.H1). Required (must be Valid()). RunEventStudy is
	// itself interval-agnostic — it only ever advances Horizon.Bars
	// positions through bars, whatever their actual cadence — but
	// Interval is validated against any horizon whose Label claims an
	// hour count (see validateHorizonLabel), so an hour-labeled
	// horizon built for H1 bars cannot silently be run against a
	// different cadence without detection.
	Interval marketdata.Interval
	// ZScorePeriod is N, the rolling window length (in bars) the
	// underlying indicator.ZScore uses. MR-01 pins N = 20.
	ZScorePeriod int
	// Horizons is the set of forward-return measurement points. MR-01
	// pins MR01Horizons() (4h/12h/24h/48h against H1 bars).
	Horizons []Horizon
}

// horizonHourLabel matches a Horizon.Label of the form "<N>h", the
// shape NewH1Horizon/MR01Horizons produce.
var horizonHourLabel = regexp.MustCompile(`^([0-9]+)h$`)

// validate reports whether cfg is well-formed.
func (cfg EventStudyConfig) validate() error {
	if cfg.Instrument.IsZero() {
		return ErrMissingInstrument
	}
	if !cfg.Interval.Valid() {
		return ErrMissingInterval
	}
	if cfg.ZScorePeriod <= 0 {
		return ErrInvalidZScorePeriod
	}
	if len(cfg.Horizons) == 0 {
		return ErrNoHorizons
	}
	for _, h := range cfg.Horizons {
		if h.Bars <= 0 {
			return ErrInvalidHorizon
		}
		if err := validateHorizonLabel(h, cfg.Interval); err != nil {
			return err
		}
	}
	return nil
}

// validateHorizonLabel checks an hour-labeled Horizon (Label matching
// "<N>h") against Interval when Interval has a fixed,
// calendar-independent bar duration — UnitMinute or UnitHour. A
// mismatch (for example a "4h" horizon whose Bars, at Interval's
// actual per-bar duration, do not total 4 hours) is exactly the
// silent-cadence-mislabeling bug this validation exists to catch —
// see EventStudyConfig's own doc comment and issue #280's provenance
// requirement.
//
// UnitDay and UnitWeek bars have calendar-dependent, non-fixed
// duration (ADR-012, ADR-021: an FX daily bar spans 23h-25h across a
// DST transition), so this check is deliberately narrow and does not
// attempt to validate an hour label against them — nor does it check
// a Label that does not claim an hour count at all.
func validateHorizonLabel(h Horizon, interval marketdata.Interval) error {
	m := horizonHourLabel.FindStringSubmatch(h.Label)
	if m == nil {
		return nil
	}

	var barHours float64
	switch interval.Unit() {
	case marketdata.UnitHour:
		barHours = float64(interval.Count())
	case marketdata.UnitMinute:
		barHours = float64(interval.Count()) / 60
	default:
		return nil
	}

	claimedHours, err := strconv.Atoi(m[1])
	if err != nil {
		return nil // regexp already guarantees digits; unreachable.
	}
	gotHours := float64(h.Bars) * barHours
	if gotHours != float64(claimedHours) {
		return fmt.Errorf("%w: horizon %q claims %dh but %d bars at %s totals %vh",
			ErrHorizonIntervalMismatch, h.Label, claimedHours, h.Bars, interval, gotHours)
	}
	return nil
}

// Observation is one time-T mean-reversion observation: the rolling
// Z-score of a single bar's close, and the Bucket it falls into. It
// carries no forward-looking information — see RunEventStudy's own
// no-lookahead documentation.
type Observation struct {
	// Index is bars[Index]'s position in the input slice RunEventStudy
	// was called with.
	Index int
	// Time is bars[Index].Time, carried through for provenance.
	Time time.Time
	// Z is the rolling Z-score computed from bars[0:Index+1].
	Z float64
	// Bucket is ClassifyZScore(Z).
	Bucket Bucket
}

// ForwardReturn is one Observation's realized outcome at one Horizon:
// the close-to-close return from the observation bar to the bar
// Horizon.Bars ahead of it.
type ForwardReturn struct {
	Observation Observation
	Horizon     Horizon
	// Return is (Close[Index+Horizon.Bars] - Close[Index]) / Close[Index].
	Return float64
}

// BucketHorizonStats aggregates every ForwardReturn sharing one
// (Bucket, Horizon) pair — the primary unit of evidence MR-01 asks
// for.
type BucketHorizonStats struct {
	Bucket  Bucket  `json:"bucket"`
	Horizon Horizon `json:"horizon"`
	// Count is the number of observations contributing to this
	// bucket/horizon cell. MR-01 requires at least 30 before any
	// conclusion is drawn from a cell — RunEventStudy reports whatever
	// count actually occurred; it is the caller's (MR-04's)
	// responsibility to apply that minimum-evidence threshold when
	// interpreting results.
	Count int `json:"count"`
	// MeanReturn is the arithmetic mean of Return across the cell's
	// observations. Zero (with Count == 0) when the cell is empty.
	MeanReturn float64 `json:"mean_return"`
	// MedianReturn is the median of Return across the cell's
	// observations.
	MedianReturn float64 `json:"median_return"`
	// StdDevReturn is the population standard deviation of Return
	// across the cell's observations (dividing by Count, not
	// Count-1) — the dispersion of this cell's own realized returns,
	// not an estimate from a wider population sample, the same
	// convention indicator.RollingStdDev uses.
	StdDevReturn float64 `json:"stddev_return"`
	// FractionTowardMean is the fraction of the cell's observations
	// whose Return moved toward the rolling mean: Return and the
	// observation's Z carry opposite signs (Return*Z < 0). An
	// observation whose Return or Z is exactly zero does not count as
	// having moved toward the mean, since there is no directional
	// move to score. Zero when Count == 0.
	FractionTowardMean float64 `json:"fraction_toward_mean"`
}

// Result is the complete output of one RunEventStudy call: every
// observation and forward return computed, plus the aggregated
// BucketHorizonStats MR-01/MR-03 need as evidence, plus enough
// provenance (issue #280's own requirement) to reproduce the run.
type Result struct {
	Config EventStudyConfig `json:"config"`
	// BarCount is len(bars) as supplied to RunEventStudy.
	BarCount int `json:"bar_count"`
	// Start and End are bars[0].Time and bars[len(bars)-1].Time —
	// empty if bars was empty.
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// Observations is every valid observation RunEventStudy computed,
	// in ascending Index order. An observation is valid once the
	// rolling window is warm and its Z-score is defined (rolling
	// standard deviation non-zero) — see indicator.ZScore.Value.
	Observations []Observation `json:"observations"`
	// ForwardReturns is every ForwardReturn RunEventStudy could
	// compute: one per (valid Observation, configured Horizon) pair
	// for which bars[Index+Horizon.Bars] existed in the input. An
	// observation near the end of bars simply contributes fewer
	// ForwardReturns than one with a full future available — no
	// value is fabricated for a horizon whose future bar does not
	// exist.
	ForwardReturns []ForwardReturn `json:"forward_returns"`
	// Stats is one BucketHorizonStats per (Bucket, Horizon)
	// combination that had at least one ForwardReturn, in Buckets
	// order, then Config.Horizons order.
	Stats []BucketHorizonStats `json:"stats"`
}

// RunEventStudy computes a deterministic Z-score forward-return event
// study over bars, per docs/research/mr-01-experiment-definition.org
// and issue #280 (MR-03).
//
// # No lookahead
//
// bars are fed one at a time, in order, into a freshly constructed
// indicator.ZScore(cfg.ZScorePeriod): the observation at index i uses
// only bars[0:i+1]. Bars after i are used only to compute that
// observation's ForwardReturn at each configured Horizon — never to
// influence the observation itself. Truncating bars after some index k
// therefore leaves every Observation with Index <= k-1 unchanged — Z
// and Bucket do not depend on Horizons at all — though it does reduce
// how many ForwardReturns those observations end up with, since a
// horizon whose future bar was truncated away no longer has one to
// measure (see TestRunEventStudy_ObservationsAreLookaheadFree).
//
// RunEventStudy returns an error if cfg is malformed. An empty or
// too-short bars slice is not an error: it simply produces a Result
// with no Observations.
func RunEventStudy(bars []marketdata.Bar, cfg EventStudyConfig) (Result, error) {
	if err := cfg.validate(); err != nil {
		return Result{}, err
	}

	result := Result{
		Config:   cfg,
		BarCount: len(bars),
	}
	if len(bars) == 0 {
		return result, nil
	}
	result.Start = bars[0].Time
	result.End = bars[len(bars)-1].Time

	z, err := indicator.NewZScore(cfg.ZScorePeriod)
	if err != nil {
		return Result{}, err
	}

	closes := make([]float64, len(bars))
	for i, bar := range bars {
		closes[i] = bar.Close.Float64()
	}

	for i, closePrice := range closes {
		if err := z.Update(closePrice); err != nil {
			return Result{}, err
		}
		score, ok := z.Value()
		if !ok {
			continue
		}

		obs := Observation{
			Index:  i,
			Time:   bars[i].Time,
			Z:      score,
			Bucket: ClassifyZScore(score),
		}
		result.Observations = append(result.Observations, obs)

		// A zero observation close cannot label a meaningful forward
		// return (it would divide by zero, producing +Inf/-Inf/NaN and
		// poisoning every downstream statistic) — marketdata.Bar.Validate
		// does not itself reject a zero Close, so this is a real,
		// reachable input, not a hypothetical. Skip every horizon for
		// this observation rather than fabricating a value; the
		// observation itself (Z, Bucket) is unaffected and still
		// recorded above.
		if closePrice == 0 {
			continue
		}

		for _, h := range cfg.Horizons {
			future := i + h.Bars
			if future >= len(closes) {
				continue
			}
			ret := (closes[future] - closePrice) / closePrice
			result.ForwardReturns = append(result.ForwardReturns, ForwardReturn{
				Observation: obs,
				Horizon:     h,
				Return:      ret,
			})
		}
	}

	result.Stats = aggregate(result.ForwardReturns, cfg.Horizons)
	return result, nil
}

// aggregate groups frs by (Bucket, Horizon) and computes
// BucketHorizonStats for every combination that has at least one
// observation, in Buckets order then horizons order.
func aggregate(frs []ForwardReturn, horizons []Horizon) []BucketHorizonStats {
	// horizon is keyed by the full Horizon value (Label and Bars), not
	// Label alone: two distinct horizons that happened to share a
	// label (or both had an empty label) would otherwise be merged
	// into one cell, silently combining unrelated forward returns.
	type key struct {
		bucket  Bucket
		horizon Horizon
	}
	groups := make(map[key][]float64)
	zByGroup := make(map[key][]float64)

	for _, fr := range frs {
		k := key{bucket: fr.Observation.Bucket, horizon: fr.Horizon}
		groups[k] = append(groups[k], fr.Return)
		zByGroup[k] = append(zByGroup[k], fr.Observation.Z)
	}

	var stats []BucketHorizonStats
	for _, bucket := range Buckets {
		for _, h := range horizons {
			k := key{bucket: bucket, horizon: h}
			returns, ok := groups[k]
			if !ok {
				continue
			}
			zs := zByGroup[k]
			stats = append(stats, BucketHorizonStats{
				Bucket:             bucket,
				Horizon:            h,
				Count:              len(returns),
				MeanReturn:         mean(returns),
				MedianReturn:       median(returns),
				StdDevReturn:       stddev(returns),
				FractionTowardMean: fractionTowardMean(returns, zs),
			})
		}
	}
	return stats
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func stddev(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := mean(xs)
	var sq float64
	for _, x := range xs {
		d := x - m
		sq += d * d
	}
	return math.Sqrt(sq / float64(len(xs)))
}

func fractionTowardMean(returns, zs []float64) float64 {
	if len(returns) == 0 {
		return 0
	}
	var toward int
	for i, ret := range returns {
		if ret*zs[i] < 0 {
			toward++
		}
	}
	return float64(toward) / float64(len(returns))
}
