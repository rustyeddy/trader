package main

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

func TestMarginLevelPolicy_ErrorsAreReturnedNotHidden(t *testing.T) {
	a := newAccount("a", usd(1))
	a.positions["EURUSD"] = &position{instrument: "EURUSD", units: math.MaxInt64, markPrice: math.MaxInt64, entryPrice: 1}

	_, err := defaultMarginPolicy(a)
	require.ErrorIs(t, err, errSizeOverflow)

	a = newAccount("a", usd(1))
	a.positions["XXXYYY"] = &position{instrument: "XXXYYY", units: 1, markPrice: 1}
	_, err = defaultMarginPolicy(a)
	require.ErrorIs(t, err, errUnknownInstrument)
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

func TestCloseoutPolicies_ReturnErrors(t *testing.T) {
	a := newAccount("a", usd(1))
	a.positions["XXXYYY"] = &position{instrument: "XXXYYY", units: 1, markPrice: 1}
	_, err := closeLargestMargin(a)
	require.ErrorIs(t, err, errUnknownInstrument)

	a = newAccount("a", usd(1))
	a.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: math.MaxInt64, entryPrice: 1, markPrice: math.MaxInt64}
	_, err = closeWorstLoser(a)
	require.ErrorIs(t, err, errSizeOverflow)
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
	br := &broker{id: "t"}
	act := br.addAccount("a", balance)
	// EURUSD: 100000 units, $2200 margin, -$1000 unrealized after the bar's mark.
	act.positions["EURUSD"] = &position{instrument: "EURUSD", direction: long, units: 100000, entryPrice: 111000, markPrice: 111000}
	// GBPUSD: 10000 units at 1.50, $300 margin, flat.
	act.positions["GBPUSD"] = &position{instrument: "GBPUSD", direction: long, units: 10000, entryPrice: 150000, markPrice: 150000}
	return br, act
}

// A bar that marks EURUSD at 1.10000 (the close is the open + 100).
func lossBar() bar { return fillBar(1, 109900) }

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

func TestBroker_OnBar_CompleteCloseoutClosesEverything(t *testing.T) {
	br, act := liquidationBroker(t, usd(2000))
	br.completeCloseout = true

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	assert.Empty(t, act.positions)
	assert.Equal(t, usd(1000), act.balance)
}

func TestBroker_OnBar_CompleteCloseoutNeedsALiquidationToStart(t *testing.T) {
	br, act := liquidationBroker(t, usd(10000))
	br.completeCloseout = true

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	assert.Len(t, act.positions, 2, "a healthy account is left alone")
}

func TestBroker_OnBar_CloseoutOrdersAreRecordedWithTheBarTime(t *testing.T) {
	br, _ := liquidationBroker(t, usd(2000))
	br.completeCloseout = true
	b := lossBar()
	// Capture the orders the broker makes through the log, as it has no other
	// record of them yet.
	logs := captureLogs(t)

	require.NoError(t, br.onBar(context.Background(), b))

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

	require.Len(t, br.closeouts, 1, "progressive: one position was liquidated")
	c := br.closeouts[0]
	assert.Equal(t, "EURUSD", c.instrument)
	assert.Equal(t, close, c.action)
	assert.Equal(t, reasonMarginCloseout, c.reason)
	assert.Equal(t, stateFilled, c.state)
	assert.Equal(t, lossBar().Time, c.Time)
	assert.Equal(t, []*order{c}, br.completed, "a closeout is also a completed order")
}

func TestBroker_OnBar_CompleteCloseoutRecordsEveryPosition(t *testing.T) {
	br, _ := liquidationBroker(t, usd(2000))
	br.completeCloseout = true

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	require.Len(t, br.closeouts, 2)
	assert.Equal(t, "EURUSD", br.closeouts[0].instrument, "worst loser first")
	assert.Equal(t, "GBPUSD", br.closeouts[1].instrument)
	assert.Len(t, br.completed, 2)
}

func TestBroker_OnBar_FailedCloseoutIsKeptButNotCompleted(t *testing.T) {
	br, _ := liquidationBroker(t, usd(2000))
	// A policy that names a position the account does not hold: the closing
	// order is rejected, so the closeout fails.
	br.closeout = func(*account) (*position, error) {
		return &position{instrument: "USDJPY", markPrice: 150000}, nil
	}

	err := br.onBar(context.Background(), lossBar())

	require.Error(t, err)
	require.Len(t, br.closeouts, 1, "the attempt is kept for diagnostics")
	assert.NotEqual(t, stateFilled, br.closeouts[0].state)
	assert.Empty(t, br.completed, "a failed closeout is not a completed order")
}

func TestBroker_OnBar_CompletedFollowsEventOrderNotQueueOrder(t *testing.T) {
	br, _ := liquidationBroker(t, usd(1000))
	later := brokerOpen("a") // first in the queue, cancelled by the closeout
	later.id, later.state, later.Time = "t-8", stateSubmitted, testStart.AddDate(0, 0, 5)
	fills := &order{id: "t-9", accountID: "a", instrument: "EURUSD", action: close, orderType: marketOrder,
		state: stateSubmitted, Time: testStart} // second in the queue, fills first
	br.orders = map[string][]*order{"EURUSD": {later, fills}}

	require.NoError(t, br.onBar(context.Background(), lossBar()))

	require.Len(t, br.completed, 3)
	assert.Same(t, fills, br.completed[0], "filled first, though second in the queue")
	assert.Same(t, later, br.completed[1], "then cancelled by the closeout")
	assert.Equal(t, "GBPUSD", br.completed[2].instrument, "then the liquidation")
}
