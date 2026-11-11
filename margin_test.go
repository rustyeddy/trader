package trader

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// marginAccount has a balance and one long EURUSD position of 100000 units
// bought and marked at 1.10000: $2200 of margin, so equity of $2200 is a
// margin level of 100% and $1100 is 50%.
func marginAccount(balance money) *account {
	a := newAccount("a", balance)
	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 100000, entryPrice: 110000, markPrice: 110000}
	return a
}

func TestMarginStatus_String(t *testing.T) {
	assert.Equal(t, "healthy", marginHealthy.String())
	assert.Equal(t, "warning", marginWarning.String())
	assert.Equal(t, "liquidate", marginLiquidate.String())
	assert.Equal(t, "unknown", marginStatus(99).String())
}

func TestDefaultMarginPolicy(t *testing.T) {
	tests := []struct {
		name    string
		balance money
		want    marginStatus
	}{
		{"plenty of margin", usd(10000), marginHealthy},
		{"exactly 100% is healthy", usd(2200), marginHealthy},
		{"one cent under 100% warns", usd(2200) - 1, marginWarning},
		{"exactly 50% only warns", usd(1100), marginWarning},
		{"one cent under 50% liquidates", usd(1100) - 1, marginLiquidate},
		{"negative equity liquidates", -usd(5), marginLiquidate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := defaultMarginPolicy(marginAccount(tt.balance))

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultMarginPolicy_NoPositionsIsHealthyEvenWithNoMoney(t *testing.T) {
	got, err := defaultMarginPolicy(newAccount("a", -usd(5)))

	require.NoError(t, err)
	assert.Equal(t, marginHealthy, got)
}

func TestDefaultMarginPolicy_UnrealizedLossCountsTowardEquity(t *testing.T) {
	a := marginAccount(usd(2300)) // 104% of the $2200 margin
	got, err := defaultMarginPolicy(a)
	require.NoError(t, err)
	require.Equal(t, marginHealthy, got)

	// 1000 points down on 100000 units is -$1000: equity $1300 against $2180.
	a.positions["EURUSD"].markPrice = 109000
	got, err = defaultMarginPolicy(a)

	require.NoError(t, err)
	assert.Equal(t, marginWarning, got)
}

func TestLevelBelow_IsExactAtTheBoundary(t *testing.T) {
	assert.False(t, levelBelow(50, 100, rate(rateScale/2)), "50/100 is not below 50%")
	assert.True(t, levelBelow(49, 100, rate(rateScale/2)))
	assert.True(t, levelBelow(math.MinInt64, 1, rate(rateScale/2)), "no overflow at the extremes")
	assert.False(t, levelBelow(math.MaxInt64, math.MaxInt64, rate(rateScale)), "no overflow at the extremes")
}

func TestCloseWorstLoser(t *testing.T) {
	registerInstrument(t, testInstrument("GBPUSD"))
	registerInstrument(t, testInstrument("AUDUSD"))
	a := newAccount("a", usd(1000))

	none, err := closeWorstLoser(a)
	require.NoError(t, err)
	assert.Nil(t, none, "nothing to close")

	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 10000, entryPrice: 110000, markPrice: 109000} // -$100
	a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: 10000, entryPrice: 150000, markPrice: 140000} // -$1000
	a.positions["AUDUSD"] = &position{instrument: "AUDUSD", direction: long, units: 10000, entryPrice: 70000, markPrice: 80000}   // +$1000

	got, err := closeWorstLoser(a)

	require.NoError(t, err)
	assert.Equal(t, "GBPUSD", got.instrument)
}

func TestCloseWorstLoser_TiesGoToTheLowestInstrument(t *testing.T) {
	registerInstrument(t, testInstrument("GBPUSD"))
	a := newAccount("a", usd(1000))
	a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: 1, entryPrice: 100000, markPrice: 100000}
	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 1, entryPrice: 100000, markPrice: 100000}

	for i := 0; i < 20; i++ { // map order varies run to run
		got, err := closeWorstLoser(a)
		require.NoError(t, err)
		require.Equal(t, "EURUSD", got.instrument)
	}
}

func TestCloseLargestMargin(t *testing.T) {
	registerInstrument(t, testInstrument("GBPUSD"))
	a := newAccount("a", usd(1000))

	none, err := closeLargestMargin(a)
	require.NoError(t, err)
	assert.Nil(t, none)

	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 10000, markPrice: 110000}  // $220
	a.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: short, units: 50000, markPrice: 150000} // $1500

	got, err := closeLargestMargin(a)

	require.NoError(t, err)
	assert.Equal(t, "GBPUSD", got.instrument)
}

