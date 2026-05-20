package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSizedAccount() *Account {
	acct := NewAccount("test", MoneyFromFloat(10_000))
	acct.Equity = acct.Balance
	acct.RiskPct = RateFromFloat(0.01)
	return acct
}

func testOpenLot(t *testing.T, acct *Account, inst string, side Side, units Units, fill Price) *Lot {
	t.Helper()
	lot := &Lot{
		TradeCommon: &TradeCommon{
			ID:         NewULID(),
			Instrument: inst,
			Side:       side,
			Units:      units,
		},
		EntryPrice:     fill,
		EntryTime:      Timestamp(100),
		OriginalUnits:  units,
		RemainingUnits: units,
		State:          LotOpen,
	}
	require.NoError(t, acct.AddLot(context.Background(), lot))
	return lot
}

func TestGapBarsSince(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, gapBarsSince(0, 100, M1))
	assert.Equal(t, 0, gapBarsSince(100, 160, M1))
	assert.Equal(t, 1, gapBarsSince(100, 220, M1))
	assert.Equal(t, 2, gapBarsSince(100, 280, M1))
}

func TestSnapshotLots(t *testing.T) {
	t.Parallel()

	// snapshotLots is in trader.go — test via indirect usage through BacktestRun.
	// Directly we can test LotBook copying behavior.
	src := &LotBook{}
	lot := &Lot{TradeCommon: &TradeCommon{ID: "p1", Instrument: "EURUSD", Side: Long, Units: 10}, EntryPrice: PriceFromFloat(1.1), OriginalUnits: 10, RemainingUnits: 10, State: LotOpen}
	src.Add(lot)

	// Use snapshotLots function from trader.go
	cp := snapshotLots(src)
	require.NotNil(t, cp)
	assert.Equal(t, 1, cp.Len())

	src.Delete("p1")
	assert.Equal(t, 0, src.Len())
	assert.Equal(t, 1, cp.Len(), "snapshot should not change after source map mutation")
}

func TestCloseLotAtPrice_Validation(t *testing.T) {
	t.Parallel()

	acct := testSizedAccount()
	err := closeLotAtPrice(nil, &Lot{TradeCommon: &TradeCommon{ID: NewULID()}}, PriceFromFloat(1.2), Timestamp(10))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil account")

	err = closeLotAtPrice(acct, nil, PriceFromFloat(1.2), Timestamp(10))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil position")
}

func TestCloseLotAtPrice_AndForceLotCloseAtEnd(t *testing.T) {
	t.Parallel()

	acct := testSizedAccount()
	lot := testOpenLot(t, acct, "EURUSD", Long, 100_000, PriceFromFloat(1.1000))

	err := closeLotAtPrice(acct, lot, PriceFromFloat(1.1010), Timestamp(200))
	require.NoError(t, err)
	assert.Equal(t, 0, acct.Lots.Len())
	require.Len(t, acct.Trades, 1)
	assert.Equal(t, Timestamp(200), acct.Trades[0].ExitTime)
	assert.Equal(t, PriceFromFloat(1.1010), acct.Trades[0].ExitPrice)

	lot2 := testOpenLot(t, acct, "EURUSD", Short, 50_000, PriceFromFloat(1.1050))
	last := Candle{Close: PriceFromFloat(1.1020)}
	err = forceLotCloseAtEnd(acct, lot2, last, Timestamp(300))
	require.NoError(t, err)
	require.Len(t, acct.Trades, 2)
	assert.Equal(t, PriceFromFloat(1.1020), acct.Trades[1].ExitPrice)
	assert.Equal(t, Timestamp(300), acct.Trades[1].ExitTime)
}

func TestCloseLotFromRequest(t *testing.T) {
	t.Parallel()

	acct := testSizedAccount()
	lot := testOpenLot(t, acct, "EURUSD", Long, 10_000, PriceFromFloat(1.2000))
	fallback := CandleTime{Candle: Candle{Close: PriceFromFloat(1.1995)}, Timestamp: Timestamp(250)}

	err := closeLotFromRequest(acct, lot, nil, fallback)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil close request")

	lot = testOpenLot(t, acct, "EURUSD", Long, 10_000, PriceFromFloat(1.2000))
	cl := &closeRequest{}
	err = closeLotFromRequest(acct, lot, cl, fallback)
	require.NoError(t, err)
	require.NotEmpty(t, acct.Trades)
	lastTrade := acct.Trades[len(acct.Trades)-1]
	assert.Equal(t, fallback.Close, lastTrade.ExitPrice)
	assert.Equal(t, fallback.Timestamp, lastTrade.ExitTime)
}

func TestFirstMatchingCloseAndFirstOpenRequest(t *testing.T) {
	t.Parallel()

	lotA := &Lot{TradeCommon: &TradeCommon{ID: "a"}}
	lotB := &Lot{TradeCommon: &TradeCommon{ID: "b"}}
	cl1 := &closeRequest{Lot: lotB}
	cl2 := &closeRequest{Lot: lotA}
	open := &OpenRequest{Request: Request{Reason: "open"}}

	assert.Nil(t, firstMatchingClose(nil, lotA))
	assert.Nil(t, firstMatchingClose(&StrategyPlan{}, nil))

	plan := &StrategyPlan{Closes: []*closeRequest{nil, cl1, cl2}, Opens: []*OpenRequest{open}}
	assert.Same(t, cl2, firstMatchingClose(plan, lotA))
	assert.Same(t, open, firstOpenRequest(plan))
	assert.Nil(t, firstOpenRequest(nil))
	assert.Nil(t, firstOpenRequest(&StrategyPlan{}))
}

