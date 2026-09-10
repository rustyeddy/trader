package smatrend

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy"
	"github.com/rustyeddy/trader/tradertest"
)

var testStart = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// fakeView is a minimal strategy.View test double.
type fakeView struct {
	snap account.Snapshot
}

func (v fakeView) Account() account.Snapshot { return v.snap }

// memoryRecorder is a minimal in-memory journal.Recorder, used only to
// assert on the KindSignal records this package's own decision-
// evidence capability produces, without needing a real storage
// adapter.
type memoryRecorder struct {
	records []journal.Record
}

func (r *memoryRecorder) Record(ctx context.Context, rec journal.Record) error {
	r.records = append(r.records, rec)
	return nil
}

func (r *memoryRecorder) Close() error { return nil }

func mustListing(t *testing.T) instrument.Listing {
	t.Helper()
	l, err := tradertest.NewListing(tradertest.ListingParams{})
	require.NoError(t, err)
	return l
}

func mustSnapshot(t *testing.T, accountID id.AccountID, position *order.Position) account.Snapshot {
	t.Helper()
	var positions []order.Position
	if position != nil {
		positions = []order.Position{*position}
	}
	snap, err := tradertest.NewSnapshot(tradertest.SnapshotParams{
		AccountID: accountID,
		Broker:    "OANDA",
		Positions: positions,
	})
	require.NoError(t, err)
	return snap
}

// bar is one OHLC bar this test's harness feeds the strategy, in an
// easy-to-write literal form.
type bar struct {
	open, high, low, close float64
}

// testHarness bundles one fresh Strategy with everything needed to
// drive it deterministically bar by bar. Unlike strategy/emacross's
// harness, side is not manually toggled by the test: it is derived
// after each bar from the intents the strategy itself just emitted
// (Enter -> Long; a stop triggering is modeled explicitly via
// triggerStop, mirroring backtest.Scheduler's own real
// IntrabarAdvancer wiring, which resolves any stop trigger before
// OnBar ever runs).
type testHarness struct {
	t         *testing.T
	strategy  *Strategy
	instID    instrument.ID
	listing   instrument.Listing
	clock     *clock.Simulated
	ids       *id.Generator
	accountID id.AccountID
	side      order.PositionSide
	// avgPrice overrides the harness-tracked position's own AvgPrice
	// (tradertest.PositionParams' own "1.10000" default otherwise) —
	// needed by any test exercising probationTrendExitRule's trail-
	// activation threshold, which is computed directly from the real
	// AvgPrice Strategy reads via currentPositionAvgPrice (issue #349).
	avgPrice string
}

func newTestHarness(t *testing.T, config Config) *testHarness {
	t.Helper()
	return newTestHarnessWithJournal(t, config, nil, id.RunID{})
}

func newTestHarnessWithJournal(t *testing.T, config Config, rec journal.Recorder, runID id.RunID) *testHarness {
	t.Helper()
	listing := mustListing(t)
	instID := listing.InstrumentID()

	s, err := New(instID, marketdata.D1, config)
	require.NoError(t, err)

	c := clock.NewSimulated(testStart)
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))
	accountID := tradertest.MustAccountID(ids)

	require.NoError(t, s.Start(context.Background(), strategy.Environment{
		Clock:   c,
		Intents: strategy.NewIntentFactory(c, ids, id.Source(Name)),
		Logger:  logging.Discard(),
		Journal: rec,
		RunID:   runID,
	}))

	return &testHarness{t: t, strategy: s, instID: instID, listing: listing, clock: c, ids: ids, accountID: accountID}
}

// triggerStop forces the harness's own tracked position to Flat, as if
// backtest.Scheduler's real IntrabarAdvancer had just closed it before
// this bar's OnBar call — the exact "position did not survive the bar"
// case OnBar's own doc comment describes. It does not touch the
// strategy's internal trailing-stop bookkeeping directly: that is only
// ever reset via onFlat, driven by the next onBar call observing Flat.
func (h *testHarness) triggerStop() {
	h.side = order.Flat
}

// buildBar constructs bar barNum's own BarEvent/View pair (bars are
// spaced one D1 interval apart from testStart), reflecting the
// harness's current position, and advances h's clock to that bar's own
// time.
func (h *testHarness) buildBar(barNum int, b bar) (strategy.BarEvent, strategy.View, time.Time) {
	h.t.Helper()
	barTime := testStart.AddDate(0, 0, barNum-1)
	require.NoError(h.t, h.clock.AdvanceTo(barTime))

	var position *order.Position
	if h.side != order.Flat {
		p, err := tradertest.NewPosition(tradertest.PositionParams{
			AccountID: h.accountID,
			Listing:   h.listing,
			Side:      h.side,
			AvgPrice:  h.avgPrice,
		})
		require.NoError(h.t, err)
		position = &p
	}

	mp := func(f float64) num.Price { return num.MustParsePrice(priceText(f)) }
	mdBar := marketdata.Bar{
		Time:  barTime,
		Open:  mp(b.open),
		High:  mp(b.high),
		Low:   mp(b.low),
		Close: mp(b.close),
	}
	event := strategy.BarEvent{Instrument: h.instID, Interval: marketdata.D1, Bar: mdBar}
	view := fakeView{snap: mustSnapshot(h.t, h.accountID, position)}
	return event, view, barTime
}

