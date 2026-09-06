package marketdata

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata/internal/provider/stooq"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spyListing returns a tradable SPY ETF Listing under provider "stooq",
// symbol "SPY" — the same registration shape
// service/marketdata.RegisterETFInstrument produces, reconstructed
// directly here since this package cannot import service/marketdata
// (it depends on this package; that would be an import cycle).
func spyListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "stooq",
		Venue:      "ARCA",
		Symbol:     "SPY",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func spyID(t *testing.T) instrument.ID {
	t.Helper()
	inst, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	return inst.ID()
}

// testUSEquityCalendarYears spans every year this package's Stooq
// fixtures and fullarchive tests actually touch — the real local
// SPY/AAPL/QQQ archives run from 1984 through the present, so this
// covers comfortably past both ends rather than being tuned to one
// specific fixture's own narrow date range.
func testUSEquityCalendarYears() []int {
	years := make([]int, 0, 2030-1980+1)
	for y := 1980; y <= 2030; y++ {
		years = append(years, y)
	}
	return years
}

// testUSEquityCalendar returns a *USEquityCalendar configured with
// StandardUSEquityHolidays for testUSEquityCalendarYears — the
// Calendar every Stooq-provider test Manager in this file uses (issue
// #296, EQ-03), in place of the default FXCalendar a Manager would
// otherwise fall back to.
func testUSEquityCalendar() *USEquityCalendar {
	return NewUSEquityCalendar(StandardUSEquityHolidays(testUSEquityCalendarYears()...))
}

// newStooqTestManager returns a Manager rooted at t.TempDir(), wired
// with a resolver holding spyListing, provider "stooq", and rawRoot
// pointing at a fresh, empty directory the caller populates via
// stooq.Import before calling Plan/Build.
func newStooqTestManager(t *testing.T, rawRoot string) *Manager {
	t.Helper()
	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(spyListing(t)))
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     r,
		ProviderName: "stooq",
		Calendar:     testUSEquityCalendar(),
	})
	require.NoError(t, err)
	return m
}

// aaplListing returns a tradable AAPL equity Listing under provider
// "stooq" — same shape as spyListing, for issue #298 (EQ-05)'s split-
// adjustment demonstration, which specifically needs a real
// known-split instrument (SPY and QQQ have never split).
func aaplListing(t *testing.T) instrument.Listing {
	t.Helper()
	inst, err := instrument.NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.01"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "stooq",
		Venue:      "NASDAQ",
		Symbol:     "AAPL",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func aaplID(t *testing.T) instrument.ID {
	t.Helper()
	inst, err := instrument.NewEquity("NASDAQ", "AAPL")
	require.NoError(t, err)
	return inst.ID()
}

func newStooqAAPLTestManager(t *testing.T, rawRoot string) *Manager {
	t.Helper()
	r := instrument.NewMemoryResolver()
	require.NoError(t, r.Register(aaplListing(t)))
	m, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     r,
		ProviderName: "stooq",
		Calendar:     testUSEquityCalendar(),
	})
	require.NoError(t, err)
	return m
}

// TestStooqEndToEnd_RejectsWrongCalendarType confirms a Manager
// configured for the "stooq" provider with anything other than a
// *USEquityCalendar fails explicitly and clearly at build time,
// instead of either (a) silently recording a Manifest CalendarVersion
// that names USEquityCalendar when some other Calendar actually ran,
// or (b) failing later with a confusing per-record misalignment error
// whose real cause (a misconfigured Manager, not bad data) is not
// obvious (PR #310 review, Copilot's CalendarVersion finding).
func TestStooqEndToEnd_RejectsWrongCalendarType(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	_, err := stooq.Import(ctx, filepath.Join("internal", "provider", "stooq", "testdata", "spy_us_d_sample.csv"), rawRoot, "SPY")
	require.NoError(t, err)

	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(spyListing(t)))
	mgr, err := New(Config{
		Clock:        testClock(),
		StoreRoot:    t.TempDir(),
		RawRoot:      rawRoot,
		Resolver:     resolver,
		ProviderName: "stooq",
		// Deliberately not a *USEquityCalendar: the default
		// FXCalendar, wrong for this provider.
	})
	require.NoError(t, err)

	span, err := NewTimeRange(
		time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 7, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: spyID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)

	_, err = mgr.Build(ctx, plan)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

