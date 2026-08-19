package marketdata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Shared coverage/plan test fixtures ---

// newTestManagerWithRaw returns a Manager rooted at t.TempDir() for both
// the canonical store and the raw archive, wired with testResolver and
// provider "oanda" — the same construction newTestManager (query_test.go)
// uses, plus RawRoot.
func newTestManagerWithRaw(t *testing.T, rawRoot string) *Manager {
	t.Helper()
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     testResolver(t),
		ProviderName: "oanda",
	})
	require.NoError(t, err)
	return m
}

const rawColumnHeaderForTest = "time,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume,complete"

// rawTFToken maps a canonical Interval to the raw file-name token used
// by marketdata/internal/provider/oanda's own fixtures.
func rawTFToken(i Interval) string {
	switch i {
	case M1:
		return "m1"
	case H1:
		return "h1"
	case H4:
		return "h4"
	case D1:
		return "d1"
	default:
		panic(fmt.Sprintf("rawTFToken: unsupported interval %s", i))
	}
}

// rawRow formats one raw-v1 data row for t, matching
// marketdata/internal/provider/oanda's reader format exactly.
func rawRow(at time.Time, complete bool) string {
	c := "true"
	if !complete {
		c = "false"
	}
	return fmt.Sprintf("%s,1.10000,1.10100,1.09900,1.10050,1.10010,1.10110,1.09910,1.10060,100,%s\n",
		at.Format(time.RFC3339), c)
}

// writeRawPartition writes one raw-v1 partition file under root, laid
// out exactly as marketdata/internal/provider/oanda.Inspect expects
// (root/SYMBOL/YYYY/MM/SYMBOL-YYYY-MM-tf.csv), and returns its path.
func writeRawPartition(t *testing.T, root, symbol string, interval Interval, year int, month time.Month, rows ...string) string {
	t.Helper()
	tf := rawTFToken(interval)
	dir := filepath.Join(root, symbol, fmt.Sprintf("%04d", year), fmt.Sprintf("%02d", int(month)))
	require.NoError(t, os.MkdirAll(dir, 0o755))

	content := fmt.Sprintf("# schema=raw-v1 source=oanda instrument=%s tf=%s year=%04d month=%02d\n%s\n",
		symbol, tf, year, int(month), rawColumnHeaderForTest)
	for _, r := range rows {
		content += r
	}

	path := filepath.Join(dir, fmt.Sprintf("%s-%04d-%02d-%s.csv", symbol, year, int(month), tf))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// writeMalformedRawPartition writes a raw partition file with a valid
// schema line but a corrupted column header, so
// marketdata/internal/provider/oanda.Inspect reports it
// PartitionStatusMalformed rather than PartitionStatusOK.
func writeMalformedRawPartition(t *testing.T, root, symbol, tf string, year int, month time.Month) string {
	t.Helper()
	dir := filepath.Join(root, symbol, fmt.Sprintf("%04d", year), fmt.Sprintf("%02d", int(month)))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, fmt.Sprintf("%s-%04d-%02d-%s.csv", symbol, year, int(month), tf))
	content := fmt.Sprintf("# schema=raw-v1 source=oanda instrument=%s tf=%s year=%04d month=%02d\nnot,the,right,header\n",
		symbol, tf, year, int(month))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// rawFingerprint returns the "sha256:<hex>" fingerprint
// oanda.Inspect would compute for the file at path, so a test can
// publish a canonical Manifest whose RawFingerprint is guaranteed to
// (dis)agree with it exactly, without duplicating oanda's own hashing
// logic by hand.
func rawFingerprint(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// publishCanonicalMonth builds and publishes a valid canonical partition
// for (interval, year, month) with the given span, bars, RawFingerprint,
// and optional parent lineage, returning the published Manifest.
func publishCanonicalMonth(t *testing.T, mgr *Manager, interval Interval, year int, month time.Month, span TimeRange, bars []Bar, fingerprint string, parent *ParentRef) Manifest {
	t.Helper()
	m := Manifest{
		Provider:         "oanda",
		Instrument:       eurusd(),
		Interval:         interval,
		Span:             span,
		Basis:            BasisBid,
		SchemaVersion:    1,
		RawFingerprint:   fingerprint,
		BuilderVersion:   "builder-v1",
		ValidatorVersion: "validator-v1",
		ResamplerVersion: noResampler,
		CalendarVersion:  "fxcalendar-v1",
		BuiltAt:          time.Date(year, month, 1, 0, 0, 0, 0, time.UTC),
		BarCount:         len(bars),
	}
	if len(bars) > 0 {
		m.FirstBar = bars[0].Time
		m.LastBar = bars[len(bars)-1].Time
	}
	if parent != nil {
		m.Parent = parent
		m.ResamplerVersion = "resampler-v1"
	}
	bs := BarSet{Instrument: eurusd(), Interval: interval, Span: span, Basis: BasisBid, Bars: bars}
	require.NoError(t, bs.Validate())
	require.NoError(t, m.Validate())
	require.NoError(t, m.Matches(bs))

	key := partitionKey{provider: "oanda", symbol: "EURUSD", instrument: eurusd(), interval: interval, year: year, month: month}
	publishTestPartition(t, mgr, key, m, bs)
	return m
}

// aWeekday returns a UTC hour-aligned Monday, safely inside the FX
// trading week under the default (no-holiday) FXCalendar every hour of
// every weekday is StatusOpen.
func aWeekday(hour int) time.Time {
	return time.Date(2024, time.January, 8, hour, 0, 0, 0, time.UTC) // a Monday
}

// --- Coverage tests ---

func TestCoverage_MissingPartition(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	assert.Equal(t, PartitionCoverageMissing, cov.Partitions[0].Status)
	assert.Nil(t, cov.Partitions[0].Manifest)
}

func TestCoverage_InvalidPartition(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)
	key := partitionKey{provider: "oanda", symbol: "EURUSD", instrument: eurusd(), interval: H1, year: 2024, month: time.January}

	bars := []Bar{barAt(t, aWeekday(0)), barAt(t, aWeekday(1))}
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, bars, validRawFingerprint, nil)

	// Corrupt the published file directly.
	path, err := key.path(mgr.storeRoot)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("not a canonical partition\n"), 0o644))

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	assert.Equal(t, PartitionCoverageInvalid, cov.Partitions[0].Status)
	assert.Nil(t, cov.Partitions[0].Manifest)
}

