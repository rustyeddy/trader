package backtest_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
)

// fixtureRawRoot is this package's own committed raw archive, copied
// from service/marketdata's identical fixture: EUR/USD H1 and D1 for
// two trading weeks starting 2024-01-07, with a real coverage gap
// around 2024-01-16 (see fixtureSpan and narrowSpan below). Kept as
// this package's own copy rather than a relative reference into
// another package's testdata, the same convention service/marketdata's
// own comment documents.
const fixtureRawRoot = "testdata/raw/oanda"

func eurusdListing(t *testing.T) instrument.Listing {
	t.Helper()
	eurusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	require.NoError(t, err)
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: eurusd,
		Provider:   "oanda",
		Symbol:     "EURUSD",
		Spec:       spec,
		Tradable:   true,
	})
	require.NoError(t, err)
	return listing
}

func eurusdID(t *testing.T) instrument.ID {
	t.Helper()
	return eurusdListing(t).InstrumentID()
}

func copyFixtureRaw(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(fixtureRawRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(fixtureRawRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	require.NoError(t, err)
	return dst
}

// newTestManager returns a *marketdata.Manager wired to a writable
// copy of this package's committed raw fixture and a fresh, empty
// canonical store.
func newTestManager(t *testing.T) *marketdata.Manager {
	t.Helper()
	resolver := instrument.NewMemoryResolver()
	require.NoError(t, resolver.Register(eurusdListing(t)))

	mgr, err := marketdata.New(marketdata.Config{
		Clock:        clock.NewSimulated(time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)),
		StoreRoot:    t.TempDir(),
		RawRoot:      copyFixtureRaw(t),
		Resolver:     resolver,
		ProviderName: "oanda",
	})
	require.NoError(t, err)
	return mgr
}

// fixtureSpan is the exact half-open range this package's raw fixture
// covers. It contains a real, deliberate coverage gap on 2024-01-16
// (present in D1 as a missing calendar month entry; present in H1 as a
// missing session-day partial), so it is used only by tests that want
// that gap.
func fixtureSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 7, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 19, 22, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

// narrowSpan is a one-week sub-range of fixtureSpan with no coverage
// gap in either H1 or D1 (144 H1 bars, 6 D1 bars) — used by every test
// that wants a fully satisfiable requirement set.
func narrowSpan(t *testing.T) marketdata.TimeRange {
	t.Helper()
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 7, 22, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 15, 22, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	return span
}

// publishFixture builds and publishes canonical data for interval over
// span, via Manager's own Plan+Build — the same test-only shortcut
// service/marketdata's tests use, since there is still no dedicated
// publish operation for tests to call directly.
func publishFixture(t *testing.T, mgr *marketdata.Manager, interval marketdata.Interval, span marketdata.TimeRange) {
	t.Helper()
	ctx := context.Background()
	plan, err := mgr.Plan(ctx, marketdata.BarQuery{Instrument: eurusdID(t), Interval: interval, Range: span})
	require.NoError(t, err)
	if len(plan.Actions) == 0 {
		return
	}
	_, err = mgr.Build(ctx, plan)
	require.NoError(t, err)
}

func h1Requirement(t *testing.T) strategy.DataRequirement {
	t.Helper()
	return strategy.DataRequirement{Instrument: eurusdID(t), Interval: marketdata.H1}
}

func d1Requirement(t *testing.T) strategy.DataRequirement {
	t.Helper()
	return strategy.DataRequirement{Instrument: eurusdID(t), Interval: marketdata.D1}
}

func drainReplay(t *testing.T, r *backtest.Replay) []strategy.BarEvent {
	t.Helper()
	ctx := context.Background()
	var events []strategy.BarEvent
	for {
		ev, err := r.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		events = append(events, ev)
	}
	return events
}

func TestNewReplay_MergesMultipleRequirementsInCanonicalOrder(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)
	publishFixture(t, mgr, marketdata.D1, span)

	r, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t), d1Requirement(t)}, span)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	events := drainReplay(t, r)
	require.NotEmpty(t, events)
	require.Len(t, events, 144+6, "144 H1 bars plus 6 D1 bars over narrowSpan")

	for i := 1; i < len(events); i++ {
		prev, cur := events[i-1], events[i]
		require.False(t, cur.Bar.Time.Before(prev.Bar.Time), "events must never move backward in time")
		if cur.Bar.Time.Equal(prev.Bar.Time) {
			// Same-timestamp tie-break: instrument ID, then interval's
			// intrinsic Unit()/Count() — never input order, and never
			// Interval.String(), which is display-only.
			if prev.Instrument.Equal(cur.Instrument) {
				require.True(t, intervalLess(prev.Interval, cur.Interval))
			} else {
				require.Less(t, prev.Instrument.String(), cur.Instrument.String())
			}
		}
	}
}

// intervalLess reports whether a sorts before b under the same
// intrinsic Unit()-then-Count() ordering Replay's own merge tie-break
// uses.
func intervalLess(a, b marketdata.Interval) bool {
	if a.Unit() != b.Unit() {
		return a.Unit() < b.Unit()
	}
	return a.Count() < b.Count()
}

