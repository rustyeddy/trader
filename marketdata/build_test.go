package marketdata

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// crossedRawRow returns a raw H1 row whose ask_o is below its bid_o — a
// crossed market — matching rawRow's column shape otherwise, so
// normalizeOANDARecord classifies it recordOutcomeSuspicious.
func crossedRawRow(at time.Time) string {
	return fmt.Sprintf("%s,1.10000,1.10100,1.09900,1.10050,1.09000,1.10110,1.09910,1.10060,100,true\n", at.Format(time.RFC3339))
}

// fullWeekD1Bars returns one valid D1 Bar per *open* day boundary within
// weekSpan, walking mgr's own calendar rather than assuming a fixed
// count or fixed UTC offsets. A calendar week spans all seven days
// (Sun-Sat), but only the open ones (Sun-Thu sessions, in FXCalendar's
// model) get a real bar — a naive walk that included the closed
// Fri17:00-Sun17:00 boundary would publish a Bar exactly where Coverage
// expects Closed, producing IntervalStateUnexpected instead of a ready
// week (this was caught by the first version of this helper producing 7
// bars instead of 5).
func fullWeekD1Bars(t *testing.T, mgr *Manager, weekSpan TimeRange) []Bar {
	t.Helper()
	var bars []Bar
	cursor := weekSpan.Start()
	for cursor.Before(weekSpan.End()) {
		daySpan, err := mgr.calendar.Bar(cursor, D1)
		require.NoError(t, err)
		if mgr.calendar.Status(daySpan.Start()) == StatusOpen {
			bars = append(bars, barAt(t, daySpan.Start()))
		}
		cursor = daySpan.End()
	}
	return bars
}

func normalizeAction(year int, month time.Month, interval Interval) Action {
	return Action{Kind: ActionNormalizeCanonical, Instrument: eurusd(), Interval: interval, Year: year, Month: month}
}

func TestBuild_NormalizePublishesFromRaw(t *testing.T) {
	rawRoot := t.TempDir()
	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January,
		rawRow(aWeekday(0), true), rawRow(aWeekday(1), true))
	mgr := newTestManagerWithRaw(t, rawRoot)

	plan := Plan{Actions: []Action{normalizeAction(2024, time.January, H1)}}
	result, err := mgr.Build(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Published, 1)
	assert.Equal(t, 2, result.Published[0].BarCount)
	assert.Empty(t, result.Skipped)

	m := result.Published[0].Manifest
	assert.Equal(t, builderVersion, m.BuilderVersion)
	assert.Equal(t, validatorVersion, m.ValidatorVersion)
	assert.Equal(t, noResampler, m.ResamplerVersion)
	assert.Equal(t, calendarVersionCurrent, m.CalendarVersion)
	assert.Nil(t, m.Parent)

	wantFingerprint, err := oanda.FingerprintPartition(rawRoot, "EURUSD", oanda.RawH1, 2024, time.January)
	require.NoError(t, err)
	assert.Equal(t, wantFingerprint, m.RawFingerprint)

	// The published span always covers the full calendar month, not
	// just the bars actually present.
	assert.True(t, m.Span.Start().Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	assert.True(t, m.Span.End().Equal(time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)))

	span, err := NewTimeRange(aWeekday(0), aWeekday(2))
	require.NoError(t, err)
	reader, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	var got []Bar
	for {
		b, err := reader.Next(context.Background())
		if err != nil {
			break
		}
		got = append(got, b)
	}
	require.Len(t, got, 2)
}

func TestBuild_NormalizeAbortsOnSuspiciousRecordNoPartialPublish(t *testing.T) {
	rawRoot := t.TempDir()
	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January,
		rawRow(aWeekday(0), true), crossedRawRow(aWeekday(1)))
	mgr := newTestManagerWithRaw(t, rawRoot)

	plan := Plan{Actions: []Action{normalizeAction(2024, time.January, H1)}}
	_, err := mgr.Build(context.Background(), plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "suspicious")

	span, err := NewTimeRange(aWeekday(0), aWeekday(2))
	require.NoError(t, err)
	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	assert.Equal(t, PartitionCoverageMissing, cov.Partitions[0].Status, "a failed build must publish nothing at all")
}

func TestBuild_NormalizeExcludesIncompleteRecordsWithoutAborting(t *testing.T) {
	rawRoot := t.TempDir()
	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January,
		rawRow(aWeekday(0), true), rawRow(aWeekday(1), false))
	mgr := newTestManagerWithRaw(t, rawRoot)

	plan := Plan{Actions: []Action{normalizeAction(2024, time.January, H1)}}
	result, err := mgr.Build(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Published, 1)
	assert.Equal(t, 1, result.Published[0].BarCount, "the incomplete record is excluded, not published, but does not abort the build")
}