func priceText(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func (h *testHarness) onBar(barNum int, b bar) ([]order.Intent, time.Time) {
	h.t.Helper()
	event, view, barTime := h.buildBar(barNum, b)
	intents, err := h.strategy.OnBar(context.Background(), event, view)
	require.NoError(h.t, err)

	// Derive the harness's own tracked position from the intents just
	// emitted — the same "this test plays the role of the pipeline"
	// convention strategy/emacross's harness uses, extended for
	// smatrend's own Enter-only-ever-opens-long, AdjustStop-never-
	// changes-side vocabulary.
	for _, in := range intents {
		switch in.Kind {
		case order.IntentEnter:
			h.side = order.Long
		case order.IntentExit:
			h.side = order.Flat
		}
	}
	return intents, barTime
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{SMAPeriod: 0, TrailingStopPercent: num.MustParseRate("0.10")})
	require.Error(t, err)
}

func TestNew_RejectsUnknownExitRuleName(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10"), ExitRuleName: "not-a-real-rule"})
	require.Error(t, err)
}

func TestNew_RejectsUnknownReEntryRuleName(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10"), ReEntryRuleName: "not-a-real-rule"})
	require.Error(t, err)
}

// TestNew_SMACrossExitRuleDoesNotRequireTrailingStopPercent proves
// TrailingStopPercent's own Validate check is skipped for any
// ExitRule other than "trailing-stop"/"probation-trend" (issue #347):
// a zero-value TrailingStopPercent, which would fail Validate under
// the default rule, must be accepted under "sma-cross".
func TestNew_SMACrossExitRuleDoesNotRequireTrailingStopPercent(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{SMAPeriod: 3, ExitRuleName: "sma-cross"})
	require.NoError(t, err)
}

func TestNew_RejectsUnknownInitialEntryModeName(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10"), InitialEntryModeName: "not-a-real-mode"})
	require.Error(t, err)
}

func TestNew_AboveSMAInitialEntryMode(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10"), InitialEntryModeName: "above-sma"})
	require.NoError(t, err)
}

// TestNew_ProbationTrendRequiresTrailingStopPercent proves
// "probation-trend" is treated the same as "trailing-stop" for
// TrailingStopPercent's own bounds check (issue #349): its TRENDING
// phase reuses the identical stop-fraction math.
func TestNew_ProbationTrendRequiresTrailingStopPercent(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{
		SMAPeriod:           3,
		ExitRuleName:        "probation-trend",
		InitialStopBelowSMA: num.MustParseRate("0.01"),
		TrailActivationGain: num.MustParseRate("0.05"),
	})
	require.Error(t, err, "probation-trend needs TrailingStopPercent for its own trending phase")
}

func TestNew_ProbationTrendRejectsNonPositiveInitialStopBelowSMA(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{
		SMAPeriod:           3,
		ExitRuleName:        "probation-trend",
		TrailingStopPercent: num.MustParseRate("0.10"),
		InitialStopBelowSMA: num.MustParseRate("0"),
		TrailActivationGain: num.MustParseRate("0.05"),
	})
	require.Error(t, err)
}

func TestNew_ProbationTrendRejectsNonPositiveTrailActivationGain(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{
		SMAPeriod:           3,
		ExitRuleName:        "probation-trend",
		TrailingStopPercent: num.MustParseRate("0.10"),
		InitialStopBelowSMA: num.MustParseRate("0.01"),
		TrailActivationGain: num.MustParseRate("0"),
	})
	require.Error(t, err)
}

func TestNew_ProbationTrendAcceptsValidConfig(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{
		SMAPeriod:           3,
		ExitRuleName:        "probation-trend",
		TrailingStopPercent: num.MustParseRate("0.10"),
		InitialStopBelowSMA: num.MustParseRate("0.01"),
		TrailActivationGain: num.MustParseRate("0.05"),
	})
	require.NoError(t, err)
}

