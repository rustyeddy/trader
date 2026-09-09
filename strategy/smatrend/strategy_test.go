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
		if in.Kind == order.IntentEnter {
			h.side = order.Long
		}
	}
	return intents, barTime
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.D1, Config{SMAPeriod: 0, TrailingStopPercent: num.MustParseRate("0.10")})
	require.Error(t, err)
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
	h := newTestHarness(t, Config{SMAPeriod: 3, TrailingStopPercent: num.MustParseRate("0.10")})
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
