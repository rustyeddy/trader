package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two-instrument trading through one trader, broker and account (#575). The
// instruments are the registered EURUSD (2% margin) and GBPUSD (5%). Every
// expected value is worked by hand in the comments; none is read back from the
// code.

// pairStart is the opening of the first hourly bar in these tests.
var pairStart = time.Date(2010, 1, 4, 10, 0, 0, 0, time.UTC)

// hour is the opening time h hours after pairStart.
func hour(h int) time.Time { return pairStart.Add(time.Duration(h) * time.Hour) }

// hbar is an hourly bar opening at the given time: complete, and available, an
// hour later.
func hbar(instrument string, opened time.Time, o, c price) bar {
	return bar{
		instrument:  instrument,
		Time:        opened,
		ohlc:        ohlc{o: o, h: max(o, c), l: min(o, c), c: c},
		availableAt: opened.Add(time.Hour),
	}
}

// pairFrame is the frame of the given bars, which share an availability.
func pairFrame(bars ...bar) frame {
	return frame{availableAt: bars[0].availableAt, bars: bars}
}

// reactStrategy answers each bar with whatever react returns and keeps the
// orders it made.
type reactStrategy struct {
	react func(b bar) *order
	made  []*order
}

func (s *reactStrategy) name() string { return "react" }
func (s *reactStrategy) init() error  { return nil }
func (s *reactStrategy) onBar(_ context.Context, b bar) *order {
	o := s.react(b)
	if o != nil {
		s.made = append(s.made, o)
	}
	return o
}

// openOn returns a strategy that opens instrument long, at price p with stop
// stop, once, when it sees a bar of on.
func openOn(on, instrument string, p, stop price) *reactStrategy {
	done := false
	return &reactStrategy{react: func(b bar) *order {
		if done || b.instrument != on {
			return nil
		}
		done = true
		return &order{instrument: instrument, direction: long, action: open, orderType: marketOrder,
			price: p, stopLoss: stop, Time: b.Time}
	}}
}

func pairTrader(balance money, strat strategy) (*trader, *account) {
	br := newBroker("t")
	act := br.addAccount("a", balance)
	return &trader{broker: br, account: act, strategy: strat}, act
}

func longAt(instrument string, units int64, entry, mark price) *position {
	return &position{instrument: instrument, direction: long, units: units,
		entryPrice: entry, entryTime: hour(0), markPrice: mark}
}

func lastReason(o *order) string {
	return o.history[len(o.history)-1].reason
}

func TestCheckFrame_RefusesFramesItCannotOrder(t *testing.T) {
	eur, gbp := hbar("EURUSD", hour(1), 110000, 110000), hbar("GBPUSD", hour(1), 130000, 130000)
	unknown := eur
	unknown.availableAt = time.Time{}
	late := gbp
	late.availableAt = gbp.availableAt.Add(time.Minute)
	offset := hbar("GBPUSD", hour(1).Add(-30*time.Minute), 130000, 130000)
	offset.availableAt = eur.availableAt // available together, opened apart

	tests := []struct {
		name string
		f    frame
		want string
	}{
		{"no bars", frame{availableAt: eur.availableAt}, "no bars"},
		{"no availability", frame{bars: []bar{unknown}}, "no availability"},
		{"a bar from another moment", frame{availableAt: eur.availableAt, bars: []bar{eur, late}}, "is available at"},
		{"instruments out of order", frame{availableAt: eur.availableAt, bars: []bar{gbp, eur}}, "in order"},
		{"an instrument twice", frame{availableAt: eur.availableAt, bars: []bar{eur, eur}}, "in order"},
		{"mixed opening times", frame{availableAt: eur.availableAt, bars: []bar{eur, offset}}, "mixed opening times"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			captureLogs(t)
			br, _ := newFillBroker()
			require.ErrorContains(t, br.onFrame(context.Background(), tt.f), tt.want)
		})
	}
}

func TestBroker_OnBar_RefusesABarWithoutAvailability(t *testing.T) {
	captureLogs(t)
	br, _ := newFillBroker()
	b := fillBar(1, 110000)
	b.availableAt = time.Time{}

	require.ErrorContains(t, br.onBar(context.Background(), b), "no availability")
}