func TestStrategy_StartRejectsJournalWithoutRunID(t *testing.T) {
	listing := mustListing(t)
	s, err := New(listing.InstrumentID(), marketdata.D1, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10")})
	require.NoError(t, err)

	c := clock.NewSimulated(testStart)
	ids := id.NewGenerator(c, id.NewDeterministic(1, 2))

	err = s.Start(context.Background(), strategy.Environment{
		Clock:   c,
		Intents: strategy.NewIntentFactory(c, ids, id.Source(Name)),
		Logger:  logging.Discard(),
		Journal: &memoryRecorder{},
		// RunID intentionally left zero.
	})
	require.Error(t, err)
}

func TestStrategy_Describe(t *testing.T) {
	h := newTestHarness(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10")})
	desc := h.strategy.Describe()

	assert.Equal(t, Name, desc.Name)
	assert.Equal(t, Version, desc.Version)
	require.Len(t, desc.Requirements, 1)
	assert.Equal(t, h.instID, desc.Requirements[0].Instrument)
	assert.Equal(t, marketdata.D1, desc.Requirements[0].Interval)
	assert.Equal(t, 3, desc.Requirements[0].WarmupBars)
}

// TestStrategy_WarmupEmitsNoIntents proves no intent is ever returned
// before the SMA is ready, however price behaves.
func TestStrategy_WarmupEmitsNoIntents(t *testing.T) {
	h := newTestHarness(t, Config{SMAPeriod: 5, TrailingStopPercent: num.MustParseRate("0.10")})

	closes := []float64{100, 101, 102, 103, 104}
	for i, c := range closes {
		intents, _ := h.onBar(i+1, bar{open: c, high: c, low: c, close: c})
		assert.Emptyf(t, intents, "bar %d (warm-up) must not emit any intent", i+1)
	}
}

// TestStrategy_NoEntryWithoutFreshCross proves that remaining
// continuously above the SMA, with no prior at-or-below bar, never by
// itself produces an entry — only a genuine cross does.
func TestStrategy_NoEntryWithoutFreshCross(t *testing.T) {
	h := newTestHarness(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10")})

	// Every close strictly increasing and already above a slowly-rising
	// SMA from the very first ready bar: no prior "at or below" state
	// ever existed for update to compare against, so bar 3 (the first
	// ready bar) itself must not signal a cross, since crossState's
	// zero value starts with have=false.
	closes := []float64{100, 101, 102, 103, 104, 105}
	for i, c := range closes {
		intents, _ := h.onBar(i+1, bar{open: c, high: c, low: c, close: c})
		assert.Emptyf(t, intents, "bar %d must not emit any intent absent a genuine cross", i+1)
	}
}

// TestStrategy_EntersOnCrossAbove proves a genuine at-or-below -> above
// transition enters long, on the bar the cross is detected — execution
// timing (next tradable session) is Scheduler's own responsibility
// (issue #214), not smatrend's.
func TestStrategy_EntersOnCrossAbove(t *testing.T) {
	h := newTestHarness(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10")})

	// SMA(3) closes: 100,100,100 (SMA=100, warm-up) then 99 (below),
	// then 102 (above: cross).
	bars := []float64{100, 100, 100, 99, 102}
	for i, c := range bars[:4] {
		intents, _ := h.onBar(i+1, bar{open: c, high: c, low: c, close: c})
		assert.Emptyf(t, intents, "bar %d must not emit any intent yet", i+1)
	}
	intents, _ := h.onBar(5, bar{open: 102, high: 102, low: 102, close: 102})
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentEnter, intents[0].Kind)
	assert.Equal(t, order.Buy, intents[0].Side)
}

// TestStrategy_OnePositionAtATime proves that, once long, further
// price action never emits a second Enter intent — only AdjustStop (or
// nothing) is possible while a position is open.
func TestStrategy_OnePositionAtATime(t *testing.T) {
	h := newTestHarness(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10")})

	for i, c := range []float64{100, 100, 100, 99} {
		h.onBar(i+1, bar{open: c, high: c, low: c, close: c})
	}
	intents, _ := h.onBar(5, bar{open: 102, high: 102, low: 102, close: 102})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentEnter, intents[0].Kind)

	// Ordered slice, not a map: bars must be fed in chronological order
	// (h.clock.AdvanceTo rejects going backward).
	for _, bc := range []struct {
		barNum int
		close  float64
	}{{6, 103}, {7, 104}, {8, 105}} {
		intents, _ := h.onBar(bc.barNum, bar{open: bc.close, high: bc.close, low: bc.close, close: bc.close})
		for _, in := range intents {
			assert.NotEqual(t, order.IntentEnter, in.Kind, "must never enter a second position while already long")
		}
	}
}

// enterLong is a shared fixture for every trailing-stop test below: it
// warms up a period-3 SMA and enters long exactly on bar 5, returning
// the harness ready for bar 6 onward.
func enterLong(t *testing.T) *testHarness {
	t.Helper()
	return enterLongWithConfig(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10")})
}

// enterLongWithConfig is enterLong parameterized by cfg, for a test
// that needs a non-default ExitRuleName/ReEntryRuleName while
// otherwise reusing the identical warm-up/entry fixture.
func enterLongWithConfig(t *testing.T, cfg Config) *testHarness {
	t.Helper()
	h := newTestHarness(t, cfg)
	for i, c := range []float64{100, 100, 100, 99} {
		h.onBar(i+1, bar{open: c, high: c, low: c, close: c})
	}
	intents, _ := h.onBar(5, bar{open: 102, high: 102, low: 102, close: 102})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentEnter, intents[0].Kind)
	h.side = order.Long
	return h
}

// TestStrategy_EstablishesInitialStopOnFirstLongBar proves the very
// first bar observed Long (the entry's own fill bar) establishes a
// high-water mark from that bar's own High and emits the initial
// AdjustStop at 90% of it.
func TestStrategy_EstablishesInitialStopOnFirstLongBar(t *testing.T) {
	h := enterLong(t)

	intents, _ := h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	require.NotNil(t, intents[0].StopPrice)
	assert.Equal(t, "99", intents[0].StopPrice.String(), "90%% of the 110 high-water mark")
}

// TestStrategy_HighWaterMarkRatchetsUpward proves a new bar high above
// the prior high-water mark raises the stop.
func TestStrategy_HighWaterMarkRatchetsUpward(t *testing.T) {
	h := enterLong(t)
	h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105}) // HWM 110, stop 99

	intents, _ := h.onBar(7, bar{open: 106, high: 120, low: 105, close: 118}) // HWM 120, stop 108
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	assert.Equal(t, "108", intents[0].StopPrice.String())
}

// TestStrategy_StopNeverMovesDownward proves a bar whose High does not
// exceed the existing high-water mark emits no intent at all — never a
// downward AdjustStop.
func TestStrategy_StopNeverMovesDownward(t *testing.T) {
	h := enterLong(t)
	h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105}) // HWM 110, stop 99

	intents, _ := h.onBar(7, bar{open: 106, high: 108, low: 100, close: 101}) // High below HWM
	assert.Empty(t, intents, "a lower bar high must never move the stop")
}

