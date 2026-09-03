package emacross

import (
	"context"
	"errors"
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

// fakeView is a minimal strategy.View test double, mirroring
// strategy_test.go's own identical fakeView: an account.Snapshot fixed
// at construction, standing in for whatever a real runner would
// compute from already-visible state.
type fakeView struct {
	snap account.Snapshot
}

func (v fakeView) Account() account.Snapshot { return v.snap }

func mustListing(t *testing.T) instrument.Listing {
	t.Helper()
	l, err := tradertest.NewListing(tradertest.ListingParams{})
	require.NoError(t, err)
	return l
}

// mustSnapshot builds an account.Snapshot holding position (nil for
// flat), using tradertest's own builders rather than hand-populating
// every account.SnapshotParams field. accountID is fixed per harness
// (see testHarness) rather than freshly generated per call: a
// Position's own AccountID must equal its Snapshot's, and generating
// each independently from a shared, stateful id.Generator would
// advance it twice and produce two different values.
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

// testHarness bundles one fresh Strategy with everything needed to
// drive it deterministically bar by bar.
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

// newTestHarnessWithJournal is newTestHarness plus an Environment.Journal/
// RunID (issue #253, EMA-08's decision-evidence capability) — rec may
// be nil, matching a Strategy that never records anything.
func newTestHarnessWithJournal(t *testing.T, config Config, rec journal.Recorder, runID id.RunID) *testHarness {
	t.Helper()
	listing := mustListing(t)
	instID := listing.InstrumentID()

	s, err := New(instID, marketdata.H1, config)
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

// setPosition updates the harness's own simulated account state, as
// if side had already become authoritative (a filled entry, exit, or
// reversal) — this test drives the strategy directly, without a real
// pipeline/broker, so the test itself plays that role.
func (h *testHarness) setPosition(side order.PositionSide) {
	h.side = side
}

// onBar advances the clock to bar barNum's own time (bars are spaced
// one H1 interval apart from testStart) and calls OnBar with a bar
// whose Close is close, against a View reflecting the harness's
// current position.
// buildBar constructs bar barNum's own BarEvent/View pair (bars are
// spaced one H1 interval apart from testStart), reflecting the
// harness's current position, and advances h's clock to that bar's
// own time — the shared setup onBar and any test that needs to call
// OnBar itself (to inspect an error onBar's own require.NoError would
// otherwise hide) both use.
func (h *testHarness) buildBar(barNum int, close float64) (strategy.BarEvent, strategy.View, time.Time) {
	h.t.Helper()
	barTime := testStart.Add(time.Duration(barNum-1) * time.Hour)
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

	priceText := strconv.FormatFloat(close, 'f', -1, 64)
	bar := marketdata.Bar{
		Time:  barTime,
		Open:  num.MustParsePrice(priceText),
		High:  num.MustParsePrice(priceText),
		Low:   num.MustParsePrice(priceText),
		Close: num.MustParsePrice(priceText),
	}
	event := strategy.BarEvent{Instrument: h.instID, Interval: marketdata.H1, Bar: bar}
	view := fakeView{snap: mustSnapshot(h.t, h.accountID, position)}
	return event, view, barTime
}

func (h *testHarness) onBar(barNum int, close float64) ([]order.Intent, time.Time) {
	h.t.Helper()
	event, view, barTime := h.buildBar(barNum, close)
	intents, err := h.strategy.OnBar(context.Background(), event, view)
	require.NoError(h.t, err)
	return intents, barTime
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	listing := mustListing(t)
	_, err := New(listing.InstrumentID(), marketdata.H1, Config{FastPeriod: 50, SlowPeriod: 20})
	require.Error(t, err)
}

// TestStrategy_StartRejectsJournalWithoutRunID proves this fails
// loudly and immediately at Start, not only later — and confusingly —
// from journal.NewRecord's own "run id must be set" error the first
// time a crossover actually tries to record a signal (PR #264 review).
func TestStrategy_StartRejectsJournalWithoutRunID(t *testing.T) {
	listing := mustListing(t)
	s, err := New(listing.InstrumentID(), marketdata.H1, Config{FastPeriod: 3, SlowPeriod: 5})
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
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})
	desc := h.strategy.Describe()

	assert.Equal(t, Name, desc.Name)
	assert.Equal(t, Version, desc.Version)
	require.Len(t, desc.Requirements, 1)
	assert.Equal(t, h.instID, desc.Requirements[0].Instrument)
	assert.Equal(t, marketdata.H1, desc.Requirements[0].Interval)
	assert.Equal(t, 5, desc.Requirements[0].WarmupBars, "WarmupBars must equal SlowPeriod (Decision 2)")
}

