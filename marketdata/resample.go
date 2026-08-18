package marketdata

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/rustyeddy/trader/num"
)

// deriveAndPublish executes one ActionDeriveCanonical entry: resamples
// canonical D1 into W1 for the target (instrument, year, month), and
// publishes the result through the canonical store.
//
// # One parent partition, matching the store's own convention
//
// A W1 partition's Manifest.Parent always references the D1 partition
// filed under the *same* (year, month) key — never a set of parent
// partitions, even though a week near a month boundary can pull some of
// its D1 bars from an adjacent month's file (Bars, #78, already handles
// that boundary-spillover transparently). This mirrors the identical
// same-month-key assumption Plan's own isStale check (#79) already
// makes for W1 staleness, and is a deliberate simplification the
// single-parent Manifest.Parent shape already implies, not an
// oversight: it is what lets Parent stay one {Instrument, Interval,
// Revision} triple instead of a set.
//
// # Per-week completeness, not a whole-month gate
//
// Plan's own gating (#79's deriveActionsW1) only guarantees the
// *queried* D1 range was complete at the time Plan ran; Build can run
// later, and the target month can contain weeks outside whatever range
// originally triggered the Action. deriveAndPublish therefore
// independently re-checks D1 completeness per week — via weekIsD1Ready,
// covering both a Gap and a non-Current partition status — and skips
// (leaves absent) any week that is not ready, rather than aborting the
// whole month or trusting a stale snapshot. This is deliberately
// different from normalizeAndPublish's abort-on-bad-record rule: an
// incomplete D1 week is not corrupt input, it simply is not ready yet,
// exactly like any other coverage gap.
func (m *Manager) deriveAndPublish(ctx context.Context, action Action) (PublishResult, error) {
	symbol, err := m.resolveRawSymbol(action.Instrument)
	if err != nil {
		return PublishResult{}, err
	}

	monthStart := time.Date(action.Year, action.Month, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	monthSpan, err := NewTimeRange(monthStart, monthEnd)
	if err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: %w", err)
	}

	d1Key := partitionKey{
		provider: m.providerName, symbol: symbol, instrument: action.Instrument,
		interval: D1, year: action.Year, month: action.Month,
	}
	parentManifest, _, err := m.loadPartition(ctx, d1Key)
	if err != nil {
		return PublishResult{}, fmt.Errorf("load parent D1 partition: %w", err)
	}

	// d1Cov analyzes the whole month up front — cheaply, since Coverage
	// (unlike Bars) never requires full coverage of what it's asked
	// about, only reports on it — so weekIsD1Ready can be checked for
	// every week without a per-week Coverage call. Bars itself is
	// different: it refuses a query it cannot fully satisfy, so it is
	// called per *ready* week below, never once for the whole month —
	// most months have some not-yet-ready weeks, and a single whole-
	// month Bars call would fail on the first one of those rather than
	// let the ready weeks publish.
	d1Query := BarQuery{Instrument: action.Instrument, Interval: D1, Range: monthSpan}
	d1Cov, err := m.coverage(ctx, d1Query, symbol, nil)
	if err != nil {
		return PublishResult{}, fmt.Errorf("check D1 prerequisite: %w", err)
	}

	cursor, err := firstBoundaryAtOrAfter(m.calendar, monthStart, W1)
	if err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: %w", err)
	}
	var bars []Bar
	for cursor.Before(monthEnd) {
		weekSpan, err := m.calendar.Bar(cursor, W1)
		if err != nil {
			return PublishResult{}, fmt.Errorf("marketdata: build: %w", err)
		}
		if weekIsD1Ready(d1Cov, weekSpan) {
			weekBars, err := m.readAllBars(ctx, BarQuery{Instrument: action.Instrument, Interval: D1, Range: weekSpan})
			if err != nil {
				return PublishResult{}, fmt.Errorf("load D1 bars for week %s: %w", weekSpan.Start(), err)
			}
			if len(weekBars) > 0 {
				agg, err := aggregateBars(weekBars)
				if err != nil {
					return PublishResult{}, fmt.Errorf("marketdata: build: aggregate week %s: %w", weekSpan.Start(), err)
				}
				agg.Time = weekSpan.Start()
				bars = append(bars, agg)
			}
		}
		cursor = weekSpan.End()
	}

	bs := BarSet{Instrument: action.Instrument, Interval: W1, Span: monthSpan, Basis: BasisBid, Bars: bars}
	if err := bs.Validate(); err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: assembled bar set: %w", err)
	}

	manifest := Manifest{
		Provider:      m.providerName,
		Instrument:    action.Instrument,
		Interval:      W1,
		Span:          monthSpan,
		Basis:         BasisBid,
		SchemaVersion: canonicalSchemaVersion,
		// RawFingerprint propagates the parent D1 manifest's own value
		// verbatim: W1 has no raw source of its own, and this is what
		// lets a caller trace a derived dataset all the way back to its
		// ultimate raw source without walking Parent (and composes
		// correctly if a future multi-hop chain ever existed, though
		// ADR-012 stays single-hop for M2).
		RawFingerprint:   parentManifest.RawFingerprint,
		BuilderVersion:   builderVersion,
		ValidatorVersion: validatorVersion,
		ResamplerVersion: resamplerVersionCurrent,
		CalendarVersion:  calendarVersionCurrent,
		BuiltAt:          m.clock.Now(),
		BarCount:         len(bars),
		Parent: &ParentRef{
			Instrument: action.Instrument,
			Interval:   D1,
			Revision:   parentManifest.Revision(),
		},
	}
	if len(bars) > 0 {
		manifest.FirstBar = bars[0].Time
		manifest.LastBar = bars[len(bars)-1].Time
	}
	if err := manifest.Validate(); err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: assembled manifest: %w", err)
	}
	if err := manifest.Matches(bs); err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: %w", err)
	}

	if err := m.publishCanonicalMonth(ctx, symbol, W1, action.Year, action.Month, manifest, bs); err != nil {
		return PublishResult{}, fmt.Errorf("publish: %w", err)
	}

	return PublishResult{Action: action, Manifest: manifest, BarCount: len(bars)}, nil
}

