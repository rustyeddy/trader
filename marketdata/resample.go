package marketdata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/rustyeddy/trader/num"
)

// deriveAndPublish executes one ActionDeriveCanonical entry: resamples
// canonical D1 into W1 for the target (instrument, year, month), and
// publishes the result through the canonical store.
//
// # Parent lineage can span two D1 partitions
//
// A W1 partition's Manifest.Parent always names the D1 partition filed
// under the *same* (year, month) key, but its Revision (and the
// Manifest's own RawFingerprint) is a composite over every D1 partition
// that actually contributed a published bar: a week near a month
// boundary can pull some of its D1 bars from the immediately following
// month's file (Bars, #78, already handles that spillover
// transparently when reading), and only the final published week in a
// month can ever do so (see w1SpansNextMonth). combineParentLineage
// folds one or two contributing manifests into single Revision/
// RawFingerprint values; with exactly one contributor (the common,
// non-boundary-spanning case) the composite is that manifest's own
// values, unchanged. isStale (coverage.go) recomputes the identical
// composite from a stored Manifest's own LastBar/Span, so the two never
// disagree about which case applies to a given W1 partition.
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

	// weekSpans collects every calendar week whose Start() falls in
	// [monthStart, monthEnd) — every week this month's W1 partition
	// could publish — before any coverage check or read happens, so
	// coverageEnd (below) can already account for a final week's
	// spillover past monthEnd into next month's D1 partition.
	weekSpans, coverageEnd, err := weekSpansForMonth(m.calendar, monthStart, monthEnd)
	if err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: %w", err)
	}

	// d1Cov analyzes the full union of week spans up front — cheaply,
	// since Coverage (unlike Bars) never requires full coverage of what
	// it's asked about, only reports on it — so weekIsD1Ready can be
	// checked for every week without a per-week Coverage call. Querying
	// through coverageEnd rather than just monthEnd matters: the final
	// week's span commonly extends past monthEnd into next month's D1
	// partition, and weekIsD1Ready must be able to see a missing,
	// stale, or invalid *next*-month partition too, not only this
	// month's own — querying only [monthStart, monthEnd) would leave
	// that adjacent partition's status completely invisible to it.
	//
	// Bars itself is different from Coverage: it refuses a query it
	// cannot fully satisfy, so it is called per *ready* week below,
	// never once for the whole span — most months have some not-yet-
	// ready weeks, and a single call spanning all of them would fail on
	// the first one rather than let the ready weeks publish.
	coverageRange, err := NewTimeRange(monthStart, coverageEnd)
	if err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: %w", err)
	}
	d1Query := BarQuery{Instrument: action.Instrument, Interval: D1, Range: coverageRange}
	d1Cov, err := m.coverage(ctx, d1Query, symbol, nil)
	if err != nil {
		return PublishResult{}, fmt.Errorf("check D1 prerequisite: %w", err)
	}

	var bars []Bar
	for _, weekSpan := range weekSpans {
		if !weekIsD1Ready(d1Cov, weekSpan) {
			continue
		}
		weekBars, err := m.readAllBars(ctx, BarQuery{Instrument: action.Instrument, Interval: D1, Range: weekSpan})
		if err != nil {
			return PublishResult{}, fmt.Errorf("load D1 bars for week %s: %w", weekSpan.Start(), err)
		}
		if len(weekBars) == 0 {
			continue
		}
		agg, err := aggregateBars(weekBars)
		if err != nil {
			return PublishResult{}, fmt.Errorf("marketdata: build: aggregate week %s: %w", weekSpan.Start(), err)
		}
		agg.Time = weekSpan.Start()
		bars = append(bars, agg)
	}

	bs := BarSet{Instrument: action.Instrument, Interval: W1, Span: monthSpan, Basis: BasisBid, Bars: bars}
	if err := bs.Validate(); err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: assembled bar set: %w", err)
	}

	// contributingD1Manifests/combineParentLineage: a published bar's
	// week can draw D1 input from this month's own partition alone, or
	// from that partition plus the immediately following month's — see
	// w1SpansNextMonth's own doc comment for why only the final
	// published week can ever do so. Recording only the same-month
	// parent (this issue's original, simpler design) means rebuilding
	// an adjacent D1 partition that actually contributed would never
	// mark this W1 partition stale; combining every real contributor's
	// Revision/RawFingerprint into one composite closes that gap. See
	// isStale (coverage.go) for the matching recomputation that keeps
	// Coverage's own staleness check in agreement with what was
	// recorded here.
	var lastBarTime time.Time
	if len(bars) > 0 {
		lastBarTime = bars[len(bars)-1].Time
	}
	spansNext, err := w1SpansNextMonth(m.calendar, lastBarTime, monthEnd)
	if err != nil {
		return PublishResult{}, fmt.Errorf("marketdata: build: %w", err)
	}
	contributing := []Manifest{parentManifest}
	if spansNext {
		nextKey := partitionKey{
			provider: m.providerName, symbol: symbol, instrument: action.Instrument,
			interval: D1, year: monthEnd.Year(), month: monthEnd.Month(),
		}
		nextManifest, _, err := m.loadPartition(ctx, nextKey)
		if err != nil {
			return PublishResult{}, fmt.Errorf("load spillover D1 partition: %w", err)
		}
		contributing = append(contributing, nextManifest)
	}
	parentRevision, rawFingerprint := combineParentLineage(contributing)

	manifest := Manifest{
		Provider:      m.providerName,
		Instrument:    action.Instrument,
		Interval:      W1,
		Span:          monthSpan,
		Basis:         BasisBid,
		SchemaVersion: canonicalSchemaVersion,
		// RawFingerprint is the (possibly composite, see above) parent
		// D1 fingerprint(s): W1 has no raw source of its own, and this
		// is what lets a caller trace a derived dataset back to its
		// ultimate raw source without walking Parent.
		RawFingerprint:   rawFingerprint,
		BuilderVersion:   builderVersion,
		ValidatorVersion: validatorVersion,
		ResamplerVersion: resamplerVersionCurrent,
		CalendarVersion:  calendarVersionCurrent,
		BuiltAt:          m.clock.Now(),
		BarCount:         len(bars),
		Parent: &ParentRef{
			Instrument: action.Instrument,
			Interval:   D1,
			Revision:   parentRevision,
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

// weekSpansForMonth walks cal's W1 boundaries starting at the first one
// at or after monthStart, collecting every week whose Start() falls in
// [monthStart, monthEnd), in chronological order. It also returns
// coverageEnd: monthEnd, or the final collected week's own End() when
// that extends past monthEnd — the upper bound a caller needs to query
// D1 coverage/readiness over the complete set of D1 data these weeks
// could possibly draw from, including a final week's spillover into the
// next month's D1 partition. Purely a function of cal and the two
// bounds; it reads no store data and cannot itself detect whether any
// of that D1 data actually exists yet.
func weekSpansForMonth(cal Calendar, monthStart, monthEnd time.Time) ([]TimeRange, time.Time, error) {
	cursor, err := firstBoundaryAtOrAfter(cal, monthStart, W1)
	if err != nil {
		return nil, time.Time{}, err
	}

	var spans []TimeRange
	coverageEnd := monthEnd
	for cursor.Before(monthEnd) {
		weekSpan, err := cal.Bar(cursor, W1)
		if err != nil {
			return nil, time.Time{}, err
		}
		spans = append(spans, weekSpan)
		if weekSpan.End().After(coverageEnd) {
			coverageEnd = weekSpan.End()
		}
		cursor = weekSpan.End()
	}
	return spans, coverageEnd, nil
}

// w1SpansNextMonth reports whether a W1 partition's final published bar
// — lastBar, the zero value when the partition has no bars at all —
// falls in a calendar week that extends past monthEnd. Because
// weekSpansForMonth only ever walks forward from monthStart, and each
// week starts exactly where the previous one ended, at most the last
// week in a month's own sequence can extend past monthEnd; no earlier
// week ever can. Checking only the final published bar's own week is
// therefore both necessary and sufficient to know whether the next
// month's D1 partition was actually a contributor — not merely a
// structural possibility — which is exactly what both deriveAndPublish
// (deciding what to combine while building) and isStale (deciding what
// to recompute while checking an already-published Manifest) need, and
// is why they share this one function rather than each encoding the
// rule separately.
func w1SpansNextMonth(cal Calendar, lastBar time.Time, monthEnd time.Time) (bool, error) {
	if lastBar.IsZero() {
		return false, nil
	}
	lastWeek, err := cal.Bar(lastBar, W1)
	if err != nil {
		return false, err
	}
	return lastWeek.End().After(monthEnd), nil
}

// combineParentLineage folds one or more contributing D1 manifests
// (always in chronological order — see w1SpansNextMonth) into a single
// ParentRef Revision and a single Manifest RawFingerprint.
//
// With exactly one contributor, the composite is simply that manifest's
// own Revision()/RawFingerprint, unchanged — the common case, and
// deliberately identical to what a single-parent design would have
// recorded, so a non-boundary-spanning W1 partition's lineage values
// are unaffected by this scheme existing. With more than one
// contributor, both values become a deterministic sha256 over the
// ordered list of contributors' own values, still qualified "sha256:"
// per RawFingerprint's own documented "raw source artifact(s)"
// (plural) contract — a composite fingerprint of multiple raw artifacts
// is exactly what that contract already anticipates.
func combineParentLineage(contributing []Manifest) (revision, rawFingerprint string) {
	if len(contributing) == 1 {
		return contributing[0].Revision(), contributing[0].RawFingerprint
	}

	var revBuf, fpBuf bytes.Buffer
	for i, man := range contributing {
		if i > 0 {
			revBuf.WriteByte('|')
			fpBuf.WriteByte('|')
		}
		revBuf.WriteString(man.Revision())
		fpBuf.WriteString(man.RawFingerprint)
	}
	revSum := sha256.Sum256(revBuf.Bytes())
	fpSum := sha256.Sum256(fpBuf.Bytes())
	return "sha256:" + hex.EncodeToString(revSum[:]), "sha256:" + hex.EncodeToString(fpSum[:])
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
		// ParseRate, not MustParseRate: b.Ticks is a runtime value read
		// from stored data, not a programmer-controlled constant or
		// fixture (MustParseRate's own documented contract), and an
		// implausibly large tick count must fail this build with an
		// error rather than panic it.
		weight, err := num.ParseRate(strconv.FormatInt(b.Ticks, 10))
		if err != nil {
			return Bar{}, fmt.Errorf("parse ticks as rate: %w", err)
		}
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
		// MustParseRate("1") is a programmer-controlled constant — safe
		// per its own documented contract — but totalTicks is not; see
		// the identical ParseRate-not-MustParseRate reasoning above.
		totalRate, err := num.ParseRate(strconv.FormatInt(totalTicks, 10))
		if err != nil {
			return Bar{}, fmt.Errorf("parse total ticks as rate: %w", err)
		}
		inv, err := num.MustParseRate("1").DivRate(totalRate)
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