func TestCoverage_CurrentPartitionRawFingerprintMatches(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	rawPath := writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January,
		rawRow(aWeekday(0), true), rawRow(aWeekday(1), true))
	fp := rawFingerprint(t, rawPath)

	bars := []Bar{barAt(t, aWeekday(0)), barAt(t, aWeekday(1))}
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, bars, fp, nil)

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	pc := cov.Partitions[0]
	assert.Equal(t, PartitionCoverageCurrent, pc.Status)
	require.NotNil(t, pc.Manifest)
	assert.Equal(t, fp, pc.Manifest.RawFingerprint)
}

func TestCoverage_StalePartitionRawFingerprintDisagrees(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January,
		rawRow(aWeekday(0), true), rawRow(aWeekday(1), true))

	bars := []Bar{barAt(t, aWeekday(0)), barAt(t, aWeekday(1))}
	// Published against a fingerprint that does not match the raw file
	// on disk (raw has since changed).
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, bars, validRawFingerprint, nil)

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	assert.Equal(t, PartitionCoverageStale, cov.Partitions[0].Status)
}

func TestCoverage_RawIncompleteCount(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	rawPath := writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January,
		rawRow(aWeekday(0), true), rawRow(aWeekday(1), false), rawRow(aWeekday(2), false))
	fp := rawFingerprint(t, rawPath)

	bars := []Bar{barAt(t, aWeekday(0)), barAt(t, aWeekday(1)), barAt(t, aWeekday(2))}
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, bars, fp, nil)

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	assert.Equal(t, 2, cov.Partitions[0].RawIncompleteCount)
}