// emaFixtureCloses is docs/research/ema-01-experiment-definition.org's
// own worked toy fixture: fast EMA(3)/slow EMA(5), one bullish cross at
// bar 7 (flat->long) and one bearish cross at bar 12 (long->short,
// reversal). Bars 1-5 are warm-up (WarmupBars = SlowPeriod = 5); bar 6
// is the first non-warm-up bar but has no signal.
var emaFixtureCloses = []float64{104, 103, 102, 101, 100, 101, 104, 108, 112, 110, 105, 100, 95, 90}

// TestStrategy_WarmupBarsEmitNoIntents proves no intent is ever
// returned for any of the fixture's first 6 bars (5 declared warm-up
// bars plus bar 6, the first non-warm-up bar with no signal yet) — see
// docs/research/ema-01-experiment-definition.org's Decision 3.
func TestStrategy_WarmupBarsEmitNoIntents(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})

	for i, close := range emaFixtureCloses[:6] {
		intents, _ := h.onBar(i+1, close)
		assert.Emptyf(t, intents, "bar %d must not emit any intent", i+1)
	}
}

// TestStrategy_FullFixtureMatchesEMA01WorkedExample replays every bar
// of the worked fixture and asserts the exact intent(s) — including
// timestamps — the design document's table records: nothing until bar
// 7 (Enter Buy, flat->long), nothing again until bar 12 (Exit+Enter
// Sell, long->short, one shared correlation ID), and nothing after.
func TestStrategy_FullFixtureMatchesEMA01WorkedExample(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})

	for i, close := range emaFixtureCloses {
		bar := i + 1
		intents, barTime := h.onBar(bar, close)

		switch bar {
		case 7:
			require.Lenf(t, intents, 1, "bar %d: expected exactly one Enter intent", bar)
			assert.Equal(t, order.IntentEnter, intents[0].Kind)
			assert.Equal(t, h.instID, intents[0].Instrument)
			assert.Equal(t, order.Buy, intents[0].Side)
			assert.True(t, barTime.Equal(intents[0].Metadata.Timestamp))
			h.setPosition(order.Long)

		case 12:
			require.Lenf(t, intents, 2, "bar %d: expected an Exit+Enter reversal pair", bar)
			assert.Equal(t, order.IntentExit, intents[0].Kind)
			assert.Equal(t, order.IntentEnter, intents[1].Kind)
			assert.Equal(t, order.Sell, intents[1].Side)
			assert.Equal(t, h.instID, intents[0].Instrument)
			assert.Equal(t, h.instID, intents[1].Instrument)
			assert.Equal(t, intents[0].Metadata.CorrelationID, intents[1].Metadata.CorrelationID,
				"the exit and re-entry must share one correlation ID (Decision 4)")
			assert.True(t, barTime.Equal(intents[0].Metadata.Timestamp))
			assert.True(t, barTime.Equal(intents[1].Metadata.Timestamp))
			h.setPosition(order.Short)

		default:
			assert.Emptyf(t, intents, "bar %d: expected no intent", bar)
		}
	}
}

// TestStrategy_DeterministicAcrossRepeatedRuns proves issue #249's own
// acceptance criterion: replaying the identical bar sequence through a
// freshly constructed Strategy (with a freshly seeded, but identically
// seeded, clock/id generator) produces an identical intent sequence
// every time — including IDs and timestamps, not merely equivalent
// trading decisions.
func TestStrategy_DeterministicAcrossRepeatedRuns(t *testing.T) {
	run := func() []order.Intent {
		h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})
		var all []order.Intent
		for i, close := range emaFixtureCloses {
			intents, _ := h.onBar(i+1, close)
			all = append(all, intents...)
			switch i + 1 {
			case 7:
				h.setPosition(order.Long)
			case 12:
				h.setPosition(order.Short)
			}
		}
		return all
	}

	first := run()
	require.NotEmpty(t, first)
	for i := range 3 {
		assert.Equal(t, first, run(), "run %d diverged from the first run", i+2)
	}
}

func TestStrategy_Config(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})
	assert.Equal(t, Config{FastPeriod: 3, SlowPeriod: 5}, h.strategy.Config())
}