// The boundary of fill eligibility: an order decided at 11:00 may fill on a bar
// opening at 11:00, and not on one opening a minute earlier, though that bar
// opened after the bar the order was made on.
func TestExecution_EligibleFromTheMomentTheOrderWasDecided(t *testing.T) {
	o := &order{instrument: "GBPUSD", direction: long, action: open, orderType: marketOrder,
		state: stateSubmitted, Time: hour(0), decidedAt: hour(1)}

	_, early := o.execution(hbar("GBPUSD", hour(1).Add(-time.Minute), 130100, 130100))
	p, onTime := o.execution(hbar("GBPUSD", hour(1), 130200, 130200))

	assert.False(t, early, "opened before the order was decided")
	assert.True(t, onTime, "opened the moment it was decided")
	assert.Equal(t, price(130200), p)
}

// A fill at the open is admitted against what is known at that moment: the
// opens of every bar in the frame, not the previous closes.
//
// $10,000 account, long 100,000 GBPUSD from 1.00000. A EURUSD open of 200,000
// units waits; at 1.10000 it needs 200,000 * 1.1 * 2% = $4,400 of margin.
// GBPUSD gaps down to 0.99000 at the frame's open:
//
//	equity  = 10,000 - 100,000 * 0.01   = $9,000
//	used    = 100,000 * 0.99 * 5%       = $4,950
//	avail   = 9,000 - 4,950             = $4,050 < $4,400: rejected
//
// At GBPUSD's previous close (1.00000) the margin available would be $5,000
// and EURUSD, first in instrument order, would be admitted. At the frame's
// close (1.02000), which has not happened yet when the open fills, it would be
// 12,000 - 5,100 = $6,900, admitted too.
func TestBroker_OnFrame_AdmitsAFillAgainstTheOpensOfTheFrame(t *testing.T) {
	captureLogs(t)
	br, act := newFillBroker()
	act.positions["GBPUSD"] = longAt("GBPUSD", 100000, 100000, 100000)
	o := &order{id: "t-1", accountID: "a", instrument: "EURUSD", direction: long, action: open,
		orderType: marketOrder, price: 110000, stopLoss: 108900, units: 200000,
		Time: hour(0), decidedAt: hour(1)}
	br.enqueue(o)

	f := pairFrame(hbar("EURUSD", hour(1), 110000, 110000), hbar("GBPUSD", hour(1), 99000, 102000))
	require.NoError(t, br.onFrame(context.Background(), f))

	assert.Equal(t, stateRejected, o.state)
	assert.Contains(t, lastReason(o), "needs 440000 cents of margin at the fill, 405000 available")
	assert.NotContains(t, act.positions, "EURUSD")
	assert.Equal(t, usd(10000), act.balance)
}

// The margin rules run once, after the whole frame is marked, so the result
// cannot depend on which instrument is processed first (the probe in the memo).
//
// $10,000 account, long 200,000 of each from 1.00000. Both open the frame at
// 1.00000; EURUSD closes 0.96500 and GBPUSD 1.03500:
//
//	equity = 10,000 - 7,000 + 7,000                        = $10,000
//	used   = 200,000*0.965*2% + 200,000*1.035*5% = 3,860 + 10,350 = $14,210
//	level  = 70%: a warning, above the 50% closeout
//
// Checked after EURUSD alone, GBPUSD would still be at its open, 1.00000:
// equity $3,000 against $13,860, 22%, and EURUSD would be liquidated.
func TestBroker_OnFrame_ChecksMarginOnceTheWholeFrameIsMarked(t *testing.T) {
	logs := captureLogs(t)
	br, act := newFillBroker()
	act.positions["EURUSD"] = longAt("EURUSD", 200000, 100000, 100000)
	act.positions["GBPUSD"] = longAt("GBPUSD", 200000, 100000, 100000)

	f := pairFrame(hbar("EURUSD", hour(1), 100000, 96500), hbar("GBPUSD", hour(1), 100000, 103500))
	require.NoError(t, br.onFrame(context.Background(), f))

	assert.Empty(t, br.closeouts())
	assert.Contains(t, logs.String(), "margin warning")
	assert.Len(t, act.positions, 2)
	assert.Equal(t, usd(10000), act.equity())
	assert.Equal(t, usd(14210), act.usedMargin())
}

