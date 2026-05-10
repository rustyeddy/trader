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

func testOpenPosition(t *testing.T, acct *Account, inst string, side Side, units Units, fill Price) *Position {
	t.Helper()
	pos := &Position{
		TradeCommon: &TradeCommon{
			ID:         NewULID(),
			Instrument: inst,
			Side:       side,
			Units:      units,
		},
		FillPrice: fill,
		FillTime:  Timestamp(100),
	}
	require.NoError(t, acct.AddPosition(context.Background(), pos))
	return pos
}

func TestGapBarsSince(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, gapBarsSince(0, 100, M1))
	assert.Equal(t, 0, gapBarsSince(100, 160, M1))
	assert.Equal(t, 1, gapBarsSince(100, 220, M1))
	assert.Equal(t, 2, gapBarsSince(100, 280, M1))
}

func TestSnapshotPositions(t *testing.T) {
	t.Parallel()

	empty := snapshotPositions(nil)
	require.NotNil(t, empty)
	assert.Equal(t, 0, empty.Len())

	src := &Positions{}
	pos := &Position{TradeCommon: &TradeCommon{ID: "p1", Instrument: "EURUSD", Side: Long, Units: 10}, FillPrice: PriceFromFloat(1.1)}
	src.Add(pos)

	cp := snapshotPositions(src)
	require.NotNil(t, cp)
	assert.Equal(t, 1, cp.Len())

	src.Delete("p1")
	assert.Equal(t, 0, src.Len())
	assert.Equal(t, 1, cp.Len(), "snapshot should not change after source map mutation")
}

func TestClosePositionAtPrice_Validation(t *testing.T) {
	t.Parallel()

	acct := testSizedAccount()
	err := closePositionAtPrice(nil, &Position{}, PriceFromFloat(1.2), Timestamp(10))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil account")

	err = closePositionAtPrice(acct, nil, PriceFromFloat(1.2), Timestamp(10))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil position")
}

func TestClosePositionAtPrice_AndForceClosePositionAtEnd(t *testing.T) {
	t.Parallel()

	acct := testSizedAccount()
	pos := testOpenPosition(t, acct, "EURUSD", Long, 100_000, PriceFromFloat(1.1000))

	err := closePositionAtPrice(acct, pos, PriceFromFloat(1.1010), Timestamp(200))
	require.NoError(t, err)
	assert.Equal(t, 0, acct.Positions.Len())
	require.Len(t, acct.Trades, 1)
	assert.Equal(t, Timestamp(200), acct.Trades[0].FillTime)
	assert.Equal(t, PriceFromFloat(1.1010), acct.Trades[0].FillPrice)

	pos2 := testOpenPosition(t, acct, "EURUSD", Short, 50_000, PriceFromFloat(1.1050))
	last := Candle{Close: PriceFromFloat(1.1020)}
	err = forceClosePositionAtEnd(acct, pos2, last, Timestamp(300))
	require.NoError(t, err)
	require.Len(t, acct.Trades, 2)
	assert.Equal(t, PriceFromFloat(1.1020), acct.Trades[1].FillPrice)
	assert.Equal(t, Timestamp(300), acct.Trades[1].FillTime)
}

func TestClosePositionFromRequest(t *testing.T) {
	t.Parallel()

	acct := testSizedAccount()
	pos := testOpenPosition(t, acct, "EURUSD", Long, 10_000, PriceFromFloat(1.2000))
	fallback := CandleTime{Candle: Candle{Close: PriceFromFloat(1.1995)}, Timestamp: Timestamp(250)}

	err := closePositionFromRequest(acct, pos, nil, fallback)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil close request")

	pos = testOpenPosition(t, acct, "EURUSD", Long, 10_000, PriceFromFloat(1.2000))
	cl := &closeRequest{}
	err = closePositionFromRequest(acct, pos, cl, fallback)
	require.NoError(t, err)
	require.NotEmpty(t, acct.Trades)
	lastTrade := acct.Trades[len(acct.Trades)-1]
	assert.Equal(t, fallback.Close, lastTrade.FillPrice)
	assert.Equal(t, fallback.Timestamp, lastTrade.FillTime)
}

func TestFirstMatchingCloseAndFirstOpenRequest(t *testing.T) {
	t.Parallel()

	posA := &Position{TradeCommon: &TradeCommon{ID: "a"}}
	posB := &Position{TradeCommon: &TradeCommon{ID: "b"}}
	cl1 := &closeRequest{Position: posB}
	cl2 := &closeRequest{Position: posA}
	open := &OpenRequest{Request: Request{Reason: "open"}}

	assert.Nil(t, firstMatchingClose(nil, posA))
	assert.Nil(t, firstMatchingClose(&StrategyPlan{}, nil))

	plan := &StrategyPlan{Closes: []*closeRequest{nil, cl1, cl2}, Opens: []*OpenRequest{open}}
	assert.Same(t, cl2, firstMatchingClose(plan, posA))
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

	long := &Position{TradeCommon: &TradeCommon{Side: Long, Stop: PriceFromFloat(1.0900), Take: PriceFromFloat(1.1100)}}
	px, reason, hit := checkExit(long, Candle{Low: PriceFromFloat(1.0890), High: PriceFromFloat(1.1110)})
	assert.True(t, hit)
	assert.Equal(t, long.Stop, px)
	assert.Contains(t, reason, "same bar")

	px, reason, hit = checkExit(long, Candle{Low: PriceFromFloat(1.0895), High: PriceFromFloat(1.1000)})
	assert.True(t, hit)
	assert.Equal(t, long.Stop, px)
	assert.Equal(t, "STOP", reason)

	short := &Position{TradeCommon: &TradeCommon{Side: Short, Stop: PriceFromFloat(1.1100), Take: PriceFromFloat(1.0900)}}
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