// weekIsD1Ready reports whether cov proves the D1 input for weekSpan is
// fully ready to resample: no PartitionCoverage overlapping weekSpan is
// anything other than PartitionCoverageCurrent (a Missing/Invalid/Stale
// partition contributes no Gaps of its own — coverage.go's own documented
// behavior — so it must be checked separately from Gaps, not inferred
// from their absence), and no Gap overlaps weekSpan either.
func weekIsD1Ready(cov Coverage, weekSpan TimeRange) bool {
	for _, pc := range cov.Partitions {
		monthStart := time.Date(pc.Year, pc.Month, 1, 0, 0, 0, 0, time.UTC)
		monthEnd := monthStart.AddDate(0, 1, 0)
		if monthStart.Before(weekSpan.End()) && monthEnd.After(weekSpan.Start()) && pc.Status != PartitionCoverageCurrent {
			return false
		}
	}
	for _, g := range cov.Gaps {
		if g.Span.Start().Before(weekSpan.End()) && g.Span.End().After(weekSpan.Start()) {
			return false
		}
	}
	return true
}

// aggregateBars folds bars (already in ascending Time order, one
// calendar week's worth of D1 bars) into a single derived Bar, per the
// formula mined from the legacy implementation's own candle
// aggregation (trader-first-try/datamanager/candle_agg.go's
// aggregateWindow): Open is the first bar's Open, High/Low are the
// max/min across the window, Close is the last bar's Close, Ticks is
// the sum, MaxSpread is the max, and AvgSpread is the tick-weighted mean
// — falling back to the zero value when total ticks is 0, matching
// legacy's own guard rather than inventing a different fallback.
//
// The caller sets the returned Bar's Time (the week's own aligned open,
// not derivable from the input bars alone) and is responsible for
// calling Validate; aggregateBars does not validate its own output.
// AvgSpread <= MaxSpread holds by construction: every input's own
// AvgSpread is at most that input's MaxSpread, which is at most the
// aggregate MaxSpread (a max over all inputs), so a weighted average of
// values each bounded by the aggregate MaxSpread cannot exceed it.
func aggregateBars(bars []Bar) (Bar, error) {
	if len(bars) == 0 {
		return Bar{}, fmt.Errorf("marketdata: aggregate: no input bars")
	}

	agg := Bar{
		Open:  bars[0].Open,
		High:  bars[0].High,
		Low:   bars[0].Low,
		Close: bars[len(bars)-1].Close,
	}

	var totalTicks int64
	var weightedSum num.Price
	haveWeighted := false
	for _, b := range bars {
		if b.High.Cmp(agg.High) > 0 {
			agg.High = b.High
		}
		if b.Low.Cmp(agg.Low) < 0 {
			agg.Low = b.Low
		}
		if b.MaxSpread.Cmp(agg.MaxSpread) > 0 {
			agg.MaxSpread = b.MaxSpread
		}
		totalTicks += b.Ticks
		if b.Ticks <= 0 {
			continue
		}
		weight := num.MustParseRate(strconv.FormatInt(b.Ticks, 10))
		contribution, err := b.AvgSpread.MulRate(weight)
		if err != nil {
			return Bar{}, fmt.Errorf("weight spread: %w", err)
		}
		if !haveWeighted {
			weightedSum = contribution
			haveWeighted = true
			continue
		}
		weightedSum, err = weightedSum.Add(contribution)
		if err != nil {
			return Bar{}, fmt.Errorf("sum weighted spread: %w", err)
		}
	}
	agg.Ticks = totalTicks

	if totalTicks > 0 {
		inv, err := num.MustParseRate("1").DivRate(num.MustParseRate(strconv.FormatInt(totalTicks, 10)))
		if err != nil {
			return Bar{}, fmt.Errorf("compute weight inverse: %w", err)
		}
		avg, err := weightedSum.MulRate(inv)
		if err != nil {
			return Bar{}, fmt.Errorf("average spread: %w", err)
		}
		agg.AvgSpread = avg
	}

	return agg, nil
}