// The strategy sees the frame's bars only after the broker has marked all of
// them, so an open is sized against the whole frame (the sizing probe in the
// memo).
//
// $10,000 account, long 20,000 GBPUSD from 1.00000, which closes at 0.95000. A
// EURUSD open at 1.00000 with its stop at 0.99000 (1,000 points):
//
//	equity       = 10,000 - 20,000 * 0.05            = $9,000
//	risk units   = 9,000 * 1% / 0.01000              = 9,000
//	margin units = (9,000 - 20,000*0.95*5%) / (1.0 * 2%) = 8,050 / 0.02 = 402,500
//
// So 9,000 units. Sized before GBPUSD's bar it would be 10,000.
func TestTrader_OnFrame_SizesAnOpenAgainstTheWholeFrame(t *testing.T) {
	captureLogs(t)
	strat := openOn("EURUSD", "EURUSD", 100000, 99000)
	tr, act := pairTrader(usd(10000), strat)
	act.positions["GBPUSD"] = longAt("GBPUSD", 20000, 100000, 100000)

	f := pairFrame(hbar("EURUSD", hour(1), 100000, 100000), hbar("GBPUSD", hour(1), 95000, 95000))
	require.NoError(t, tr.onFrame(context.Background(), f))

	require.Len(t, strat.made, 1)
	assert.Equal(t, int64(9000), strat.made[0].units)
	assert.Equal(t, stateSubmitted, strat.made[0].state)
}

// One provider and one timeframe give bars that line up, so a frame never
// opens before the previous one was available. Offset bars would let a GBPUSD
// open at 10:30 be margined against EURUSD's 11:00 close, a price from after
// the fill, so the broker refuses the frame before anything executes.
func TestBroker_OnFrame_RefusesAFrameOverlappingThePreviousOne(t *testing.T) {
	captureLogs(t)
	br := newBroker("t")
	act := br.addAccount("a", usd(10000))
	act.positions["EURUSD"] = longAt("EURUSD", 10000, 110000, 110000)
	o := &order{accountID: "a", instrument: "GBPUSD", direction: long, action: open, orderType: marketOrder,
		price: 130000, stopLoss: 128700, Time: hour(-1), decidedAt: hour(0)}
	require.NoError(t, br.submit(context.Background(), o))

	require.NoError(t, br.onFrame(context.Background(), pairFrame(hbar("EURUSD", hour(0), 110000, 100000))))
	half := 30 * time.Minute
	err := br.onFrame(context.Background(), pairFrame(hbar("GBPUSD", hour(0).Add(half), 130000, 130000)))

	require.ErrorIs(t, err, errFrameOverlap)
	assert.Contains(t, err.Error(), "opens at 2010-01-04T10:30:00Z, before 2010-01-04T11:00:00Z")
	assert.Equal(t, stateSubmitted, o.state, "nothing executes on a refused frame")
	assert.Equal(t, hour(1), br.clock, "the refused frame does not advance the broker")
}

// A frame may open exactly when the previous one became available, or later
// after a gap: those are the aligned bars the broker supports.
func TestBroker_OnFrame_AcceptsContiguousAndGappedFrames(t *testing.T) {
	captureLogs(t)
	br := newBroker("t")
	for _, h := range []int{0, 1, 3} {
		require.NoError(t, br.onFrame(context.Background(), pairFrame(hbar("EURUSD", hour(h), 110000, 110000))))
	}
	assert.Equal(t, hour(4), br.clock)
}

// A closeout triggered by one instrument's bar blocks an open decided on
// another instrument's bar in the same frame, though that bar comes first.
//
// $1,000 account, long 10,000 GBPUSD from 1.10000, which crashes to 0.90000:
// equity 1,000 - 2,000 = -$1,000, so it is liquidated. The strategy opens
// EURUSD on EURUSD's bar of the same frame; the closeout guard refuses it.
// Processed bar by bar, the open would have been sized and queued before
// GBPUSD's bar and then cancelled by the closeout instead.
func TestTrader_OnFrame_ACloseoutBlocksOpensFromEveryBarOfTheFrame(t *testing.T) {
	captureLogs(t)
	strat := openOn("EURUSD", "EURUSD", 110000, 108900)
	tr, act := pairTrader(usd(1000), strat)
	act.positions["GBPUSD"] = longAt("GBPUSD", 10000, 110000, 110000)

	f := pairFrame(hbar("EURUSD", hour(1), 110000, 110000), hbar("GBPUSD", hour(1), 90000, 90000))
	require.NoError(t, tr.onFrame(context.Background(), f))

	require.Len(t, tr.broker.closeouts(), 1)
	assert.Equal(t, "GBPUSD", tr.broker.closeouts()[0].instrument)
	assert.Equal(t, f.availableAt, act.closedOutAt)
	require.Len(t, strat.made, 1)
	o := strat.made[0]
	assert.Equal(t, stateRejected, o.state)
	assert.Contains(t, lastReason(o), errCloseoutGuard.Error())
	assert.Equal(t, []*order{o}, tr.rejected)
}

