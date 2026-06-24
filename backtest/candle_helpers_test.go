package backtest

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/engine"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSizedAccount() *execution.Account {
	acct := execution.NewAccount("test", market.MoneyFromFloat(10_000))
	acct.Equity = acct.Balance
	acct.RiskFraction = market.RateFromFloat(0.01)
	return acct
}

func testOpenLot(t *testing.T, acct *execution.Account, inst string, side market.Side, units market.Units, fill market.Price) *execution.Lot {
	t.Helper()
	lot := &execution.Lot{
		TradeCommon: &execution.TradeCommon{
			ID:         market.NewULID(),
			Instrument: inst,
			Side:       side,
			Units:      units,
		},
		EntryPrice:     fill,
		EntryTime:      market.Timestamp(100),
		OriginalUnits:  units,
		RemainingUnits: units,
		State:          execution.LotOpen,
	}
	require.NoError(t, acct.AddLot(lot))
	return lot
}

func TestSnapshotLots(t *testing.T) {
	t.Parallel()

	// SnapshotLots is in trader.go — test via indirect usage through BacktestRun.
	// Directly we can test LotBook copying behavior.
	src := &execution.LotBook{}
	lot := &execution.Lot{TradeCommon: &execution.TradeCommon{ID: "p1", Instrument: "EURUSD", Side: market.Long, Units: 10}, EntryPrice: market.PriceFromFloat(1.1), OriginalUnits: 10, RemainingUnits: 10, State: execution.LotOpen}
	src.Add(lot)

	// Use SnapshotLots function from the engine package.
	cp := engine.SnapshotLots(src)
	require.NotNil(t, cp)
	assert.Equal(t, 1, cp.Len())

	src.Delete("p1")
	assert.Equal(t, 0, src.Len())
	assert.Equal(t, 1, cp.Len(), "snapshot should not change after source map mutation")
}

func TestCheckExit(t *testing.T) {
	t.Parallel()

	assert.Equal(t, false, func() bool {
		_, _, hit := checkExit(nil, market.Candle{})
		return hit
	}())

	long := &execution.Lot{TradeCommon: &execution.TradeCommon{Side: market.Long, Stop: market.PriceFromFloat(1.0900), Take: market.PriceFromFloat(1.1100)}}
	px, reason, hit := checkExit(long, market.Candle{Low: market.PriceFromFloat(1.0890), High: market.PriceFromFloat(1.1110)})
	assert.True(t, hit)
	assert.Equal(t, long.Stop, px)
	assert.Contains(t, reason, "same bar")

	px, reason, hit = checkExit(long, market.Candle{Low: market.PriceFromFloat(1.0895), High: market.PriceFromFloat(1.1000)})
	assert.True(t, hit)
	assert.Equal(t, long.Stop, px)
	assert.Equal(t, "STOP", reason)

	short := &execution.Lot{TradeCommon: &execution.TradeCommon{Side: market.Short, Stop: market.PriceFromFloat(1.1100), Take: market.PriceFromFloat(1.0900)}}
	px, reason, hit = checkExit(short, market.Candle{Low: market.PriceFromFloat(1.0890), High: market.PriceFromFloat(1.1110)})
	assert.True(t, hit)
	assert.Equal(t, short.Stop, px)
	assert.Contains(t, reason, "same bar")

	px, reason, hit = checkExit(short, market.Candle{Low: market.PriceFromFloat(1.0890), High: market.PriceFromFloat(1.1050)})
	assert.True(t, hit)
	assert.Equal(t, short.Take, px)
	assert.Equal(t, "TAKE", reason)

	px, reason, hit = checkExit(short, market.Candle{Low: market.PriceFromFloat(1.0950), High: market.PriceFromFloat(1.1050)})
	assert.False(t, hit)
	assert.Equal(t, market.Price(0), px)
	assert.Equal(t, "", reason)
}

func TestAutoCloseExits_StopAndTake(t *testing.T) {
	t.Parallel()

	acct := execution.NewAccount("test", market.MoneyFromFloat(10_000))
	b := execution.NewBroker("test")
	b.Account = acct

	// Open a long lot with stop below and take above current price.
	stopLot := testOpenLot(t, acct, "EURUSD", market.Long, 10_000, market.PriceFromFloat(1.1000))
	stopLot.Stop = market.PriceFromFloat(1.0950)
	stopLot.Take = market.PriceFromFloat(1.1200)

	// Open a second lot whose stop is not hit by this bar.
	safeLot := testOpenLot(t, acct, "EURUSD", market.Long, 10_000, market.PriceFromFloat(1.1000))
	safeLot.Stop = market.PriceFromFloat(1.0800)
	safeLot.Take = market.PriceFromFloat(1.1200)

	// Bar whose low dips below stopLot's stop but not safeLot's stop.
	candle := market.CandleTime{
		Candle:    market.Candle{Open: market.PriceFromFloat(1.1000), High: market.PriceFromFloat(1.1050), Low: market.PriceFromFloat(1.0940), Close: market.PriceFromFloat(1.1010)},
		Timestamp: market.Timestamp(1000),
	}

	n, err := autoCloseExits(context.Background(), b, candle, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only the stop lot should have been auto-closed")

	assert.Equal(t, 1, acct.Lots.Len(), "one lot should remain open")
	assert.Equal(t, safeLot.ID, acct.Lots.Slice()[0].ID, "safe lot should still be open")
	require.Len(t, acct.Trades, 1, "one closed trade recorded")
	assert.Equal(t, execution.CloseStopLoss, acct.Trades[0].CloseCause)
	assert.Equal(t, stopLot.Stop, acct.Trades[0].ExitPrice, "exit price should be the stop level")
}

func TestAutoCloseExits_TakeProfit(t *testing.T) {
	t.Parallel()

	acct := execution.NewAccount("test", market.MoneyFromFloat(10_000))
	b := execution.NewBroker("test")
	b.Account = acct

	lot := testOpenLot(t, acct, "EURUSD", market.Long, 10_000, market.PriceFromFloat(1.1000))
	lot.Stop = market.PriceFromFloat(1.0900)
	lot.Take = market.PriceFromFloat(1.1100)

	candle := market.CandleTime{
		Candle:    market.Candle{Open: market.PriceFromFloat(1.1050), High: market.PriceFromFloat(1.1120), Low: market.PriceFromFloat(1.1040), Close: market.PriceFromFloat(1.1110)},
		Timestamp: market.Timestamp(2000),
	}

	n, err := autoCloseExits(context.Background(), b, candle, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 0, acct.Lots.Len())
	require.Len(t, acct.Trades, 1)
	assert.Equal(t, execution.CloseTakeProfit, acct.Trades[0].CloseCause)
	assert.Equal(t, lot.Take, acct.Trades[0].ExitPrice)
}