func TestCoverage_W1StaleWhenParentRevisionChanges(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir()) // raw not needed for W1
	// A full calendar month, matching deriveAndPublish's own Span
	// convention — not the short aWeekday(0)-aWeekday(4) range other
	// fixtures in this file use, since isStale's W1 staleness check
	// (coverage.go) derives an adjacent D1 partition's (year, month)
	// key from this Span's own End(), which is only meaningful when
	// End() is a real month boundary.
	monthStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	span, err := NewTimeRange(monthStart, monthStart.AddDate(0, 1, 0))
	require.NoError(t, err)

	d1Bars := []Bar{barAt(t, aWeekday(0))}
	d1Manifest := publishCanonicalMonth(t, mgr, D1, 2024, time.January, span, d1Bars, validRawFingerprint, nil)

	w1Bars := []Bar{barAt(t, aWeekday(0))}
	parent := &ParentRef{Instrument: eurusd(), Interval: D1, Revision: d1Manifest.Revision()}
	publishCanonicalMonth(t, mgr, W1, 2024, time.January, span, w1Bars, validRawFingerprint, parent)

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: W1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	assert.Equal(t, PartitionCoverageCurrent, cov.Partitions[0].Status, "parent revision matches: not stale")

	// Rebuild D1 with different content -> different Revision(). This
	// bypasses Manager (there is no publish operation yet, ADR-020), so
	// it must also invalidate the cache entry publishTestPartition's
	// direct store.publish call does not know to evict — exactly the
	// seam barCache.invalidate exists for (see cache.go).
	d1Key := partitionKey{provider: "oanda", symbol: "EURUSD", instrument: eurusd(), interval: D1, year: 2024, month: time.January}
	mgr.cache.invalidate(d1Key)
	d1Bars2 := []Bar{barAt(t, aWeekday(0)), barAt(t, aWeekday(1))}
	publishCanonicalMonth(t, mgr, D1, 2024, time.January, span, d1Bars2, validRawFingerprint, nil)

	cov2, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: W1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov2.Partitions, 1)
	assert.Equal(t, PartitionCoverageStale, cov2.Partitions[0].Status, "parent was rebuilt: now stale")
}

func TestCoverage_GapsExcludeClosedWeekend(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	// Friday 22:00 UTC through Monday 02:00 UTC straddles the weekend
	// closure (FX closes Friday 17:00 NY, reopens Sunday 17:00 NY).
	start := time.Date(2024, time.January, 5, 22, 0, 0, 0, time.UTC) // Friday
	end := time.Date(2024, time.January, 8, 2, 0, 0, 0, time.UTC)    // Monday
	span, err := NewTimeRange(start, end)
	require.NoError(t, err)

	rawPath := writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January, rawRow(start, true))
	fp := rawFingerprint(t, rawPath)

	// No bars at all published in [start, end) other than matching the
	// manifest's own coverage summary requirements, but the manifest's
	// Span itself defines what was "checked" — use one bar to keep the
	// fixture valid, span covering the whole range.
	bars := []Bar{barAt(t, start)}
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, bars, fp, nil)

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	for _, g := range cov.Gaps {
		assert.NotEqual(t, IntervalStateClosed, g.State, "a closed weekend must never appear as a Gap")
	}
}

func TestCoverage_GapsMergeContiguousMissingBoundaries(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	start := aWeekday(0)
	end := aWeekday(5) // five open H1 boundaries: 0,1,2,3,4
	span, err := NewTimeRange(start, end)
	require.NoError(t, err)

	rawPath := writeRawPartition(t, rawRoot, "EURUSD", H1, 2024, time.January, rawRow(start, true), rawRow(aWeekday(4), true))
	fp := rawFingerprint(t, rawPath)

	// Only the first and last hour are present; hours 1,2,3 are missing
	// and must merge into exactly one Gap.
	bars := []Bar{barAt(t, aWeekday(0)), barAt(t, aWeekday(4))}
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, bars, fp, nil)

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov.Gaps, 1, "three consecutive missing hours must merge into one Gap")
	assert.Equal(t, IntervalStateMissing, cov.Gaps[0].State)
	// .Equal(), not assert.Equal: Bar's boundaries are computed relative
	// to the FX daily rollover (ADR-021) and so carry a New York
	// time.Location, while aWeekday constructs its comparison values in
	// UTC — both name the identical instant, but time.Time's struct
	// representation differs by Location even when the instant matches.
	assert.True(t, cov.Gaps[0].Span.Start().Equal(aWeekday(1)))
	assert.True(t, cov.Gaps[0].Span.End().Equal(aWeekday(4)))
}