// TestStrategy_NoSameBarStopTighteningOrLookahead proves the stop
// computed from bar N's own High is not itself evaluated against bar
// N's own price action by smatrend — that is Scheduler/
// IntrabarAdvancer's own responsibility, entirely outside this
// package. This test asserts the one thing smatrend itself controls:
// the AdjustStop intent for bar N is built from bar N's own High
// (already-known, completed information), never from any later bar,
// and OnBar performs no comparison of the newly computed stop against
// bar N's own Low/Close at all (there is no code path in this package
// that could reject or fast-track a fill — that vocabulary belongs to
// order.IntentAdjustStop's consumer, not its producer).
func TestStrategy_NoSameBarStopTighteningOrLookahead(t *testing.T) {
	h := enterLong(t)

	// This bar's own Low (90) is far below the stop smatrend is about
	// to compute (99, from this same bar's High of 110) — if smatrend
	// evaluated its own freshly computed stop against this bar's own
	// Low, it would have to react to that breach itself. It must not:
	// OnBar's only observable behavior is the AdjustStop intent it
	// returns, with no error and no special-cased "already breached"
	// signal.
	intents, _ := h.onBar(6, bar{open: 103, high: 110, low: 90, close: 105})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	assert.Equal(t, "99", intents[0].StopPrice.String())
}

// TestStrategy_NormalStopHitResetsToFlat models backtest.Scheduler's
// own real behavior (issue #338): once IntrabarAdvancer closes the
// position, the next OnBar call observes Flat and must not itself try
// to exit again or emit a stray AdjustStop.
func TestStrategy_NormalStopHitResetsToFlat(t *testing.T) {
	h := enterLong(t)
	h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105}) // stop ratchets to 99

	h.triggerStop() // as if the broker just closed the position on bar 7

	intents, _ := h.onBar(7, bar{open: 98, high: 99, low: 95, close: 96})
	assert.Empty(t, intents, "flat with no fresh cross must emit nothing")
}

// TestStrategy_NoReentryWithoutFreshCrossAfterStop proves EQS-01's own
// central re-entry rule: after a stop exit, remaining above the SMA
// (never returning to at-or-below it) must not by itself re-enter.
func TestStrategy_NoReentryWithoutFreshCrossAfterStop(t *testing.T) {
	h := enterLong(t)
	h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105})
	h.triggerStop()

	// Every subsequent close stays comfortably above the still-low SMA
	// (SMA period 3 over a short recent window; using highs/lows well
	// above the SMA guarantees "above" every bar).
	for _, bc := range []struct {
		barNum int
		close  float64
	}{{7, 106}, {8, 107}, {9, 108}} {
		intents, _ := h.onBar(bc.barNum, bar{open: bc.close, high: bc.close + 1, low: bc.close - 1, close: bc.close})
		assert.Emptyf(t, intents, "bar %d must not re-enter without a fresh cross", bc.barNum)
	}
}