// TestStooqEndToEnd_PlanBuildBars is issue #303 (EQ-03A)'s central
// acceptance criterion, exercised directly: a small Stooq CSV fixture
// imported into the raw archive, run through the exact same
// Plan -> Build -> Bars path FX data uses, with marketdata.Manager
// never once referencing anything Stooq-specific — the whole point of
// ADR-047's internal provider seam.
func TestStooqEndToEnd_PlanBuildBars(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	_, err := stooq.Import(ctx, filepath.Join("internal", "provider", "stooq", "testdata", "spy_us_d_sample.csv"), rawRoot, "SPY")
	require.NoError(t, err)

	mgr := newStooqTestManager(t, rawRoot)

	span, err := NewTimeRange(
		time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 7, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: spyID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)

	// Every raw partition Import already wrote must be found "ok" —
	// no ActionDownloadRaw for reason "missing" should ever be
	// produced for data that is already on disk.
	for _, a := range plan.Actions {
		assert.NotEqual(t, ActionDownloadRaw, a.Kind, "unexpected download action: %+v", a)
	}

	buildResult, err := mgr.Build(ctx, plan)
	require.NoError(t, err)
	require.NotEmpty(t, buildResult.Published)
	for _, pr := range buildResult.Published {
		assert.Equal(t, BasisTrade, pr.Manifest.Basis)
		assert.Equal(t, calendarVersionUSEquityV1, pr.Manifest.CalendarVersion)
		assert.Equal(t, "stooq", pr.Manifest.Provider)
	}

	reader, err := mgr.Bars(ctx, query)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	var bars []Bar
	for {
		b, err := reader.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		bars = append(bars, b)
	}

	require.Len(t, bars, 3)
	assert.True(t, bars[0].Time.Equal(time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, "282.8", bars[0].Open.String())
	assert.Equal(t, "283.19", bars[0].High.String())
	assert.Equal(t, "278.85", bars[0].Low.String())
	assert.Equal(t, "282.79", bars[0].Close.String())
	assert.Equal(t, int64(74424000), bars[0].Ticks)
	// AvgSpread/MaxSpread are the zero num.Price for a BasisTrade bar —
	// Stooq has no bid/ask history (ADR-047's documented limitation).
	assert.True(t, bars[0].AvgSpread.IsZero())
	assert.True(t, bars[0].MaxSpread.IsZero())

	assert.True(t, bars[1].Time.Equal(time.Date(2020, 5, 4, 0, 0, 0, 0, time.UTC)))
	assert.True(t, bars[2].Time.Equal(time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)))
}

// TestStooqEndToEnd_RejectsCorruptRawData confirms a Stooq raw partition
// containing an invalid OHLC relationship (High below Low) aborts the
// entire canonical build with no partial publish — normalize_stooq.go's
// reuse of Bar.Validate, exercised through the real Manager.Build path
// rather than only normalizeStooqRecord's own unit tests.
func TestStooqEndToEnd_RejectsCorruptRawData(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	rec := stooq.Record{
		Time:  time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		Open:  num.MustParsePrice("100"),
		High:  num.MustParsePrice("90"), // High < Low: invalid OHLC
		Low:   num.MustParsePrice("95"),
		Close: num.MustParsePrice("92"),
	}
	require.NoError(t, stooq.WritePartition(ctx, rawRoot, "SPY", 2020, time.May, []stooq.Record{rec}, false))

	mgr := newStooqTestManager(t, rawRoot)
	span, err := NewTimeRange(
		time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: spyID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)

	_, err = mgr.Build(ctx, plan)
	assert.Error(t, err)

	// No canonical partition was published for the aborted month.
	_, err = mgr.Bars(ctx, query)
	assert.ErrorIs(t, err, ErrDataUnavailable)
}

// TestStooqEndToEnd_RejectsOutOfOrderRawData confirms an out-of-order
// same-month raw Stooq partition aborts the canonical build with
// errRecordOutOfOrder — the real regression PR #306's review asked
// for. stooq.WritePartition no longer sorts records before writing
// (see its own doc comment), so this test is the actual proof that an
// out-of-order raw source reaches normalizeStooqSequence's ordering
// check instead of being silently pre-sorted into something the
// checker can never see.
func TestStooqEndToEnd_RejectsOutOfOrderRawData(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	// Two records for the same month, written in descending
	// (out-of-order) Time — WritePartition must preserve this order
	// verbatim for the test to mean anything.
	later := stooq.Record{
		Time: time.Date(2020, 5, 4, 0, 0, 0, 0, time.UTC),
		Open: num.MustParsePrice("280.34"), High: num.MustParsePrice("286.44"),
		Low: num.MustParsePrice("278.83"), Close: num.MustParsePrice("285.34"),
	}
	earlier := stooq.Record{
		Time: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		Open: num.MustParsePrice("282.80"), High: num.MustParsePrice("283.19"),
		Low: num.MustParsePrice("278.85"), Close: num.MustParsePrice("282.79"),
	}
	require.NoError(t, stooq.WritePartition(ctx, rawRoot, "SPY", 2020, time.May,
		[]stooq.Record{later, earlier}, false))

	mgr := newStooqTestManager(t, rawRoot)
	span, err := NewTimeRange(
		time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: spyID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)

	// firstBadOutcomeError (build_normalize.go) itemizes every bad
	// record's message via %v, not %w, so errRecordOutOfOrder itself
	// is not reachable through errors.Is here — matching
	// TestStooqEndToEnd_RejectsCorruptRawData's own identical
	// generic-error assertion above. The message text is checked
	// instead, to actually confirm this is the out-of-order rejection
	// and not some other failure.
	_, err = mgr.Build(ctx, plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), errRecordOutOfOrder.Error())

	// No canonical partition was published for the aborted month.
	_, err = mgr.Bars(ctx, query)
	assert.ErrorIs(t, err, ErrDataUnavailable)
}