// errBoom is a sentinel test error for fault-injecting IntentFactory
// failures below.
var errBoom = errors.New("boom")

// erroringIntentFactory wraps a real strategy.IntentFactory and
// selectively fails specific calls, so actOnCross/reverse's own error
// paths (which a correctly behaving IntentFactory never actually
// exercises) can be tested directly.
type erroringIntentFactory struct {
	strategy.IntentFactory
	failEnter       bool
	failExit        bool
	failCorrelation bool
}

func (f *erroringIntentFactory) Enter(instID instrument.ID, side order.Side) (order.Intent, error) {
	if f.failEnter {
		return order.Intent{}, errBoom
	}
	return f.IntentFactory.Enter(instID, side)
}

func (f *erroringIntentFactory) Exit(instID instrument.ID) (order.Intent, error) {
	if f.failExit {
		return order.Intent{}, errBoom
	}
	return f.IntentFactory.Exit(instID)
}

func (f *erroringIntentFactory) NewCorrelationID() (id.CorrelationID, error) {
	if f.failCorrelation {
		return id.CorrelationID{}, errBoom
	}
	return f.IntentFactory.NewCorrelationID()
}

func (f *erroringIntentFactory) WithCorrelation(corr id.CorrelationID) strategy.IntentFactory {
	return &erroringIntentFactory{
		IntentFactory:   f.IntentFactory.WithCorrelation(corr),
		failEnter:       f.failEnter,
		failExit:        f.failExit,
		failCorrelation: f.failCorrelation,
	}
}

func TestStrategy_ActOnCross_FlatEntersOnEitherSide(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})

	action, intents, err := h.strategy.actOnCross(order.Flat, order.Buy)
	require.NoError(t, err)
	assert.Equal(t, "enter-long", action)
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentEnter, intents[0].Kind)
	assert.Equal(t, order.Buy, intents[0].Side)

	action, intents, err = h.strategy.actOnCross(order.Flat, order.Sell)
	require.NoError(t, err)
	assert.Equal(t, "enter-short", action)
	require.Len(t, intents, 1)
	assert.Equal(t, order.Sell, intents[0].Side)
}

func TestStrategy_ActOnCross_SameSideRepeatIsNoOp(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})

	action, intents, err := h.strategy.actOnCross(order.Long, order.Buy)
	require.NoError(t, err)
	assert.Equal(t, "none", action)
	assert.Empty(t, intents, "a bullish cross while already long must not re-enter")

	action, intents, err = h.strategy.actOnCross(order.Short, order.Sell)
	require.NoError(t, err)
	assert.Equal(t, "none", action)
	assert.Empty(t, intents, "a bearish cross while already short must not re-enter")
}

func TestStrategy_ActOnCross_OppositeSideReverses(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})

	action, intents, err := h.strategy.actOnCross(order.Long, order.Sell)
	require.NoError(t, err)
	assert.Equal(t, "reverse", action)
	require.Len(t, intents, 2)
	assert.Equal(t, order.IntentExit, intents[0].Kind)
	assert.Equal(t, order.IntentEnter, intents[1].Kind)

	action, intents, err = h.strategy.actOnCross(order.Short, order.Buy)
	require.NoError(t, err)
	assert.Equal(t, "reverse", action)
	require.Len(t, intents, 2)
	assert.Equal(t, order.IntentExit, intents[0].Kind)
	assert.Equal(t, order.IntentEnter, intents[1].Kind)
}

func TestStrategy_ActOnCross_UnrecognizedSideReturnsError(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})
	_, _, err := h.strategy.actOnCross(order.PositionSide(99), order.Buy)
	assert.Error(t, err)
}

func TestStrategy_ActOnCross_EnterFailurePropagates(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})
	h.strategy.intents = &erroringIntentFactory{IntentFactory: h.strategy.intents, failEnter: true}

	_, _, err := h.strategy.actOnCross(order.Flat, order.Buy)
	require.ErrorIs(t, err, errBoom)
}

func TestStrategy_Reverse_NewCorrelationIDFailurePropagates(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})
	h.strategy.intents = &erroringIntentFactory{IntentFactory: h.strategy.intents, failCorrelation: true}

	_, err := h.strategy.reverse(order.Buy)
	require.ErrorIs(t, err, errBoom)
}