// The margin rules value a position at its latest mark, but a liquidation is
// an execution and needs a current price. Here the worst loser has no bar in
// the frame, so the closeout stops rather than fill it at its old mark, and
// resumes when that instrument's next bar arrives.
//
// $1,000 account, long 10,000 of each from 1.10000. GBPUSD was last marked at
// 0.90000 (-$2,000); EURUSD's bar closes at 1.09000 (-$100). Equity is
// -$1,100: liquidate, worst loser GBPUSD, which has no bar. Deferred.
//
// GBPUSD's next bar closes at 0.91000 (-$1,900): equity -$1,000, liquidate,
// GBPUSD is current and closes at 0.91000, leaving a balance of -$900. EURUSD
// is still under the closeout level (equity -$1,000 against $218 of margin),
// but its mark is now old, so it waits.
func TestBroker_OnFrame_DefersALiquidationWithoutACurrentPrice(t *testing.T) {
	logs := captureLogs(t)
	br, act := newFillBroker()
	act.balance = usd(1000)
	act.positions["EURUSD"] = longAt("EURUSD", 10000, 110000, 110000)
	act.positions["GBPUSD"] = longAt("GBPUSD", 10000, 110000, 90000)

	first := pairFrame(hbar("EURUSD", hour(1), 109000, 109000))
	require.NoError(t, br.onFrame(context.Background(), first))

	assert.Empty(t, br.closeouts(), "never closed at the old mark")
	assert.Len(t, act.positions, 2)
	assert.Equal(t, first.availableAt, act.closedOutAt, "the closeout still happened")
	assert.Contains(t, logs.String(), "margin closeout deferred: no current price")

	second := pairFrame(hbar("GBPUSD", hour(2), 91000, 91000))
	require.NoError(t, br.onFrame(context.Background(), second))

	require.Len(t, br.closeouts(), 1)
	c := br.closeouts()[0]
	assert.Equal(t, "GBPUSD", c.instrument)
	assert.Equal(t, price(91000), c.fillPrice)
	assert.Equal(t, hour(2), c.Time)
	assert.Equal(t, usd(-900), act.balance)
	assert.Contains(t, act.positions, "EURUSD", "its mark is old: deferred again")
}

// pairBars is EURUSD and GBPUSD hourly from pairStart. GBPUSD has no bar at
// hour 3. Each bar opens at the previous close.
//
//	hour  EURUSD           GBPUSD
//	0     1.00000          2.00000
//	1     1.00000-1.00300  2.00000-2.00600
//	2     1.00300-1.00600  2.00600-2.01200
//	3     1.00600-1.00900  -
//	4     1.00900-1.01200  2.01200-2.01800
func pairBars() (*marketData, *marketData) {
	eur := &marketData{instrument: "EURUSD"}
	for h, c := range []price{100000, 100300, 100600, 100900, 101200} {
		o := c
		if h > 0 {
			o = eur.data[h-1].c
		}
		eur.data = append(eur.data, hbar("EURUSD", hour(h), o, c))
	}
	gbp := &marketData{instrument: "GBPUSD", data: []bar{
		hbar("GBPUSD", hour(0), 200000, 200000),
		hbar("GBPUSD", hour(1), 200000, 200600),
		hbar("GBPUSD", hour(2), 200600, 201200),
		hbar("GBPUSD", hour(4), 201200, 201800),
	}}
	return eur, gbp
}

func runPair(t *testing.T, eur, gbp *marketData) (*trader, *account) {
	t.Helper()
	strat := &emaxEach{id: "pair", instruments: []string{"EURUSD", "GBPUSD"}, short: 2, long: 3, stopPct: 400}
	require.NoError(t, strat.init())
	tr, act := pairTrader(usd(10000), strat)
	for f, err := range frames(mergedBars(context.Background(), eur, gbp)) {
		require.NoError(t, err)
		require.NoError(t, tr.onFrame(context.Background(), f))
	}
	return tr, act
}