func TestAccount_PositionSymbolsAreSorted(t *testing.T) {
	a := newAccount("a", 0)
	for _, s := range []string{"USDJPY", "EURUSD", "GBPUSD", "AUDUSD"} {
		a.positions[s] = &position{instrument: s}
	}
	assert.Equal(t, []string{"AUDUSD", "EURUSD", "GBPUSD", "USDJPY"}, a.positionSymbols())
}

// liquidationBroker has one account with long EURUSD and long GBPUSD positions
// where the account is under water.
func liquidationBroker(t *testing.T, balance money) (*broker, *account) {
	t.Helper()
	captureLogs(t)
	registerInstrument(t, testInstrument("GBPUSD"))
	br := newBroker("t")
	act := br.addAccount("a", balance)
	// EURUSD: 100000 units, $2200 margin, -$1000 unrealized after the bar's mark.
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 100000, entryPrice: 111000, markPrice: 111000}
	// GBPUSD: 10000 units at 1.50, $300 margin, flat.
	act.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: 10000, entryPrice: 150000, markPrice: 150000}
	return br, act
}

// A bar that marks EURUSD at 1.10000 (the close is the open + 100).
func lossBar() bar { return fillBar(1, 109900) }

// lossFrame is lossBar with a GBPUSD bar at the same time, flat at 1.50, plus
// a flat bar for each extra instrument, so every position has a current price.
func lossFrame(extra ...string) frame {
	b := lossBar()
	f := frame{availableAt: b.availableAt, bars: []bar{b}}
	for _, inst := range append([]string{"GBPUSD"}, extra...) {
		f.bars = append(f.bars, daily(bar{instrument: inst, Time: b.Time, ohlc: ohlc{o: 150000, h: 150000, l: 150000, c: 150000}}))
	}
	return f
}

func TestBroker_OnBar_WarnsButClosesNothing(t *testing.T) {
	br, act := liquidationBroker(t, usd(2400))
	logs := captureLogs(t)
	// equity = 2400 - 1000 = $1400 against $2500 of margin: 56%, warning only.

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	assert.Contains(t, logs.String(), "margin warning")
	assert.NotContains(t, logs.String(), "margin closeout")
	assert.Len(t, act.positions, 2)
}

func TestBroker_OnBar_ProgressiveCloseoutStopsOnceHealthyEnough(t *testing.T) {
	br, act := liquidationBroker(t, usd(2000))
	logs := captureLogs(t)
	// equity = 2000 - 1000 = $1000 against $2500: 40%, below the 50% closeout.
	// Closing the worst loser (EURUSD, realizing -$1000) leaves $1000 of
	// equity against $300 of margin: no longer liquidating.

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	assert.Contains(t, logs.String(), "margin closeout")
	assert.NotContains(t, act.positions, "EURUSD", "the worst loser was closed")
	assert.Contains(t, act.positions, "GBPUSD", "progressive: the rest is left alone")
	assert.Equal(t, usd(1000), act.balance, "the loss was booked")
}

func TestBroker_OnFrame_CompleteCloseoutClosesEverything(t *testing.T) {
	br, act := liquidationBroker(t, usd(2000))
	br.completeCloseout = true

	require.NoError(t, br.onFrame(context.Background(), lossFrame()))

	assert.Empty(t, act.positions)
	assert.Equal(t, usd(1000), act.balance)
}

// A complete closeout that defers a position without a price stays owed,
// though closing the first position restored margin health: with $2000,
// closing EURUSD realizes -$1000, leaving $1000 against GBPUSD's $300 of
// margin. GBPUSD has no bar until the third frame; meanwhile every open is
// refused, and the third frame closes GBPUSD at its price.
func TestBroker_OnFrame_DeferredCompleteCloseoutResumesUntilEverythingIsClosed(t *testing.T) {
	br, act := liquidationBroker(t, usd(2000))
	br.completeCloseout = true
	ctx := context.Background()

	require.NoError(t, br.onBar(ctx, lossBar()))
	require.NotContains(t, act.positions, "EURUSD")
	require.Contains(t, act.positions, "GBPUSD", "deferred: GBPUSD had no bar")
	require.Equal(t, usd(1000), act.balance)
	require.True(t, act.closeoutPending)

	require.NoError(t, br.onBar(ctx, fillBar(2, 110000)))
	assert.Contains(t, act.positions, "GBPUSD", "still no GBPUSD bar")
	o := brokerOpen("a")
	o.Time, o.decidedAt = testStart.AddDate(0, 0, 2), testStart.AddDate(0, 0, 3) // decided after the closeout's frame
	err := br.submit(ctx, o)
	require.ErrorIs(t, err, errCloseoutGuard, "the closeout is still owed")

	gbp := daily(bar{instrument: "GBPUSD", Time: testStart.AddDate(0, 0, 3), ohlc: ohlc{o: 149000, h: 149000, l: 148000, c: 148000}})
	require.NoError(t, br.onBar(ctx, gbp))
	assert.Empty(t, act.positions)
	assert.False(t, act.closeoutPending)
	require.Len(t, act.trades, 2)
	assert.Equal(t, price(148000), act.trades[1].exitPrice, "closed at GBPUSD's own bar")
	assert.Equal(t, usd(800), act.balance, "10000 units lost 0.02000")
}