// TestStrategy_FreshCrossPermitsReentry proves that once price actually
// returns to at-or-below the SMA and then crosses back above it, a new
// Enter intent is emitted, with a fresh trailing-stop episode (no stale
// high-water mark/stop carried over).
func TestStrategy_FreshCrossPermitsReentry(t *testing.T) {
	h := enterLong(t)
	h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105}) // HWM 110, stop 99
	h.triggerStop()

	// Bar 7: close 95, at-or-below the SMA (SMA(3) over bars 5,6,7 =
	// (102+105+95)/3 = 100.667 -- close 95 is below it).
	intents, _ := h.onBar(7, bar{open: 98, high: 99, low: 94, close: 95})
	assert.Empty(t, intents)

	// Bar 8: close 110, comfortably above the now-updated SMA(3) over
	// bars 6,7,8 = (105+95+110)/3 = 103.33.
	intents, _ = h.onBar(8, bar{open: 100, high: 111, low: 99, close: 110})
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentEnter, intents[0].Kind)
	h.side = order.Long

	// The new trailing episode must start fresh: bar 9's own High (111,
	// lower than the *previous* episode's 110 high-water mark would
	// have compared against 99.9, coincidentally close) is what matters
	// here is that the stop is computed from *this* episode's own
	// high-water mark, established starting bar 9 — not left over from
	// before the stop exit.
	intents, _ = h.onBar(9, bar{open: 108, high: 120, low: 107, close: 115})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	assert.Equal(t, "108", intents[0].StopPrice.String(), "90%% of this fresh episode's own 120 high")
}

// TestStrategy_DecisionEvidenceRecordsSignals proves KindSignal records
// are journaled for entry and stop-ratchet decisions when a Journal is
// configured.
func TestStrategy_DecisionEvidenceRecordsSignals(t *testing.T) {
	rec := &memoryRecorder{}
	runID := mustRunID(t)
	h := newTestHarnessWithJournal(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10")}, rec, runID)

	for i, c := range []float64{100, 100, 100, 99} {
		h.onBar(i+1, bar{open: c, high: c, low: c, close: c})
	}
	h.onBar(5, bar{open: 102, high: 102, low: 102, close: 102})
	h.side = order.Long
	h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105})

	require.Len(t, rec.records, 2)
	assert.Equal(t, journal.KindSignal, rec.records[0].Kind)
	assert.Equal(t, "enter-long", rec.records[0].Signal.Values["action"])
	assert.Equal(t, journal.KindSignal, rec.records[1].Kind)
	assert.Equal(t, "adjust-stop", rec.records[1].Signal.Values["action"])
	assert.Equal(t, "99", rec.records[1].Signal.Values["stop_price"])
}

func mustRunID(t *testing.T) id.RunID {
	t.Helper()
	c := clock.NewSimulated(testStart)
	ids := id.NewGenerator(c, id.NewDeterministic(9, 9))
	runID, err := id.GenerateRunID(ids)
	require.NoError(t, err)
	return runID
}

// TestStrategy_ReclaimExitPriceReEntersWithoutFreshCross is issue
// #347's own central proof: with ReEntryRuleName "reclaim-exit-price"
// configured, the strategy re-enters purely because price closes back
// above the level it was stopped out at — even on the very same bar
// the stop triggers, and even though price never dipped back below
// the SMA at all (so under the default "fresh-cross" rule, no
// re-entry would ever have been possible without a later genuine
// cross).
func TestStrategy_ReclaimExitPriceReEntersWithoutFreshCross(t *testing.T) {
	h := enterLongWithConfig(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10"), ReEntryRuleName: "reclaim-exit-price"})
	h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105}) // ratchets stop to 99 (90% of 110)

	h.triggerStop() // as if a broker-side intrabar wick to 99 stopped it out, closing well above that

	// This bar's own Close (104) remains above the SMA the whole time
	// (no fresh cross), yet is above the 99 exit price the stop
	// triggered at.
	intents, _ := h.onBar(7, bar{open: 100, high: 106, low: 99, close: 104})
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentEnter, intents[0].Kind, "reclaim-exit-price must re-enter without any fresh SMA cross")
}

// TestStrategy_ReclaimExitPriceDoesNotEnterBelowExitPrice proves the
// rule does not fire merely because price is moving upward — it must
// actually close back above the specific exit price.
func TestStrategy_ReclaimExitPriceDoesNotEnterBelowExitPrice(t *testing.T) {
	h := enterLongWithConfig(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10"), ReEntryRuleName: "reclaim-exit-price"})
	h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105}) // ratchets stop to 99
	h.triggerStop()

	intents, _ := h.onBar(7, bar{open: 95, high: 98, low: 90, close: 96}) // close (96) still below the 99 exit price
	assert.Empty(t, intents, "must not re-enter before price actually reclaims the exit price")
}