func TestCoverage_RawRootRequiredForRawBuiltInterval(t *testing.T) {
	mgr := newTestManagerWithRaw(t, "") // no raw root
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	_, err = mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestCoverage_RawRootNotRequiredForW1(t *testing.T) {
	mgr := newTestManagerWithRaw(t, "") // no raw root
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: W1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	assert.Equal(t, PartitionCoverageMissing, cov.Partitions[0].Status)
}

func TestCoverage_InvalidQuery(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	_, err = mgr.Coverage(context.Background(), BarQuery{Instrument: instrument.ID{}, Interval: H1, Range: span})
	assert.ErrorIs(t, err, ErrInvalidQuery)
}

func TestCoverage_CancelledContext(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = mgr.Coverage(ctx, BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestCoverage_NotConfigured(t *testing.T) {
	var mgr Manager
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	_, err = mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestCoverage_UnresolvedInstrument(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	span, err := NewTimeRange(aWeekday(0), aWeekday(4))
	require.NoError(t, err)

	_, err = mgr.Coverage(context.Background(), BarQuery{Instrument: gbpusd(), Interval: H1, Range: span})
	assert.ErrorIs(t, err, instrument.ErrUnknownSymbol)
}

func TestIntervalToRawInterval(t *testing.T) {
	cases := []struct {
		in Interval
		ok bool
	}{
		{M1, true}, {H1, true}, {H4, true}, {D1, true}, {W1, false},
	}
	for _, c := range cases {
		_, ok := intervalToRawInterval(c.in)
		assert.Equal(t, c.ok, ok, "%s", c.in)
	}
}

// TestCoverage_CurrentNotStaleWhenRawUnavailable proves isStale's
// conservative default: a canonical partition is never marked Stale
// merely because its raw counterpart cannot itself be found or trusted
// right now — that condition surfaces separately, through
// PartitionCoverage.RawIncompleteCount / a future Plan action, not by
// silently declaring the canonical data itself out of date.
func TestCoverage_CurrentNotStaleWhenRawUnavailable(t *testing.T) {
	rawRoot := t.TempDir() // no raw files at all
	mgr := newTestManagerWithRaw(t, rawRoot)
	span, err := NewTimeRange(aWeekday(0), aWeekday(1))
	require.NoError(t, err)

	bars := []Bar{barAt(t, aWeekday(0))}
	publishCanonicalMonth(t, mgr, H1, 2024, time.January, span, bars, validRawFingerprint, nil)

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	assert.Equal(t, PartitionCoverageCurrent, cov.Partitions[0].Status)
}

func TestCoverage_W1InProgressWeekIsAGap(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	// mgr's clock (testClock, 2026-01-07) falls inside its own current
	// week, which has therefore not yet elapsed.
	now := mgr.clock.Now()
	weekSpan, err := mgr.calendar.Bar(now, W1)
	require.NoError(t, err)
	require.True(t, weekSpan.Contains(now))

	// Publish a W1 partition for that week — no bars yet, since the
	// week is still forming — with a dummy Parent (isStale treats a
	// D1 partition it cannot itself load as "unverifiable, not stale,"
	// so no D1 fixture is needed to keep this Current).
	parent := &ParentRef{Instrument: eurusd(), Interval: D1, Revision: "dummy-parent-revision"}
	utcStart := weekSpan.Start().UTC()
	publishCanonicalMonth(t, mgr, W1, utcStart.Year(), utcStart.Month(), weekSpan, nil, validRawFingerprint, parent)

	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: W1, Range: weekSpan})
	require.NoError(t, err)
	require.Len(t, cov.Partitions, 1)
	require.Equal(t, PartitionCoverageCurrent, cov.Partitions[0].Status)
	require.Len(t, cov.Gaps, 1)
	assert.Equal(t, IntervalStateInProgress, cov.Gaps[0].State)
}

// TestCoverage_GapNeverStartsBeforeQueryRange is the regression for a
// review finding: a query range starting mid-boundary (D1/W1 are not
// UTC-clock-aligned, so this is routine) must never produce a Gap whose
// Span begins before the range actually asked about — a bar dated
// before the range's own Start would never be returned by Bars anyway
// (BarQuery.Range.Contains excludes it), so it must not be reported as
// missing from this query's results either.
func TestCoverage_GapNeverStartsBeforeQueryRange(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())
	day1, err := mgr.calendar.Bar(aWeekday(0), D1)
	require.NoError(t, err)
	day2, err := mgr.calendar.Bar(day1.End(), D1)
	require.NoError(t, err)

	// Query starts three hours into day1 — mid-boundary — and runs
	// through day2's end. Publish a D1 partition covering both days
	// with no bars at all (both Missing, once elapsed).
	queryStart := day1.Start().Add(3 * time.Hour)
	span, err := NewTimeRange(day1.Start(), day2.End())
	require.NoError(t, err)
	publishCanonicalMonth(t, mgr, D1, day1.Start().UTC().Year(), day1.Start().UTC().Month(), span, nil, validRawFingerprint, nil)

	queryRange, err := NewTimeRange(queryStart, day2.End())
	require.NoError(t, err)
	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: D1, Range: queryRange})
	require.NoError(t, err)

	for _, g := range cov.Gaps {
		assert.False(t, g.Span.Start().Before(queryStart),
			"gap %+v starts before the query range %s", g, queryStart)
	}
	// day1's partial portion is skipped entirely (its bar, if any,
	// could never be returned by Bars for this range); only day2's full
	// boundary is reported.
	require.Len(t, cov.Gaps, 1)
	assert.True(t, cov.Gaps[0].Span.Start().Equal(day2.Start()))
}