func TestBuild_NormalizeMissingRawPartitionErrors(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	plan := Plan{Actions: []Action{normalizeAction(2024, time.January, H1)}}
	_, err := mgr.Build(context.Background(), plan)
	assert.Error(t, err)
}

func TestBuild_NormalizeUnresolvedInstrumentErrors(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	plan := Plan{Actions: []Action{{Kind: ActionNormalizeCanonical, Instrument: gbpusd(), Interval: H1, Year: 2024, Month: time.January}}}
	_, err := mgr.Build(context.Background(), plan)
	assert.Error(t, err)
}

func TestBuild_NormalizeReportsEveryBadRecord(t *testing.T) {
	rawRoot := t.TempDir()
	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January,
		crossedRawRow(aWeekday(0)), crossedRawRow(aWeekday(1)))
	mgr := newTestManagerWithRaw(t, rawRoot)

	plan := Plan{Actions: []Action{normalizeAction(2024, time.January, H1)}}
	_, err := mgr.Build(context.Background(), plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 record(s) failed", "every bad record must be named, not just the first")
}

func TestBuild_DeriveUnresolvedInstrumentErrors(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	plan := Plan{Actions: []Action{{Kind: ActionDeriveCanonical, Instrument: gbpusd(), Interval: W1, Year: 2024, Month: time.January}}}
	_, err := mgr.Build(context.Background(), plan)
	assert.Error(t, err)
}

func TestBuild_NormalizeUnsupportedInterval(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	odd, err := NewInterval(UnitHour, 7)
	require.NoError(t, err)
	plan := Plan{Actions: []Action{{Kind: ActionNormalizeCanonical, Instrument: eurusd(), Interval: odd, Year: 2024, Month: time.January}}}
	_, err = mgr.Build(context.Background(), plan)
	assert.Error(t, err)
}

func TestBuild_DerivePublishesCompleteWeekOnly(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	weekSpan, err := mgr.calendar.Bar(aWeekday(0), W1)
	require.NoError(t, err)
	d1Bars := fullWeekD1Bars(t, mgr, weekSpan)
	d1Manifest := publishCanonicalMonth(t, mgr, D1, 2024, time.January, weekSpan, d1Bars, validRawFingerprint, nil)

	plan := Plan{Actions: []Action{{Kind: ActionDeriveCanonical, Instrument: eurusd(), Interval: W1, Year: 2024, Month: time.January}}}
	result, err := mgr.Build(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Published, 1)
	assert.Equal(t, 1, result.Published[0].BarCount, "only the one fully-populated week produces a W1 bar; the rest of January has no D1 data at all")

	w1 := result.Published[0].Manifest
	require.NotNil(t, w1.Parent)
	assert.Equal(t, D1, w1.Parent.Interval)
	assert.Equal(t, d1Manifest.Revision(), w1.Parent.Revision)
	assert.Equal(t, validRawFingerprint, w1.RawFingerprint, "propagated verbatim from the parent D1 manifest")
	assert.Equal(t, resamplerVersionCurrent, w1.ResamplerVersion)

	reader, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: W1, Range: weekSpan})
	require.NoError(t, err)
	b, err := reader.Next(context.Background())
	require.NoError(t, err)
	assert.True(t, b.Time.Equal(weekSpan.Start()))
	assert.Equal(t, d1Bars[0].Open.String(), b.Open.String())
	assert.Equal(t, d1Bars[len(d1Bars)-1].Close.String(), b.Close.String())
}

func TestBuild_DeriveSkipsWeekWithPartialD1(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	weekSpan, err := mgr.calendar.Bar(aWeekday(0), W1)
	require.NoError(t, err)
	d1Bars := fullWeekD1Bars(t, mgr, weekSpan)
	require.True(t, len(d1Bars) > 1, "fixture assumption: a normal week has more than one D1 session")
	partial := d1Bars[:len(d1Bars)-1] // missing the week's last day

	publishCanonicalMonth(t, mgr, D1, 2024, time.January, weekSpan, partial, validRawFingerprint, nil)

	plan := Plan{Actions: []Action{{Kind: ActionDeriveCanonical, Instrument: eurusd(), Interval: W1, Year: 2024, Month: time.January}}}
	result, err := mgr.Build(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Published, 1)
	assert.Equal(t, 0, result.Published[0].BarCount, "a week missing even one D1 day must not be resampled")
}