func TestEnsureSizedOpenRequest(t *testing.T) {
	t.Parallel()

	acct := testSizedAccount()

	require.NoError(t, ensureSizedOpenRequest(acct, nil))

	ready := &OpenRequest{Request: Request{TradeCommon: &TradeCommon{ID: "x", Instrument: "EURUSD", Side: Long, Units: 1, Stop: PriceFromFloat(1.1)}, Price: PriceFromFloat(1.2)}}
	require.NoError(t, ensureSizedOpenRequest(acct, ready))
	assert.Equal(t, Units(1), ready.Units)

	missingStop := &OpenRequest{Request: Request{TradeCommon: &TradeCommon{ID: "y", Instrument: "EURUSD", Side: Long}, Price: PriceFromFloat(1.2)}}
	err := ensureSizedOpenRequest(acct, missingStop)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stop price")

	needsSizing := &OpenRequest{Request: Request{TradeCommon: &TradeCommon{ID: "z", Instrument: "EURUSD", Side: Long, Stop: PriceFromFloat(1.1950)}, Price: PriceFromFloat(1.2000)}}
	err = ensureSizedOpenRequest(acct, needsSizing)
	require.NoError(t, err)
	assert.Greater(t, int64(needsSizing.Units), int64(0))
}

func TestCheckExit(t *testing.T) {
	t.Parallel()

	assert.Equal(t, false, func() bool {
		_, _, hit := checkExit(nil, Candle{})
		return hit
	}())

	long := &Lot{TradeCommon: &TradeCommon{Side: Long, Stop: PriceFromFloat(1.0900), Take: PriceFromFloat(1.1100)}}
	px, reason, hit := checkExit(long, Candle{Low: PriceFromFloat(1.0890), High: PriceFromFloat(1.1110)})
	assert.True(t, hit)
	assert.Equal(t, long.Stop, px)
	assert.Contains(t, reason, "same bar")

	px, reason, hit = checkExit(long, Candle{Low: PriceFromFloat(1.0895), High: PriceFromFloat(1.1000)})
	assert.True(t, hit)
	assert.Equal(t, long.Stop, px)
	assert.Equal(t, "STOP", reason)

	short := &Lot{TradeCommon: &TradeCommon{Side: Short, Stop: PriceFromFloat(1.1100), Take: PriceFromFloat(1.0900)}}
	px, reason, hit = checkExit(short, Candle{Low: PriceFromFloat(1.0890), High: PriceFromFloat(1.1110)})
	assert.True(t, hit)
	assert.Equal(t, short.Stop, px)
	assert.Contains(t, reason, "same bar")

	px, reason, hit = checkExit(short, Candle{Low: PriceFromFloat(1.0890), High: PriceFromFloat(1.1050)})
	assert.True(t, hit)
	assert.Equal(t, short.Take, px)
	assert.Equal(t, "TAKE", reason)

	px, reason, hit = checkExit(short, Candle{Low: PriceFromFloat(1.0950), High: PriceFromFloat(1.1050)})
	assert.False(t, hit)
	assert.Equal(t, Price(0), px)
	assert.Equal(t, "", reason)
}

func TestAutoCloseExits_StopAndTake(t *testing.T) {
	t.Parallel()

	acct := NewAccount("test", MoneyFromFloat(10_000))
	b := NewBroker("test")
	b.Account = acct

	// Open a long lot with stop below and take above current price.
	stopLot := testOpenLot(t, acct, "EURUSD", Long, 10_000, PriceFromFloat(1.1000))
	stopLot.Stop = PriceFromFloat(1.0950)
	stopLot.Take = PriceFromFloat(1.1200)

	// Open a second lot whose stop is not hit by this bar.
	safeLot := testOpenLot(t, acct, "EURUSD", Long, 10_000, PriceFromFloat(1.1000))
	safeLot.Stop = PriceFromFloat(1.0800)
	safeLot.Take = PriceFromFloat(1.1200)

	// Bar whose low dips below stopLot's stop but not safeLot's stop.
	candle := CandleTime{
		Candle:    Candle{Open: PriceFromFloat(1.1000), High: PriceFromFloat(1.1050), Low: PriceFromFloat(1.0940), Close: PriceFromFloat(1.1010)},
		Timestamp: Timestamp(1000),
	}

	n, err := autoCloseExits(context.Background(), b, candle, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only the stop lot should have been auto-closed")

	assert.Equal(t, 1, acct.Lots.Len(), "one lot should remain open")
	assert.Equal(t, safeLot.ID, acct.Lots.Slice()[0].ID, "safe lot should still be open")
	require.Len(t, acct.Trades, 1, "one closed trade recorded")
	assert.Equal(t, CloseStopLoss, acct.Trades[0].CloseCause)
	assert.Equal(t, stopLot.Stop, acct.Trades[0].ExitPrice, "exit price should be the stop level")
}

func TestAutoCloseExits_TakeProfit(t *testing.T) {
	t.Parallel()

	acct := NewAccount("test", MoneyFromFloat(10_000))
	b := NewBroker("test")
	b.Account = acct

	lot := testOpenLot(t, acct, "EURUSD", Long, 10_000, PriceFromFloat(1.1000))
	lot.Stop = PriceFromFloat(1.0900)
	lot.Take = PriceFromFloat(1.1100)

	candle := CandleTime{
		Candle:    Candle{Open: PriceFromFloat(1.1050), High: PriceFromFloat(1.1120), Low: PriceFromFloat(1.1040), Close: PriceFromFloat(1.1110)},
		Timestamp: Timestamp(2000),
	}

	n, err := autoCloseExits(context.Background(), b, candle, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 0, acct.Lots.Len())
	require.Len(t, acct.Trades, 1)
	assert.Equal(t, CloseTakeProfit, acct.Trades[0].CloseCause)
	assert.Equal(t, lot.Take, acct.Trades[0].ExitPrice)
}