// filledOpen is the account's filled open on the instrument.
func filledOpen(t *testing.T, br *broker, instrument string) *order {
	t.Helper()
	for _, o := range br.completed {
		if o.instrument == instrument && o.action == open {
			require.Equal(t, stateFilled, o.state)
			return o
		}
	}
	require.FailNow(t, "no filled open", instrument)
	return nil
}

// Both instruments signal long on the hour-2 bars, in one frame, and compete
// for one account's margin. With short=2 and long=3 (alpha 2/3 and 1/2):
//
//	EURUSD hour 2: short 1.00450 > long 1.00300; GBPUSD: 2.00900 > 2.00600
//
// The stop is 0.04% of the close, rounded down: 40 points for EURUSD at
// 1.00600 (stop 1.00560), 80 for GBPUSD at 2.01200 (stop 2.01120). Risk is 1%
// of $10,000 = $100. EURUSD is sized first:
//
//	EURUSD risk   = 100 / 0.00040 = 250,000; margin 10,000 / (1.006*2%) = 497,017
//	        -> 250,000 units, reserving 250,000 * 1.006 * 2% = $5,030
//	GBPUSD risk   = 100 / 0.00080 = 125,000
//	       margin = (10,000 - 5,030) / (2.012*5%) = 4,970 / 0.1006 = 49,403.6
//	        -> 49,403 units: margin-limited by EURUSD's reservation
//
// EURUSD fills at hour 3's open, 1.00600. GBPUSD has no hour-3 bar, so its
// order waits (nothing is synthesized) and fills at hour 4's open, 2.01200,
// needing 49,403 * 2.012 * 5% = $4,969.95 (rounded up) against the $5,705
// available (equity $10,750 less EURUSD's $5,045, both at hour 4's open).
// Neither EMA advances at the missing hour, and both trends stay up, so no
// more orders. At the end, marked at hour 4's closes:
//
//	EURUSD unrealized 250,000 * 0.00600 = $1,500.00
//	GBPUSD unrealized  49,403 * 0.00600 = $296.418 -> $296.41
//	equity 10,000 + 1,796.41 = $11,796.41
//	margin 250,000*1.012*2% + 49,403*2.018*5% = 5,060 + 4,984.77 = $10,044.77
func TestEMAXEach_TwoInstrumentsShareOneAccount(t *testing.T) {
	captureLogs(t)
	eurBars, gbpBars := pairBars()
	tr, act := runPair(t, eurBars, gbpBars)

	eur, gbp := filledOpen(t, tr.broker, "EURUSD"), filledOpen(t, tr.broker, "GBPUSD")
	assert.Equal(t, int64(250000), eur.units)
	assert.Equal(t, int64(49403), gbp.units)
	assert.Equal(t, hour(3), eur.decidedAt, "both decided on the hour-2 bars")
	assert.Equal(t, hour(3), gbp.decidedAt)
	assert.Equal(t, price(100600), eur.fillPrice)
	assert.Equal(t, hour(3), eur.Time)
	assert.Equal(t, price(201200), gbp.fillPrice)
	assert.Equal(t, hour(4), gbp.Time, "no GBPUSD bar at hour 3")

	require.Len(t, act.positions, 2, "one position per instrument")
	assert.Equal(t, price(101200), act.positions["EURUSD"].markPrice)
	assert.Equal(t, price(201800), act.positions["GBPUSD"].markPrice)
	assert.Equal(t, usd(10000), act.balance, "nothing realized")
	assert.Equal(t, money(179641), act.unrealizedPnL())
	assert.Equal(t, money(1179641), act.equity())
	assert.Equal(t, money(1004477), act.usedMargin())

	stops := map[string]price{}
	for _, o := range tr.broker.orders["EURUSD"] {
		stops[o.instrument] = o.price
	}
	for _, o := range tr.broker.orders["GBPUSD"] {
		stops[o.instrument] = o.price
	}
	assert.Equal(t, map[string]price{"EURUSD": 100560, "GBPUSD": 201120}, stops, "each position has its own stop")
	assert.Empty(t, tr.rejected)
}