func TestStrategy_Reverse_ExitFailurePropagates(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})
	h.strategy.intents = &erroringIntentFactory{IntentFactory: h.strategy.intents, failExit: true}

	_, err := h.strategy.reverse(order.Buy)
	require.ErrorIs(t, err, errBoom)
}

func TestStrategy_Reverse_EnterFailurePropagates(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5})
	h.strategy.intents = &erroringIntentFactory{IntentFactory: h.strategy.intents, failEnter: true}

	_, err := h.strategy.reverse(order.Buy)
	require.ErrorIs(t, err, errBoom)
}

// TestStrategy_ActOnCross_AllowedSideShortOnly covers issue #273's
// full short-only transition table: a bullish cross while flat never
// enters (long is disallowed), a bullish cross while short exits to
// flat only (never reverses into a disallowed long), and a bearish
// cross behaves exactly as SideBoth (short is always allowed).
func TestStrategy_ActOnCross_AllowedSideShortOnly(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5, AllowedSide: SideShortOnly})

	action, intents, err := h.strategy.actOnCross(order.Flat, order.Buy)
	require.NoError(t, err)
	assert.Equal(t, "none", action)
	assert.Empty(t, intents, "flat + bullish cross must never enter long under short-only")

	action, intents, err = h.strategy.actOnCross(order.Flat, order.Sell)
	require.NoError(t, err)
	assert.Equal(t, "enter-short", action)
	require.Len(t, intents, 1)
	assert.Equal(t, order.Sell, intents[0].Side)

	action, intents, err = h.strategy.actOnCross(order.Short, order.Buy)
	require.NoError(t, err)
	assert.Equal(t, "exit", action, "a bullish cross while short must close to flat, not reverse into a disallowed long")
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentExit, intents[0].Kind)

	action, intents, err = h.strategy.actOnCross(order.Short, order.Sell)
	require.NoError(t, err)
	assert.Equal(t, "none", action)
	assert.Empty(t, intents, "a bearish cross while already short must not re-enter")
}

// TestStrategy_ActOnCross_AllowedSideLongOnly is the long-only mirror
// of TestStrategy_ActOnCross_AllowedSideShortOnly.
func TestStrategy_ActOnCross_AllowedSideLongOnly(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5, AllowedSide: SideLongOnly})

	action, intents, err := h.strategy.actOnCross(order.Flat, order.Sell)
	require.NoError(t, err)
	assert.Equal(t, "none", action)
	assert.Empty(t, intents, "flat + bearish cross must never enter short under long-only")

	action, intents, err = h.strategy.actOnCross(order.Flat, order.Buy)
	require.NoError(t, err)
	assert.Equal(t, "enter-long", action)
	require.Len(t, intents, 1)
	assert.Equal(t, order.Buy, intents[0].Side)

	action, intents, err = h.strategy.actOnCross(order.Long, order.Sell)
	require.NoError(t, err)
	assert.Equal(t, "exit", action, "a bearish cross while long must close to flat, not reverse into a disallowed short")
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentExit, intents[0].Kind)

	action, intents, err = h.strategy.actOnCross(order.Long, order.Buy)
	require.NoError(t, err)
	assert.Equal(t, "none", action)
	assert.Empty(t, intents, "a bullish cross while already long must not re-enter")
}

// TestStrategy_ActOnCross_AllowedSideBothUnchanged proves the default
// (SideBoth, the zero value) reproduces exactly
// TestStrategy_ActOnCross_OppositeSideReverses's own reversal
// behavior — issue #273 must not change any existing SideBoth-mode
// caller's observable behavior.
func TestStrategy_ActOnCross_AllowedSideBothUnchanged(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5, AllowedSide: SideBoth})

	action, intents, err := h.strategy.actOnCross(order.Long, order.Sell)
	require.NoError(t, err)
	assert.Equal(t, "reverse", action)
	require.Len(t, intents, 2)

	action, intents, err = h.strategy.actOnCross(order.Short, order.Buy)
	require.NoError(t, err)
	assert.Equal(t, "reverse", action)
	require.Len(t, intents, 2)
}

func TestStrategy_ExitOnly_ExitFailurePropagates(t *testing.T) {
	h := newTestHarness(t, Config{FastPeriod: 3, SlowPeriod: 5, AllowedSide: SideShortOnly})
	h.strategy.intents = &erroringIntentFactory{IntentFactory: h.strategy.intents, failExit: true}

	_, _, err := h.strategy.actOnCross(order.Short, order.Buy)
	require.ErrorIs(t, err, errBoom)
}