// TestStrategy_BreakoutReEntryRequiresExceedingSinceExitHigh proves
// the breakout rule is genuinely stricter than reclaim-exit-price: a
// close that reclaims the old exit price but has not yet exceeded the
// high observed since the exit must not enter.
func TestStrategy_BreakoutReEntryRequiresExceedingSinceExitHigh(t *testing.T) {
	h := enterLongWithConfig(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10"), ReEntryRuleName: "breakout"})
	h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105}) // ratchets stop to 99
	h.triggerStop()

	// Exit bar's own High (106) seeds sinceExitHigh. This bar's close
	// (104) is above the 99 exit price but below that 106 high.
	intents, _ := h.onBar(7, bar{open: 100, high: 106, low: 99, close: 104})
	assert.Empty(t, intents, "reclaiming the exit price alone must not be enough for the breakout rule")

	// A later bar closing above the since-exit high does enter.
	intents, _ = h.onBar(8, bar{open: 105, high: 108, low: 104, close: 107})
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentEnter, intents[0].Kind)
}

// TestStrategy_ReEntryGatedByAboveSMAEvenForNonDefaultRule is the SMA
// Long Hold playbook's own re-entry invariant (PR #348 review): after
// a stop-out, a configured ReEntryRule only ever gets to decide *how*
// to resume within the still-bullish (above-SMA) regime — it can
// never fire while price is below the SMA, regardless of which rule
// is configured. Below the SMA, only a fresh cross re-enters, exactly
// as if fresh-cross were configured.
//
// reclaim-exit-price is used here specifically because, taken alone
// (ignoring the SMA), it would otherwise re-enter purely on reclaiming
// the exit price even while price sits below the SMA — this test
// proves the central gate in onFlat overrides that.
func TestStrategy_ReEntryGatedByAboveSMAEvenForNonDefaultRule(t *testing.T) {
	h := enterLongWithConfig(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10"), ReEntryRuleName: "reclaim-exit-price"})
	h.onBar(6, bar{open: 103, high: 110, low: 102, close: 105}) // ratchets stop to 99
	h.triggerStop()

	// Close (100) reclaims the 99 exit price, but SMA(bar4,5,6) = 102
	// puts this bar below the SMA — the central gate must block entry
	// even though reclaim-exit-price's own logic alone would allow it.
	intents, _ := h.onBar(7, bar{open: 98, high: 101, low: 97, close: 100})
	assert.Empty(t, intents, "must not re-enter below the SMA even though the exit price was reclaimed")

	// A later bar with a genuine fresh cross back above the SMA still
	// re-enters normally.
	intents, _ = h.onBar(8, bar{open: 100, high: 105, low: 99, close: 104})
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentEnter, intents[0].Kind, "a fresh cross above the SMA must still re-enter regardless of the configured ReEntryRule")
}

// ambiguousExitRule is a test double proving Strategy.onLong rejects
// an ExitRule that returns both ExitNow and NewStop set on the same
// decision (PR #348 review): ExitDecision's own doc comment requires
// these to be mutually exclusive, and a well-behaved implementation
// can never trigger this path on its own — hence registering a
// deliberately misbehaving fake rather than finding a real rule that
// does it.
type ambiguousExitRule struct{}

func (ambiguousExitRule) OnEntry(marketdata.Bar, num.Price) {}

func (ambiguousExitRule) OnLongBar(bar marketdata.Bar, _ float64) (ExitDecision, error) {
	stop := bar.Close
	return ExitDecision{ExitNow: true, NewStop: &stop}, nil
}

func TestStrategy_OnLongRejectsAmbiguousExitDecision(t *testing.T) {
	exitRuleRegistry["test-ambiguous-exit"] = func(Config) (ExitRule, error) { return ambiguousExitRule{}, nil }
	defer delete(exitRuleRegistry, "test-ambiguous-exit")

	h := enterLongWithConfig(t, Config{SMAPeriod: 3, ExitRuleName: "test-ambiguous-exit", ReEntryRuleName: "fresh-cross"})

	event, view, _ := h.buildBar(6, bar{open: 103, high: 110, low: 102, close: 105})
	_, err := h.strategy.OnBar(context.Background(), event, view)
	require.Error(t, err, "an ExitRule returning both ExitNow and NewStop must be rejected, not silently resolved")
}