func TestBuild_DeriveMissingParentErrors(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	plan := Plan{Actions: []Action{{Kind: ActionDeriveCanonical, Instrument: eurusd(), Interval: W1, Year: 2024, Month: time.January}}}
	_, err := mgr.Build(context.Background(), plan)
	assert.Error(t, err)
}

// TestBuild_DeriveWorksWithNoRawRoot is the regression for a design-
// review finding: Build previously required RawRoot unconditionally,
// even though ActionDeriveCanonical (W1 from canonical D1) never reads
// raw data at all — contradicting deriveActionsW1's own documented
// support for a Manager with no RawRoot configured. A W1-only Plan must
// succeed with RawRoot unset.
func TestBuild_DeriveWorksWithNoRawRoot(t *testing.T) {
	mgr := newTestManagerWithRaw(t, "")
	weekSpan, err := mgr.calendar.Bar(aWeekday(0), W1)
	require.NoError(t, err)
	d1Bars := fullWeekD1Bars(t, mgr, weekSpan)
	publishCanonicalMonth(t, mgr, D1, 2024, time.January, weekSpan, d1Bars, validRawFingerprint, nil)

	plan := Plan{Actions: []Action{{Kind: ActionDeriveCanonical, Instrument: eurusd(), Interval: W1, Year: 2024, Month: time.January}}}
	result, err := mgr.Build(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Published, 1)
	assert.Equal(t, 1, result.Published[0].BarCount)
}

// TestBuild_PlanWithOnlySkippedActionsReportsThemWithoutRawRoot is the
// same finding's second half: a Plan whose only entries are outside
// Build's own scope (ActionDownloadRaw/ActionRepairRaw, Sync's
// responsibility) must still report them in Skipped rather than failing
// before they can even be reported, when RawRoot is unset.
func TestBuild_PlanWithOnlySkippedActionsReportsThemWithoutRawRoot(t *testing.T) {
	mgr := newTestManagerWithRaw(t, "")
	plan := Plan{Actions: []Action{
		{Kind: ActionDownloadRaw, Instrument: eurusd(), Interval: H1, Year: 2024, Month: time.January},
	}}
	result, err := mgr.Build(context.Background(), plan)
	require.NoError(t, err)
	assert.Empty(t, result.Published)
	require.Len(t, result.Skipped, 1)
}

// deriveMonthSpan returns a full calendar-month TimeRange, the Span
// convention every real deriveAndPublish/normalizeAndPublish Manifest
// uses (and which w1SpansNextMonth's month-boundary arithmetic depends
// on).
func deriveMonthSpan(t *testing.T, year int, month time.Month) TimeRange {
	t.Helper()
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(start, start.AddDate(0, 1, 0))
	require.NoError(t, err)
	return span
}

// lastWeekSpanOfMonth returns the W1-aligned week whose Start() is the
// last one before monthEnd — the only week a month's own D1 partition
// boundary can ever spill past, per w1SpansNextMonth's own reasoning.
func lastWeekSpanOfMonth(t *testing.T, mgr *Manager, year int, month time.Month) TimeRange {
	t.Helper()
	monthStart := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	spans, _, err := weekSpansForMonth(mgr.calendar, monthStart, monthEnd)
	require.NoError(t, err)
	require.NotEmpty(t, spans)
	return spans[len(spans)-1]
}

// splitBarsAtMonthBoundary partitions bars (a boundary week's full D1
// set, which can span two calendar months) into the subset before
// boundary and the subset at-or-after it, so each half can be published
// under its own month's D1 partition key without a bar ever appearing
// outside its own partition's declared Span.
func splitBarsAtMonthBoundary(bars []Bar, boundary time.Time) (before, atOrAfter []Bar) {
	for _, b := range bars {
		if b.Time.Before(boundary) {
			before = append(before, b)
		} else {
			atOrAfter = append(atOrAfter, b)
		}
	}
	return before, atOrAfter
}