// TestNewReplay_OrderIsIndependentOfRequirementInputOrder is issue
// #212's own review request: constructing the same requirement set in
// a different input order must produce an identical replay sequence,
// since the merge order is a canonical, intrinsic property of the data
// (timestamp, instrument ID, interval) rather than a serialization
// accident of caller-supplied order.
func TestNewReplay_OrderIsIndependentOfRequirementInputOrder(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)
	publishFixture(t, mgr, marketdata.D1, span)

	forward, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t), d1Requirement(t)}, span)
	require.NoError(t, err)
	t.Cleanup(func() { _ = forward.Close() })

	reversed, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{d1Requirement(t), h1Requirement(t)}, span)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reversed.Close() })

	forwardEvents := drainReplay(t, forward)
	reversedEvents := drainReplay(t, reversed)
	require.Equal(t, forwardEvents, reversedEvents)
}

func TestNewReplay_FiltersToRequestedSpanAndRequirements(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)

	r, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t)}, span)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	events := drainReplay(t, r)
	require.Len(t, events, 144)
	for _, ev := range events {
		require.True(t, ev.Instrument.Equal(eurusdID(t)))
		require.Equal(t, marketdata.H1, ev.Interval)
		require.True(t, span.Contains(ev.Bar.Time))
	}
}

func TestNewReplay_WarmupBarsIsIgnoredAndNeverWidensSpan(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)

	req := h1Requirement(t)
	req.WarmupBars = 500 // far more than the fixture even contains

	r, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{req}, span)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	events := drainReplay(t, r)
	require.Len(t, events, 144, "WarmupBars must never silently widen the requested span")
}

func TestNewReplay_RejectsDuplicateRequirement(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)

	_, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t), h1Requirement(t)}, span)
	require.ErrorIs(t, err, backtest.ErrDuplicateRequirement)
}

// TestNewReplay_CoveragePreflightReportsEveryFailure proves the
// preflight is not first-failure-only: two requirements, neither of
// which has any published canonical data, must both appear in the
// resulting CoverageError.
func TestNewReplay_CoveragePreflightReportsEveryFailure(t *testing.T) {
	mgr := newTestManager(t)
	span := fixtureSpan(t)
	// Deliberately never publish anything.

	_, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t), d1Requirement(t)}, span)
	require.ErrorIs(t, err, marketdata.ErrDataUnavailable)

	var covErr *backtest.CoverageError
	require.ErrorAs(t, err, &covErr)
	require.Len(t, covErr.Failures, 2)
}

func TestNewReplay_PartialCoverageFailsTheWholeRequirement(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)
	// D1 left unpublished: only H1 is satisfiable.

	_, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t), d1Requirement(t)}, span)
	require.ErrorIs(t, err, marketdata.ErrDataUnavailable)

	var covErr *backtest.CoverageError
	require.ErrorAs(t, err, &covErr)
	require.Len(t, covErr.Failures, 1)
	require.Equal(t, marketdata.D1, covErr.Failures[0].Requirement.Interval)
}

// TestNewReplay_NonDataUnavailableFailureAbortsImmediately is issue
// #212's own review finding: a Bars failure that is not
// marketdata.ErrDataUnavailable — here, a listing-resolution failure
// for an instrument the resolver never registered — must abort
// NewReplay directly rather than being accumulated into a
// *CoverageError alongside genuine coverage-unavailable failures.
func TestNewReplay_NonDataUnavailableFailureAbortsImmediately(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)

	unregistered := instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	req := strategy.DataRequirement{Instrument: unregistered, Interval: marketdata.H1}

	_, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t), req}, span)
	require.Error(t, err)
	require.NotErrorIs(t, err, marketdata.ErrDataUnavailable)

	var covErr *backtest.CoverageError
	require.False(t, errors.As(err, &covErr), "a resolution failure must not be wrapped in CoverageError")
}

func TestReplay_NextReturnsIOEOFRepeatedlyAfterExhaustion(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)

	r, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t)}, span)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	_ = drainReplay(t, r)

	_, err = r.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)
	_, err = r.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)
}

func TestReplay_CloseIsIdempotentAndNextReturnsIOEOFAfterClose(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)

	r, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t)}, span)
	require.NoError(t, err)

	require.NoError(t, r.Close())
	require.NoError(t, r.Close())

	_, err = r.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)
}

func TestReplay_NextPropagatesCancelledContext(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)

	r, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t)}, span)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = r.Next(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewReplay_PropagatesCancelledContext(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)
	publishFixture(t, mgr, marketdata.H1, span)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := backtest.NewReplay(ctx, mgr, []strategy.DataRequirement{h1Requirement(t)}, span)
	require.ErrorIs(t, err, context.Canceled)
}

// TestNewReplay_NeverReturnsReplayAlongsideAnError proves the
// always-true invariant that NewReplay never returns a non-nil
// *Replay together with an error, for both preflight and open-time
// failures.
func TestNewReplay_NeverReturnsReplayAlongsideAnError(t *testing.T) {
	mgr := newTestManager(t)
	span := narrowSpan(t)

	r, err := backtest.NewReplay(context.Background(), mgr, []strategy.DataRequirement{h1Requirement(t)}, span)
	require.Error(t, err)
	require.Nil(t, r)
}