// A progressive closeout that defers is not owed: the next frame judges the
// account afresh, and a healthy account keeps its position.
func TestBroker_OnFrame_DeferredProgressiveCloseoutIsJudgedAfresh(t *testing.T) {
	br, act := liquidationBroker(t, usd(2000))
	br.closeout = func(a *account) (*position, error) { return a.positions["GBPUSD"], nil } // GBPUSD first: deferred
	ctx := context.Background()

	require.NoError(t, br.onBar(ctx, lossBar()))
	require.Len(t, act.positions, 2)
	assert.False(t, act.closeoutPending)
}

func TestBroker_OnBar_CompleteCloseoutNeedsALiquidationToStart(t *testing.T) {
	br, act := liquidationBroker(t, usd(10000))
	br.completeCloseout = true

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	assert.Len(t, act.positions, 2, "a healthy account is left alone")
}

func TestBroker_OnFrame_CloseoutOrdersAreRecordedWithTheBarTime(t *testing.T) {
	br, _ := liquidationBroker(t, usd(2000))
	br.completeCloseout = true
	f := lossFrame()
	// Capture the orders the broker makes through the log, as it has no other
	// record of them yet.
	logs := captureLogs(t)

	require.NoError(t, br.onFrame(context.Background(), f))

	out := logs.String()
	assert.Contains(t, out, "cause=\"margin closeout\"")
	assert.Contains(t, out, "order=t-1")
	assert.Contains(t, out, "order=t-2")
	assert.Equal(t, 2, strings.Count(out, "to=filled"), "a liquidation is an ordinary order that ends filled")
	assert.Equal(t, 2, strings.Count(out, "to=requested"))
	assert.Equal(t, 6, strings.Count(out, `cause="margin closeout"`), "requested, submitted and filled, for both orders")
	assert.NotContains(t, out, "to=rejected")
}

func TestBroker_OnBar_CloseoutCancelsTheAccountsPendingOpens(t *testing.T) {
	br, act := liquidationBroker(t, usd(2000))
	pending := brokerOpen("a")
	pending.instrument = "GBPUSD"
	pending.state = stateSubmitted
	pending.id = "t-9"
	pending.Time = testStart.AddDate(0, 0, 5) // not due on the bar below
	br.addAccount("other", usd(10000))
	other := brokerOpen("other")
	other.state = stateSubmitted
	other.Time = testStart.AddDate(0, 0, 5)
	br.orders = map[string][]*order{"GBPUSD": {pending}, "EURUSD": {other}}

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	assert.Equal(t, stateCancelled, pending.state, "accepted earlier, so cancelled, not rejected")
	last := pending.history[len(pending.history)-1]
	assert.Equal(t, "margin closeout", last.reason)
	assert.Equal(t, lossBar().Time, last.time, "stamped with the closeout bar, not the order's own time")
	assert.Empty(t, br.orders["GBPUSD"])
	assert.Contains(t, br.completed, pending, "the cancelled order is retained")
	assert.Equal(t, stateSubmitted, other.state, "another account's order is untouched")
	assert.Equal(t, []*order{other}, br.orders["EURUSD"])
	_ = act
}

func TestBroker_OnBar_CustomPoliciesAreUsed(t *testing.T) {
	br, act := liquidationBroker(t, usd(10000)) // healthy by the default rules
	br.margin = func(a *account) (marginStatus, error) {
		if len(a.positions) > 1 { // a policy of its own: never more than one position
			return marginLiquidate, nil
		}
		return marginHealthy, nil
	}
	br.closeout = closeLargestMargin

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	assert.NotContains(t, act.positions, "EURUSD", "the larger margin user was closed")
	assert.Contains(t, act.positions, "GBPUSD")
}