// TestBuild_DeriveSkipsBoundaryWeekWhenNextMonthD1Missing is the
// regression for a design-review finding: the original coverage check
// only queried [monthStart, monthEnd), so it could not see that the
// final week's D1 input spills into a *missing* next-month partition —
// weekIsD1Ready would incorrectly declare that week ready, and
// readAllBars would then fail the whole build trying to read D1 data
// that Coverage never reported as absent. Querying through the full
// week-span union (this issue's fix) must make that week correctly
// unready — skipped, not a build failure.
func TestBuild_DeriveSkipsBoundaryWeekWhenNextMonthD1Missing(t *testing.T) {
	mgr := newTestManagerWithRaw(t, "")
	febStart := time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)
	boundaryWeek := lastWeekSpanOfMonth(t, mgr, 2024, time.January)
	require.True(t, boundaryWeek.End().After(febStart),
		"fixture assumption: January 2024's final W1 week spills into February")

	janBars, _ := splitBarsAtMonthBoundary(fullWeekD1Bars(t, mgr, boundaryWeek), febStart)
	publishCanonicalMonth(t, mgr, D1, 2024, time.January, deriveMonthSpan(t, 2024, time.January), janBars, validRawFingerprint, nil)
	// Deliberately no February D1 partition published.

	plan := Plan{Actions: []Action{{Kind: ActionDeriveCanonical, Instrument: eurusd(), Interval: W1, Year: 2024, Month: time.January}}}
	result, err := mgr.Build(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Published, 1)
	assert.Equal(t, 0, result.Published[0].BarCount, "the boundary week must be skipped, not aborted, when its spillover D1 partition is missing")
}

// TestBuild_DeriveSkipsBoundaryWeekWhenNextMonthD1Invalid mirrors the
// missing-partition case above for a next-month D1 partition that
// exists but is unreadable/corrupted (PartitionCoverageInvalid, not
// PartitionCoverageMissing): the union-range coverage query must see
// that status too, not just outright absence. (deriveAndPublish's own
// D1 coverage query deliberately passes a nil raw inventory lookup —
// the same "cannot verify staleness" precedent already established
// elsewhere in this package — so PartitionCoverageStale specifically is
// never reachable through this particular query; Invalid is the
// non-Current, non-Missing status this query path can actually produce.)
func TestBuild_DeriveSkipsBoundaryWeekWhenNextMonthD1Invalid(t *testing.T) {
	mgr := newTestManagerWithRaw(t, "")
	febStart := time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)
	boundaryWeek := lastWeekSpanOfMonth(t, mgr, 2024, time.January)
	require.True(t, boundaryWeek.End().After(febStart))

	janBars, febBars := splitBarsAtMonthBoundary(fullWeekD1Bars(t, mgr, boundaryWeek), febStart)
	require.NotEmpty(t, febBars, "fixture assumption: the boundary week has at least one February day")
	publishCanonicalMonth(t, mgr, D1, 2024, time.January, deriveMonthSpan(t, 2024, time.January), janBars, validRawFingerprint, nil)
	publishCanonicalMonth(t, mgr, D1, 2024, time.February, deriveMonthSpan(t, 2024, time.February), febBars, validRawFingerprint, nil)

	// Corrupt the published February D1 partition file directly, the
	// same technique TestCoverage_InvalidPartition uses.
	febKey := partitionKey{provider: "oanda", symbol: "EURUSD", instrument: eurusd(), interval: D1, year: 2024, month: time.February}
	path, err := febKey.path(mgr.storeRoot)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("not a canonical partition\n"), 0o644))

	plan := Plan{Actions: []Action{{Kind: ActionDeriveCanonical, Instrument: eurusd(), Interval: W1, Year: 2024, Month: time.January}}}
	result, err := mgr.Build(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Published, 1)
	assert.Equal(t, 0, result.Published[0].BarCount, "the boundary week must be skipped when its spillover D1 partition is invalid")
}