// TestStooqEndToEnd_AAPLRecordsSplitAdjustedPolicy is issue #298
// (EQ-05)'s real-data demonstration in miniature: a small excerpt of
// AAPL's actual real Stooq daily history spanning its real 2020-08-31
// 4-for-1 split (2020-08-28 through 2020-09-01) shows continuous
// pricing across the split date — no ~4x jump — and the resulting
// canonical Manifest records AdjustmentSplitAdjusted, not
// AdjustmentUnadjusted. TestStooqAAPLFullArchive (gated, real local
// archive) is the full-history version of this same proof; this test
// keeps a deterministic, CI-committed regression for it using a real
// (not synthetic) 3-row excerpt.
func TestStooqEndToEnd_AAPLRecordsSplitAdjustedPolicy(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	_, err := stooq.Import(ctx, filepath.Join("internal", "provider", "stooq", "testdata", "aapl_us_d_sample.csv"), rawRoot, "AAPL")
	require.NoError(t, err)

	mgr := newStooqAAPLTestManager(t, rawRoot)
	span, err := NewTimeRange(
		time.Date(2020, 8, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 10, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: aaplID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)
	buildResult, err := mgr.Build(ctx, plan)
	require.NoError(t, err)
	require.NotEmpty(t, buildResult.Published)
	for _, pr := range buildResult.Published {
		assert.Equal(t, AdjustmentSplitAdjusted, pr.Manifest.AdjustmentPolicy)
	}

	reader, err := mgr.Bars(ctx, query)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	var bars []Bar
	for {
		b, err := reader.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		bars = append(bars, b)
	}
	require.Len(t, bars, 3)

	// The real split date (2020-08-31) sits between bars[0] (08-28) and
	// bars[1] (08-31): a ~4x-unadjusted jump would put the ratio near 4
	// or 0.25; split-adjusted data keeps it close to 1.
	ratio := bars[1].Close.Float64() / bars[0].Close.Float64()
	assert.InDelta(t, 1.0, ratio, 0.2, "close-to-close ratio across the split date = %v, want ~1 (split-adjusted), not ~4 or ~0.25 (unadjusted)", ratio)
}

// TestStooqEndToEnd_HolidayGapReadsCorrectly is issue #299 (EQ-06)'s
// weekend/holiday/session-gap requirement: a real SPY excerpt spanning
// New Year's Day 2020 (2020-01-01, a genuine NYSE closure with no row
// in Stooq's own source file at all — not merely a weekend) proves
// Manager.Bars returns exactly the real trading days on either side,
// with no error and no fabricated bar for the closed day. Reading
// canonical data does not need a trading calendar to behave correctly
// here: it returns whatever is actually stored, nothing more.
func TestStooqEndToEnd_HolidayGapReadsCorrectly(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	_, err := stooq.Import(ctx, filepath.Join("internal", "provider", "stooq", "testdata", "spy_us_d_holiday_sample.csv"), rawRoot, "SPY")
	require.NoError(t, err)

	mgr := newStooqTestManager(t, rawRoot)
	span, err := NewTimeRange(
		time.Date(2019, 12, 30, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 1, 4, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: spyID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)
	_, err = mgr.Build(ctx, plan)
	require.NoError(t, err)

	reader, err := mgr.Bars(ctx, query)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	var bars []Bar
	for {
		b, err := reader.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		bars = append(bars, b)
	}

	require.Len(t, bars, 4, "expected exactly the 4 real trading days; New Year's Day must not appear as a bar")
	wantDates := []time.Time{
		time.Date(2019, 12, 30, 0, 0, 0, 0, time.UTC),
		time.Date(2019, 12, 31, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC),
	}
	for i, want := range wantDates {
		assert.True(t, bars[i].Time.Equal(want), "bar[%d].Time = %v, want %v", i, bars[i].Time, want)
	}
}

// TestStooqCoverage_HolidayGapCorrectlyClosedNotMissing is issue #296
// (EQ-03)'s own payoff, and the direct successor to this file's former
// TestStooqCoverage_HolidayGapPendingEquityCalendar (issue #299,
// EQ-06), which regression-locked the *bug* this issue exists to fix:
// with the default FXCalendar wired in, Manager.Coverage reported two
// spurious "missing" Gaps spanning this exact fixture, each straddling
// a mix of real trading days and the New Year's/weekend closure,
// because FXCalendar's 17:00-America/New_York D1 boundary does not
// align with Stooq's own midnight-UTC daily bars at all.
//
// With USEquityCalendar now wired in (newStooqTestManager), that
// misalignment is gone: USEquityCalendar's D1 boundary is midnight
// UTC, the same anchor Stooq's own bars use, so every real trading day
// in this fixture classifies as IntervalStatePresent and New Year's
// Day 2020 classifies as IntervalStateClosed (a calendar holiday, not
// a gap) — Coverage now reports zero Gaps for this span, exactly as
// issue #299's own sibling test (TestStooqEndToEnd_HolidayGapReadsCorrectly)
// already proved Bars itself was reading correctly all along.
func TestStooqCoverage_HolidayGapCorrectlyClosedNotMissing(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	_, err := stooq.Import(ctx, filepath.Join("internal", "provider", "stooq", "testdata", "spy_us_d_holiday_sample.csv"), rawRoot, "SPY")
	require.NoError(t, err)

	mgr := newStooqTestManager(t, rawRoot)
	span, err := NewTimeRange(
		time.Date(2019, 12, 30, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 1, 4, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: spyID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)
	_, err = mgr.Build(ctx, plan)
	require.NoError(t, err)

	cov, err := mgr.Coverage(ctx, query)
	require.NoError(t, err)

	for _, pc := range cov.Partitions {
		assert.Equal(t, PartitionCoverageCurrent, pc.Status, "%04d-%02d", pc.Year, pc.Month)
	}
	assert.Empty(t, cov.Gaps,
		"New Year's Day 2020 and the surrounding weekend must classify as calendar closures, not Gaps, now that USEquityCalendar's D1 boundary agrees with Stooq's own")
}

// TestStooqCoverage_HalfDayDoesNotStraddleError confirms a query
// spanning a real half day (the day after Thanksgiving) never trips
// ErrIntervalStraddlesBoundary. Session/Status now honestly report the
// real, truncated half-day trading hours (a genuinely narrower window
// than Bar's own midnight-to-midnight D1 span) — but ClassifyInterval
// never actually samples Session/Status against Bar's own span for
// USEquityCalendar at all: uniformStatus (interval_state.go) prefers
// the optional BarSpanClassifier capability, which answers "is this
// whole labeled UTC day an open trading day" directly, decoupled from
// literal endpoint sampling, so the half day's shorter real session
// window never has a chance to fail a containment check that no longer
// applies to it. This is checked through a real Manager.Coverage call,
// not only USEquityCalendar's own unit tests.
func TestStooqCoverage_HalfDayDoesNotStraddleError(t *testing.T) {
	ctx := context.Background()
	rawRoot := t.TempDir()

	// 2020-11-25, 26, 27 (day after Thanksgiving, a half day), and 30 —
	// a real trading week containing one half day.
	records := []stooq.Record{
		{Time: time.Date(2020, 11, 25, 0, 0, 0, 0, time.UTC), Open: num.MustParsePrice("358.00"), High: num.MustParsePrice("360.00"), Low: num.MustParsePrice("357.00"), Close: num.MustParsePrice("359.00")},
		{Time: time.Date(2020, 11, 27, 0, 0, 0, 0, time.UTC), Open: num.MustParsePrice("359.50"), High: num.MustParsePrice("361.00"), Low: num.MustParsePrice("359.00"), Close: num.MustParsePrice("360.50")},
		{Time: time.Date(2020, 11, 30, 0, 0, 0, 0, time.UTC), Open: num.MustParsePrice("360.00"), High: num.MustParsePrice("363.00"), Low: num.MustParsePrice("359.50"), Close: num.MustParsePrice("362.00")},
	}
	require.NoError(t, stooq.WritePartition(ctx, rawRoot, "SPY", 2020, time.November, records, false))

	mgr := newStooqTestManager(t, rawRoot)
	span, err := NewTimeRange(
		time.Date(2020, 11, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 12, 1, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	query := BarQuery{Instrument: spyID(t), Interval: D1, Range: span}

	plan, err := mgr.Plan(ctx, query)
	require.NoError(t, err)
	_, err = mgr.Build(ctx, plan)
	require.NoError(t, err)

	cov, err := mgr.Coverage(ctx, query)
	require.NoError(t, err, "a half day must never trip ErrIntervalStraddlesBoundary")
	assert.Empty(t, cov.Gaps, "Thanksgiving (2020-11-26) is a calendar closure, not a gap; every other queried day has a real bar")
}