// TestStrategy_SMACrossExitCapturesDecisionBarCloseAsReEntryReference
// is PR #348 review's required proof for the sma-cross +
// reclaim-exit-price combination: the re-entry reference price must
// come from the bar that actually decided the exit (bar 6, close 90),
// not the later bar the exit is first observed Flat on (bar 7, close
// 95) — which bears no relationship to why the position closed. Using
// bar 7's own close as the reference would also be self-referential
// on this very bar (a value can never compare strictly greater than
// itself), so the bug's symptom is that no immediate re-entry is ever
// possible on the observing bar even when price has clearly already
// recovered above the real decision level.
func TestStrategy_SMACrossExitCapturesDecisionBarCloseAsReEntryReference(t *testing.T) {
	h := enterLongWithConfig(t, Config{SMAPeriod: 3, ExitRuleName: "sma-cross", ReEntryRuleName: "reclaim-exit-price"})

	// SMA(bar4,5,6) = (99+102+90)/3 = 97, close (90) at/below it: the
	// sma-cross rule decides to exit right here, on this bar, with
	// this bar's own close (90) as the only meaningful reference level
	// — there is no resting stop to fall back on.
	intents, _ := h.onBar(6, bar{open: 95, high: 96, low: 89, close: 90})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentExit, intents[0].Kind)

	// Bar 7 is the first bar the exit is observed Flat on. SMA(bar5,6,7)
	// = (102+90+95)/3 = 95.67, close (95) below it, so the central
	// above-SMA gate alone would already block entry here regardless
	// of the reference price — this assertion is not yet the proof.
	intents, _ = h.onBar(7, bar{open: 92, high: 97, low: 91, close: 95})
	assert.Empty(t, intents, "below the SMA on bar 7 regardless of reference price")

	// Bar 8: SMA(bar6,7,8) = (90+95+93)/3 = 92.667, close (93) above
	// it — the central gate now permits the rule to decide. With the
	// correct reference (90, bar 6's decision close), 93 > 90 must
	// enter. The bug this replaces used bar 7's own close (95) as the
	// reference instead, under which 93 > 95 is false and this
	// assertion would fail.
	intents, _ = h.onBar(8, bar{open: 94, high: 98, low: 92, close: 93})
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentEnter, intents[0].Kind, "must reclaim against the decision bar's own close (90), not the later observing bar's close (95)")
}