func TestBroker_OnBar_PolicyErrorsStopTheRun(t *testing.T) {
	boom := errors.New("boom")
	t.Run("margin policy", func(t *testing.T) {
		br, _ := liquidationBroker(t, usd(10000))
		br.margin = func(*account) (marginStatus, error) { return marginHealthy, boom }
		require.ErrorIs(t, br.onBar(context.Background(), lossBar()), boom)
	})
	t.Run("closeout policy", func(t *testing.T) {
		br, _ := liquidationBroker(t, usd(2000))
		br.closeout = func(*account) (*position, error) { return nil, boom }
		require.ErrorIs(t, br.onBar(context.Background(), lossBar()), boom)
	})
	t.Run("margin policy while liquidating", func(t *testing.T) {
		br, _ := liquidationBroker(t, usd(2000))
		calls := 0
		br.margin = func(*account) (marginStatus, error) {
			calls++
			if calls == 1 {
				return marginLiquidate, nil
			}
			return marginHealthy, boom
		}
		require.ErrorIs(t, br.onBar(context.Background(), lossBar()), boom)
	})
}

func TestBroker_OnBar_LiquidationWithNothingToCloseIsFine(t *testing.T) {
	br, _ := liquidationBroker(t, usd(10000))
	br.margin = func(*account) (marginStatus, error) { return marginLiquidate, nil }
	br.closeout = func(*account) (*position, error) { return nil, nil }

	require.NoError(t, br.onBar(context.Background(), lossBar()))
}

func TestBroker_OnBar_ACloseoutThatDoesNotFillIsAnError(t *testing.T) {
	br, act := liquidationBroker(t, usd(2000))
	// A policy that names a position the account does not hold: the close is
	// rejected for want of a position, which would loop forever if ignored.
	ghost := &position{instrument: "EURUSD", direction: long, units: 1, markPrice: 110000}
	br.closeout = func(*account) (*position, error) { return ghost, nil }
	delete(act.positions, "EURUSD")
	delete(act.positions, "GBPUSD")
	act.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: 10000, entryPrice: 150000, markPrice: 150000}
	br.margin = func(*account) (marginStatus, error) { return marginLiquidate, nil }

	err := br.onBar(context.Background(), lossBar())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "closeout EURUSD")
}

func TestBroker_OnBar_CloseoutOrdersAreRecorded(t *testing.T) {
	br, _ := liquidationBroker(t, usd(2000))

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	require.Len(t, br.closeouts(), 1, "progressive: one position was liquidated")
	c := br.closeouts()[0]
	assert.Equal(t, "EURUSD", c.instrument)
	assert.Equal(t, close, c.action)
	assert.Equal(t, reasonMarginCloseout, c.reason)
	assert.Equal(t, stateFilled, c.state)
	assert.Equal(t, lossBar().Time, c.Time)
	assert.Equal(t, []*order{c}, br.completed, "a closeout is also a completed order")
}

func TestBroker_OnFrame_CompleteCloseoutRecordsEveryPosition(t *testing.T) {
	br, _ := liquidationBroker(t, usd(2000))
	br.completeCloseout = true

	require.NoError(t, br.onFrame(context.Background(), lossFrame()))

	require.Len(t, br.closeouts(), 2)
	assert.Equal(t, "EURUSD", br.closeouts()[0].instrument, "worst loser first")
	assert.Equal(t, "GBPUSD", br.closeouts()[1].instrument)
	assert.Len(t, br.completed, 2)
}

func TestBroker_OnFrame_RejectedCloseoutIsKeptAsCompleted(t *testing.T) {
	br, _ := liquidationBroker(t, usd(2000))
	// A policy that names a position the account does not hold: the closing
	// order is rejected, so the closeout fails.
	br.closeout = func(*account) (*position, error) {
		return &position{instrument: "USDJPY", markPrice: 150000}, nil
	}

	err := br.onFrame(context.Background(), lossFrame("USDJPY"))

	require.Error(t, err)
	require.Len(t, br.closeouts(), 1, "the attempt is kept for diagnostics")
	assert.Equal(t, stateRejected, br.closeouts()[0].state)
	assert.Equal(t, br.closeouts(), br.completed, "a rejected closeout is terminal, so completed")
}

func TestBroker_OnFrame_CompletedFollowsEventOrderNotQueueOrder(t *testing.T) {
	br, _ := liquidationBroker(t, usd(1000))
	later := brokerOpen("a") // first in the queue, cancelled by the closeout
	later.id, later.state, later.Time = "t-8", stateSubmitted, testStart.AddDate(0, 0, 5)
	fills := &order{id: "t-9", accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder,
		state: stateSubmitted, Time: testStart} // second in the queue, fills first
	br.orders = map[string][]*order{"EURUSD": {later, fills}}

	require.NoError(t, br.onFrame(context.Background(), lossFrame()))

	require.Len(t, br.completed, 3)
	assert.Same(t, fills, br.completed[0], "filled first, though second in the queue")
	assert.Same(t, later, br.completed[1], "then cancelled by the closeout")
	assert.Equal(t, "GBPUSD", br.completed[2].instrument, "then the liquidation")
}