// TestBuild_DeriveCombinesParentLineageAcrossMonthBoundary is the
// regression for a design-review finding: recording only the same-month
// D1 manifest as Parent means rebuilding a genuinely-contributing
// next-month D1 partition would never mark the resulting W1 partition
// stale. With both months' D1 data present and current, the boundary
// week must publish, and its RawFingerprint/Parent.Revision must be a
// composite that changes if *either* contributing D1 partition changes
// — verified here by rebuilding only the February partition and
// confirming Coverage now reports the January W1 partition Stale.
func TestBuild_DeriveCombinesParentLineageAcrossMonthBoundary(t *testing.T) {
	mgr := newTestManagerWithRaw(t, "")
	febStart := time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)
	boundaryWeek := lastWeekSpanOfMonth(t, mgr, 2024, time.January)
	require.True(t, boundaryWeek.End().After(febStart))

	janBars, febBars := splitBarsAtMonthBoundary(fullWeekD1Bars(t, mgr, boundaryWeek), febStart)
	require.NotEmpty(t, febBars, "fixture assumption: the boundary week has at least one February day")
	publishCanonicalMonth(t, mgr, D1, 2024, time.January, deriveMonthSpan(t, 2024, time.January), janBars, validRawFingerprint, nil)
	febManifest := publishCanonicalMonth(t, mgr, D1, 2024, time.February, deriveMonthSpan(t, 2024, time.February), febBars, validRawFingerprint, nil)

	plan := Plan{Actions: []Action{{Kind: ActionDeriveCanonical, Instrument: eurusd(), Interval: W1, Year: 2024, Month: time.January}}}
	result, err := mgr.Build(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, result.Published, 1)
	require.Equal(t, 1, result.Published[0].BarCount, "the boundary week must publish when both contributing D1 partitions are current")

	w1 := result.Published[0].Manifest
	require.NotNil(t, w1.Parent)
	assert.NotEqual(t, febManifest.Revision(), w1.Parent.Revision, "a two-contributor composite must not collapse to either single contributor's own revision")

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: W1, Range: deriveMonthSpan(t, 2024, time.January)})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	assert.Equal(t, PartitionCoverageCurrent, cov.Partitions[0].Status, "composite parent revision must match: not stale immediately after build")

	// Rebuild only February's D1 partition with different content: one
	// extra open day right after the boundary week's own February span.
	febKey := partitionKey{provider: "oanda", symbol: "EURUSD", instrument: eurusd(), interval: D1, year: 2024, month: time.February}
	mgr.cache.invalidate(febKey)
	extraDay, err := mgr.calendar.Bar(boundaryWeek.End(), D1)
	require.NoError(t, err)
	febBars2 := append(append([]Bar{}, febBars...), barAt(t, extraDay.Start()))
	publishCanonicalMonth(t, mgr, D1, 2024, time.February, deriveMonthSpan(t, 2024, time.February), febBars2, validRawFingerprint, nil)

	cov2, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: W1, Range: deriveMonthSpan(t, 2024, time.January)})
	require.NoError(t, err)
	require.Len(t, cov2.Partitions, 1)
	assert.Equal(t, PartitionCoverageStale, cov2.Partitions[0].Status,
		"rebuilding the contributing NEXT-month D1 partition must mark the W1 partition stale, not just the same-month one")
}

func TestBuild_SkipsNonCanonicalActions(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	plan := Plan{Actions: []Action{
		{Kind: ActionDownloadRaw, Instrument: eurusd(), Interval: H1, Year: 2024, Month: time.January},
		{Kind: ActionRepairRaw, Instrument: eurusd(), Interval: H1, Year: 2024, Month: time.January},
	}}
	result, err := mgr.Build(context.Background(), plan)
	require.NoError(t, err)
	assert.Empty(t, result.Published)
	require.Len(t, result.Skipped, 2)
}

func TestBuild_RepublishInvalidatesCache(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)

	span, err := NewTimeRange(aWeekday(0), aWeekday(1))
	require.NoError(t, err)
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, []Bar{barAt(t, aWeekday(0))}, validRawFingerprint, nil)

	reader, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	_, err = reader.Next(context.Background())
	require.NoError(t, err) // primes the cache with the old manifest

	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January, rawRow(aWeekday(0), true))
	plan := Plan{Actions: []Action{normalizeAction(2024, time.January, H1)}}
	_, err = mgr.Build(context.Background(), plan)
	require.NoError(t, err)

	reader2, err := mgr.Bars(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	manifests := reader2.Manifests()
	require.Len(t, manifests, 1)
	assert.NotEqual(t, validRawFingerprint, manifests[0].RawFingerprint,
		"must reflect the freshly-built manifest, not a stale cached one from before the rebuild")
}

func TestBuild_NotConfigured(t *testing.T) {
	var mgr Manager
	_, err := mgr.Build(context.Background(), Plan{})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestBuild_RequiresRawRoot(t *testing.T) {
	mgr := newTestManagerWithRaw(t, "")
	plan := Plan{Actions: []Action{normalizeAction(2024, time.January, H1)}}
	_, err := mgr.Build(context.Background(), plan)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestBuild_CancelledContext(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := mgr.Build(ctx, Plan{Actions: []Action{normalizeAction(2024, time.January, H1)}})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestBuild_CancelledMidLoop(t *testing.T) {
	rawRoot := t.TempDir()
	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January, rawRow(aWeekday(0), true))
	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.February, rawRow(aWeekday(0).AddDate(0, 1, 0), true))
	mgr := newTestManagerWithRaw(t, rawRoot)

	ctx := &nthCancelContext{Context: context.Background(), remaining: 1}
	plan := Plan{Actions: []Action{
		normalizeAction(2024, time.January, H1),
		normalizeAction(2024, time.February, H1),
	}}
	_, err := mgr.Build(ctx, plan)
	assert.ErrorIs(t, err, context.Canceled)
}