// TestStrategy_InitialEntryModeFreshCrossWaitsIndefinitelyWhenStartingAboveSMA
// documents issue #349 review's own motivating startup gap under the
// default "fresh-cross" InitialEntryMode: crossState's own zero value
// (have=false) means the very first bar the SMA becomes ready can
// never itself report a cross, and if price is already above the SMA
// on that bar and simply stays there, no later bar reports one
// either — so a run or live session starting mid-trend never enters
// at all under this mode.
func TestStrategy_InitialEntryModeFreshCrossWaitsIndefinitelyWhenStartingAboveSMA(t *testing.T) {
	h := newTestHarness(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10")})

	// SMA becomes ready at bar 3 (=101), close (102) already above it
	// — but crossState.have is false on this very call, so
	// crossedAbove is false regardless.
	for i, c := range []float64{100, 101, 102, 103, 104} {
		intents, _ := h.onBar(i+1, bar{open: c, high: c, low: c, close: c})
		assert.Empty(t, intents, "fresh-cross must never enter while price only ever rises above the SMA without first dipping back below it")
	}
}

// TestStrategy_InitialEntryModeAboveSMAEntersOnFirstReadyBarAboveSMA
// proves the "above-sma" InitialEntryMode (issue #349 review) fixes
// exactly the gap the previous test documents: it enters on the very
// first bar the SMA is ready and price is already above it, with no
// cross required at all.
func TestStrategy_InitialEntryModeAboveSMAEntersOnFirstReadyBarAboveSMA(t *testing.T) {
	h := newTestHarness(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10"), InitialEntryModeName: "above-sma"})

	h.onBar(1, bar{open: 100, high: 100, low: 100, close: 100})
	h.onBar(2, bar{open: 101, high: 101, low: 101, close: 101})
	// SMA ready this bar: (100+101+102)/3 = 101, close (102) above it.
	intents, _ := h.onBar(3, bar{open: 102, high: 102, low: 102, close: 102})
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentEnter, intents[0].Kind, "above-sma must enter on the very first ready bar above the SMA, without requiring a fresh cross")
}

// TestStrategy_ProbationTrendFullLifecyclePhaseTransitions is issue
// #349's own central end-to-end proof, driving Strategy through a
// complete FLAT->PROBATION->TRENDING cycle, a stop-out back to FLAT,
// and a fresh re-entry — asserting Strategy.Phase() (not any
// ExitRule-internal field) at every step, per PR #348/#349 review's
// explicit requirement that Probation/Trending be first-class and
// queryable from Strategy itself.
func TestStrategy_ProbationTrendFullLifecyclePhaseTransitions(t *testing.T) {
	h := newTestHarness(t, Config{
		SMAPeriod:           3,
		ExitRuleName:        "probation-trend",
		ReEntryRuleName:     "fresh-cross",
		InitialStopBelowSMA: num.MustParseRate("0.01"),
		TrailActivationGain: num.MustParseRate("0.05"),
		TrailingStopPercent: num.MustParseRate("0.10"),
	})
	assert.Equal(t, PhaseFlat, h.strategy.Phase())

	for i, c := range []float64{100, 100, 100, 99} {
		h.onBar(i+1, bar{open: c, high: c, low: c, close: c})
	}
	intents, _ := h.onBar(5, bar{open: 102, high: 102, low: 102, close: 102})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentEnter, intents[0].Kind)
	h.side = order.Long
	h.avgPrice = "102" // the real fill price this episode entered at; activation threshold = 102 * 1.05 = 107.1

	assert.Equal(t, PhaseFlat, h.strategy.Phase(), "OnEntry has not yet been observed — this was only the entry-decision bar")

	// Bar 6: first bar observed Long — OnEntry seeds Probation.
	// sma(99,102,102)=101, stop=101*0.99=99.99.
	intents, _ = h.onBar(6, bar{open: 103, high: 105, low: 102, close: 102})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	assert.Equal(t, "99.99", intents[0].StopPrice.String())
	assert.Equal(t, PhaseProbation, h.strategy.Phase())

	// Bar 7: still below the 107.1 activation threshold.
	// sma(102,102,105)=103, stop=103*0.99=101.97.
	intents, _ = h.onBar(7, bar{open: 104, high: 110, low: 103, close: 105})
	require.Len(t, intents, 1)
	assert.Equal(t, "101.97", intents[0].StopPrice.String())
	assert.Equal(t, PhaseProbation, h.strategy.Phase())

	// Bar 8: close (108) reaches the 107.1 activation threshold.
	// sma(102,105,108)=105, stop=105*0.99=103.95 (still probation
	// math on this same bar). High (108) is below the 110 high-water
	// mark bar 7 already set, so it stays 110.
	intents, _ = h.onBar(8, bar{open: 106, high: 108, low: 105, close: 108})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	assert.Equal(t, "103.95", intents[0].StopPrice.String(), "the activation bar itself must still use probation math")
	assert.Equal(t, PhaseTrending, h.strategy.Phase(), "the transition takes effect immediately after this bar")

	// Bar 9: now genuinely Trending. Deep below any plausible SMA
	// (immune) with a lower High (90) than the 110 high-water mark bar
	// 7 already set — TRENDING's own raw formula (110*0.90=99) would
	// actually *loosen* protection below the 103.95 probation stop
	// bar 8 already placed, so the never-loosen handoff (issue #349
	// review) must emit nothing here, leaving that 103.95 resting stop
	// in place untouched.
	intents, _ = h.onBar(9, bar{open: 80, high: 90, low: 45, close: 50})
	assert.Empty(t, intents, "TRENDING's own raw stop (99) is below the 103.95 probation stop already in place and must never loosen it")
	assert.Equal(t, PhaseTrending, h.strategy.Phase())

	// Bar 10: a genuine new high-water mark (130, since entry) finally
	// pushes TRENDING's own formula (130*0.90=117) past that 103.95
	// floor — normal ratcheting resumes once it actually earns it.
	// Close stays low (55) deliberately: TRENDING is immune to the SMA
	// entirely, so this also keeps the SMA itself low for the
	// following bars, letting a real fresh cross re-enter later
	// without an outsized High permanently skewing it.
	intents, _ = h.onBar(10, bar{open: 60, high: 130, low: 55, close: 55})
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	assert.Equal(t, "117", intents[0].StopPrice.String())
	assert.Equal(t, PhaseTrending, h.strategy.Phase())

	// The trailing stop triggers (broker-side, ADR-026) — Strategy
	// itself has not yet processed this; Phase() still reports
	// Trending until the next OnBar call actually observes Flat.
	h.triggerStop()

	// Two flat bars staying below the SMA: fresh-cross re-entry must
	// wait.
	intents, _ = h.onBar(11, bar{open: 45, high: 48, low: 38, close: 40})
	assert.Empty(t, intents)
	assert.Equal(t, PhaseFlat, h.strategy.Phase())

	// A genuine fresh cross back above the SMA re-enters.
	intents, _ = h.onBar(12, bar{open: 45, high: 72, low: 44, close: 70})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentEnter, intents[0].Kind)
	h.side = order.Long
	h.avgPrice = "70"
	assert.Equal(t, PhaseFlat, h.strategy.Phase(), "still only the entry-decision bar")

	// The re-entry's own first Long bar must start a fresh Probation
	// episode: no stale high-water mark, activation, or trend-stop
	// floor carried over from the first episode (whose high-water
	// mark had reached 130 and whose trend-stop floor had reached
	// 117). close (72) stays under the fresh 70*1.05=73.5 activation
	// threshold, so this also confirms activation is computed from
	// the new episode's own entry price, not the old one.
	intents, _ = h.onBar(13, bar{open: 71, high: 73, low: 70, close: 72})
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	assert.Equal(t, PhaseProbation, h.strategy.Phase(), "a fresh re-entry must start in Probation, never stale Trending")
	assert.Less(t, intents[0].StopPrice.Cmp(num.MustParsePrice("99")), 0, "the fresh probation stop must be nowhere near the old episode's stop levels")
}