// TestCoverage_StraddlingMonthBoundaryNotDoubleCounted is the second,
// more important symptom the same review finding traced to: without
// advancing the cursor to the first boundary at or after clipStart, a
// D1/W1 boundary that straddles a UTC calendar-month division would be
// walked once by the month it starts in and *again* by the following
// month (whose own clipStart falls inside that same boundary) — the
// same Gap reported twice, as two overlapping Span values.
func TestCoverage_StraddlingMonthBoundaryNotDoubleCounted(t *testing.T) {
	mgr := newTestManagerWithRaw(t, t.TempDir())

	// Find a D1 session boundary that starts in January and ends in
	// February (routine: D1 sessions roll over at 17:00 New York, not
	// UTC midnight).
	probe := time.Date(2024, time.January, 31, 23, 0, 0, 0, time.UTC)
	straddle, err := mgr.calendar.Bar(probe, D1)
	require.NoError(t, err)
	require.Equal(t, time.January, straddle.Start().UTC().Month())
	require.Equal(t, time.February, straddle.End().UTC().Month(), "fixture assumption: this boundary must straddle Jan/Feb")

	// January's own manifest/coverage window reaches into February,
	// including the straddling boundary — matching the store's own
	// overlap-not-containment convention (store_csv.go) for a session
	// that pokes past the calendar month.
	janSpan, err := NewTimeRange(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), straddle.End())
	require.NoError(t, err)
	publishCanonicalMonth(t, mgr, D1, 2024, time.January, janSpan, nil, validRawFingerprint, nil)

	febSpan, err := NewTimeRange(straddle.End(), time.Date(2024, 2, 3, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	publishCanonicalMonth(t, mgr, D1, 2024, time.February, febSpan, nil, validRawFingerprint, nil)

	queryRange, err := NewTimeRange(time.Date(2024, 1, 30, 0, 0, 0, 0, time.UTC), time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	cov, err := mgr.Coverage(context.Background(), BarQuery{Instrument: eurusd(), Interval: D1, Range: queryRange})
	require.NoError(t, err)

	sort.Slice(cov.Gaps, func(i, j int) bool { return cov.Gaps[i].Span.Start().Before(cov.Gaps[j].Span.Start()) })
	for i := 1; i < len(cov.Gaps); i++ {
		assert.False(t, cov.Gaps[i].Span.Start().Before(cov.Gaps[i-1].Span.End()),
			"gaps %+v and %+v overlap: the straddling boundary was double-counted", cov.Gaps[i-1], cov.Gaps[i])
	}
}

func TestCoverage_CancelledMidLoop(t *testing.T) {
	rawRoot := t.TempDir()
	mgr := newTestManagerWithRaw(t, rawRoot)
	// Two months touched, so the per-key loop iterates at least twice.
	span, err := NewTimeRange(
		time.Date(2024, time.January, 31, 23, 0, 0, 0, time.UTC),
		time.Date(2024, time.February, 1, 2, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)

	// Enough non-cancelled Err() calls to pass the top-level checks in
	// Coverage/rawInventoryLookup, then cancel inside the per-key loop.
	ctx := &nthCancelContext{Context: context.Background(), remaining: 2}
	_, err = mgr.Coverage(ctx, BarQuery{Instrument: eurusd(), Interval: H1, Range: span})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPartitionCoverageStatus_String(t *testing.T) {
	assert.Equal(t, "missing", PartitionCoverageMissing.String())
	assert.Equal(t, "invalid", PartitionCoverageInvalid.String())
	assert.Equal(t, "stale", PartitionCoverageStale.String())
	assert.Equal(t, "current", PartitionCoverageCurrent.String())
	assert.Contains(t, PartitionCoverageStatus(99).String(), "PartitionCoverageStatus")
}