// GBPUSD starts an hour later, so it warms up and signals an hour after
// EURUSD. Its open is sized on the hour-3 frame, after EURUSD's position has
// filled at 1.00600 and been marked at 1.00900:
//
//	equity  = 10,000 + 250,000 * 0.003 = $10,750
//	risk    = 107.50 / 0.00080          = 134,375
//	margin  = (10,750 - 250,000*1.009*2%) / (2.012*5%) = 5,705 / 0.1006 = 56,709.7
//	        -> 56,709 units
//
// It fills at hour 4's open, 2.01200, needing $5,704.93 against $5,705.
func TestEMAXEach_StaggeredSignalsSizeAgainstTheEarlierPosition(t *testing.T) {
	captureLogs(t)
	eur, _ := pairBars()
	gbp := &marketData{instrument: "GBPUSD", data: []bar{
		hbar("GBPUSD", hour(1), 200000, 200000),
		hbar("GBPUSD", hour(2), 200000, 200600),
		hbar("GBPUSD", hour(3), 200600, 201200),
		hbar("GBPUSD", hour(4), 201200, 201800),
	}}
	tr, act := runPair(t, eur, gbp)

	e, g := filledOpen(t, tr.broker, "EURUSD"), filledOpen(t, tr.broker, "GBPUSD")
	assert.Equal(t, hour(3), e.decidedAt)
	assert.Equal(t, hour(4), g.decidedAt, "an hour later")
	assert.Equal(t, int64(56709), g.units)
	assert.Equal(t, hour(4), g.Time)
	assert.Len(t, act.positions, 2)
}

func TestEMAXEach_Init(t *testing.T) {
	tests := []struct {
		name string
		s    emaxEach
		want string
	}{
		{"no instruments", emaxEach{short: 2, long: 3, stopPct: 400}, "no instruments"},
		{"an instrument twice", emaxEach{instruments: []string{"EURUSD", "EURUSD"}, short: 2, long: 3, stopPct: 400}, "twice"},
		{"bad windows", emaxEach{instruments: []string{"EURUSD"}, short: 3, long: 3, stopPct: 400}, "short window"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, tt.s.init(), tt.want)
		})
	}
}

// Each instrument has its own emax: a bar is seen only by its instrument's, and
// a bar of an instrument it was not given is not traded.
func TestEMAXEach_KeepsStatePerInstrument(t *testing.T) {
	s := &emaxEach{id: "pair", instruments: []string{"EURUSD", "GBPUSD"}, short: 2, long: 3, stopPct: 400}
	require.NoError(t, s.init())
	eur, _ := pairBars()

	for _, b := range eur.data[:3] {
		s.onBar(context.Background(), b)
	}

	assert.Equal(t, long, s.byInstrument["EURUSD"].pos)
	assert.Equal(t, noDirection, s.byInstrument["GBPUSD"].pos, "saw no bars")
	assert.Equal(t, "pair/EURUSD", s.byInstrument["EURUSD"].name())
	assert.Nil(t, s.onBar(context.Background(), hbar("USDCHF", hour(0), 90000, 90000)))
	assert.Equal(t, "pair", s.name())
}

func TestTrader_OnFrame_ReportsABrokerFailureWithTheFrame(t *testing.T) {
	captureLogs(t)
	tr, _ := pairTrader(usd(10000), &reactStrategy{react: func(bar) *order { return nil }})
	eur, gbp := hbar("EURUSD", hour(1), 110000, 110000), hbar("GBPUSD", hour(1), 130000, 130000)

	err := tr.onFrame(context.Background(), frame{availableAt: eur.availableAt, bars: []bar{gbp, eur}})

	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "broker on frame at "), err.Error())
}

func TestTrader_OnBar_ReportsABrokerFailureWithTheBar(t *testing.T) {
	captureLogs(t)
	tr, _ := pairTrader(usd(10000), &reactStrategy{react: func(bar) *order { return nil }})
	b := hbar("EURUSD", hour(1), 110000, 110000)
	b.availableAt = time.Time{}

	err := tr.onBar(context.Background(), b)

	require.ErrorContains(t, err, "no availability")
	assert.True(t, strings.HasPrefix(err.Error(), "broker on bar EURUSD 2010-01-04: "), err.Error())
}
