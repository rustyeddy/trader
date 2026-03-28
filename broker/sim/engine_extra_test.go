package sim

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers shared across this file
// ---------------------------------------------------------------------------

var (
	t0 = time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Minute)
	t2 = t0.Add(2 * time.Minute)
)

func setTick(t *testing.T, e *Engine, instr string, bid, ask float64, tm time.Time) {
	t.Helper()
	require.NoError(t, e.UpdatePrice(market.Tick{
		Instrument: instr,
		Timestamp:  types.FromTime(tm),
		BA: market.BA{
			Bid: types.PriceFromFloat(bid),
			Ask: types.PriceFromFloat(ask),
		},
	}))
}

func buyEURUSD(t *testing.T, e *Engine, units float64, sl, tp *float64) string {
	t.Helper()
	fill := openMarket(t, e, "EURUSD", units, sl, tp)
	return fill.TradeID
}

// ---------------------------------------------------------------------------
// Prices
// ---------------------------------------------------------------------------

func TestPrices_ReturnsNonNil(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	assert.NotNil(t, e.Prices())
}

func TestPrices_SetAndGet(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)

	tick, err := e.Prices().Get("EURUSD")
	require.NoError(t, err)
	assert.Equal(t, types.PriceFromFloat(1.1000), tick.Bid)
}

// ---------------------------------------------------------------------------
// IsTradeOpen
// ---------------------------------------------------------------------------

func TestIsTradeOpen_Unknown(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	assert.False(t, e.IsTradeOpen("nonexistent-id"))
}

func TestIsTradeOpen_OpenTrade(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	id := buyEURUSD(t, e, 10_000, nil, nil)
	assert.True(t, e.IsTradeOpen(id))
}

func TestIsTradeOpen_ClosedTrade(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	id := buyEURUSD(t, e, 10_000, nil, nil)
	require.NoError(t, e.CloseTrade(context.Background(), id, "test"))
	assert.False(t, e.IsTradeOpen(id))
}

// ---------------------------------------------------------------------------
// CloseTrade
// ---------------------------------------------------------------------------

func TestCloseTrade_LongClosesBid(t *testing.T) {
	e, j := newEngine(t, 100_000)

	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	id := buyEURUSD(t, e, 10_000, nil, nil)

	// Move price up so we have a profit.
	setTick(t, e, "EURUSD", 1.1050, 1.1052, t1)
	require.NoError(t, e.CloseTrade(context.Background(), id, "manual"))

	acct, err := e.GetAccount(context.Background())
	require.NoError(t, err)

	// Balance should reflect profit (long closed on bid 1.1050, opened at ask 1.1002).
	assert.Greater(t, acct.Balance.Float64(), 100_000.0)
	assert.False(t, e.IsTradeOpen(id))

	// Should have one trade record with reason "manual".
	require.Len(t, j.trades, 1)
	assert.Equal(t, "manual", j.trades[0].Reason)
}

func TestCloseTrade_ShortClosesAsk(t *testing.T) {
	e, _ := newEngine(t, 100_000)

	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	id := buyEURUSD(t, e, -10_000, nil, nil) // short

	// Move price down for profit.
	setTick(t, e, "EURUSD", 1.0950, 1.0952, t1)
	require.NoError(t, e.CloseTrade(context.Background(), id, ""))

	acct, err := e.GetAccount(context.Background())
	require.NoError(t, err)
	assert.Greater(t, acct.Balance.Float64(), 100_000.0)
}

func TestCloseTrade_DefaultReason(t *testing.T) {
	e, j := newEngine(t, 100_000)
	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	id := buyEURUSD(t, e, 1_000, nil, nil)
	setTick(t, e, "EURUSD", 1.1000, 1.1002, t1)
	require.NoError(t, e.CloseTrade(context.Background(), id, ""))
	require.Len(t, j.trades, 1)
	assert.Equal(t, "ManualClose", j.trades[0].Reason)
}

func TestCloseTrade_TradeNotFound(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	err := e.CloseTrade(context.Background(), "bogus-id", "test")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTradeNotFound)
}

func TestCloseTrade_AlreadyClosed(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	id := buyEURUSD(t, e, 1_000, nil, nil)
	require.NoError(t, e.CloseTrade(context.Background(), id, "first"))

	err := e.CloseTrade(context.Background(), id, "second")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTradeAlreadyClosed)
}

// ---------------------------------------------------------------------------
// CloseAll
// ---------------------------------------------------------------------------

func TestCloseAll_EmptyEngine(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	// No open trades – CloseAll must return nil without doing anything.
	assert.NoError(t, e.CloseAll(context.Background(), "reason"))
}

func TestCloseAll_ClosesAllOpenTrades(t *testing.T) {
	e, j := newEngine(t, 100_000)

	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	setTick(t, e, "GBPUSD", 1.2700, 1.2703, t0)

	id1 := buyEURUSD(t, e, 10_000, nil, nil)
	fill2 := openMarket(t, e, "GBPUSD", 10_000, nil, nil)
	id2 := fill2.TradeID

	setTick(t, e, "EURUSD", 1.1010, 1.1012, t1)
	setTick(t, e, "GBPUSD", 1.2710, 1.2713, t1)

	require.NoError(t, e.CloseAll(context.Background(), "end_of_data"))

	assert.False(t, e.IsTradeOpen(id1))
	assert.False(t, e.IsTradeOpen(id2))

	require.Len(t, j.trades, 2)
	for _, rec := range j.trades {
		assert.Equal(t, "end_of_data", rec.Reason)
	}
}

func TestCloseAll_DefaultReason(t *testing.T) {
	e, j := newEngine(t, 100_000)
	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	buyEURUSD(t, e, 1_000, nil, nil)
	require.NoError(t, e.CloseAll(context.Background(), ""))
	require.Len(t, j.trades, 1)
	assert.Equal(t, "ManualClose", j.trades[0].Reason)
}

// ---------------------------------------------------------------------------
// Revalue
// ---------------------------------------------------------------------------

func TestRevalue_NoTrades(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	require.NoError(t, e.Revalue())
	acct, err := e.GetAccount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, acct.Balance, acct.Equity)
}

func TestRevalue_OpenLongTrade(t *testing.T) {
	e, _ := newEngine(t, 100_000)

	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	buyEURUSD(t, e, 10_000, nil, nil)

	// Move price up.
	setTick(t, e, "EURUSD", 1.1100, 1.1102, t1)

	// Clear the revalue that happened in setTick, then call Revalue() explicitly.
	require.NoError(t, e.Revalue())

	acct, err := e.GetAccount(context.Background())
	require.NoError(t, err)
	assert.Greater(t, acct.Equity.Float64(), acct.Balance.Float64())
}

func TestRevalue_ClosedTradeIgnored(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	id := buyEURUSD(t, e, 10_000, nil, nil)
	require.NoError(t, e.CloseTrade(context.Background(), id, "test"))

	// No open trades – equity must equal balance.
	require.NoError(t, e.Revalue())
	acct, err := e.GetAccount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, acct.Balance, acct.Equity)
}

// ---------------------------------------------------------------------------
// pnlUnits (package-private helper)
// ---------------------------------------------------------------------------

func TestPnlUnits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		side  int
		entry int32
		exit  int32
		want  types.Units
	}{
		{"long_profit", 1, 110000, 111000, types.Units(1000)},
		{"long_loss", 1, 111000, 110000, types.Units(-1000)},
		{"short_profit", -1, 111000, 110000, types.Units(1000)},
		{"short_loss", -1, 110000, 111000, types.Units(-1000)},
		{"zero_delta", 1, 110000, 110000, 0},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, pnlUnits(tt.side, tt.entry, tt.exit))
		})
	}
}

// ---------------------------------------------------------------------------
// Position.CheckExit
// ---------------------------------------------------------------------------

func TestPosition_CheckExit_NotOpen(t *testing.T) {
	p := &Position{Open: false}
	_, hit := p.CheckExit(market.Candle{})
	assert.False(t, hit)
}

func TestPosition_CheckExit_LongStopHit(t *testing.T) {
	p := &Position{
		Side:  1,
		Entry: types.PriceFromFloat(1.1000),
		Stop:  types.PriceFromFloat(1.0950),
		Take:  types.PriceFromFloat(1.1100),
		Open:  true,
	}
	c := market.Candle{
		Low:  types.PriceFromFloat(1.0940), // <= stop
		High: types.PriceFromFloat(1.1020),
	}
	exitPrice, hit := p.CheckExit(c)
	assert.True(t, hit)
	assert.Equal(t, p.Stop, exitPrice)
}

func TestPosition_CheckExit_LongTakeProfitHit(t *testing.T) {
	p := &Position{
		Side:  1,
		Entry: types.PriceFromFloat(1.1000),
		Stop:  types.PriceFromFloat(1.0950),
		Take:  types.PriceFromFloat(1.1100),
		Open:  true,
	}
	c := market.Candle{
		Low:  types.PriceFromFloat(1.1010), // > stop
		High: types.PriceFromFloat(1.1110), // >= take
	}
	exitPrice, hit := p.CheckExit(c)
	assert.True(t, hit)
	assert.Equal(t, p.Take, exitPrice)
}

func TestPosition_CheckExit_LongNoExit(t *testing.T) {
	p := &Position{
		Side:  1,
		Entry: types.PriceFromFloat(1.1000),
		Stop:  types.PriceFromFloat(1.0950),
		Take:  types.PriceFromFloat(1.1100),
		Open:  true,
	}
	c := market.Candle{
		Low:  types.PriceFromFloat(1.0960),
		High: types.PriceFromFloat(1.1050),
	}
	_, hit := p.CheckExit(c)
	assert.False(t, hit)
}

func TestPosition_CheckExit_ShortStopHit(t *testing.T) {
	p := &Position{
		Side:  -1,
		Entry: types.PriceFromFloat(1.1000),
		Stop:  types.PriceFromFloat(1.1050),
		Take:  types.PriceFromFloat(1.0950),
		Open:  true,
	}
	c := market.Candle{
		Low:  types.PriceFromFloat(1.0980),
		High: types.PriceFromFloat(1.1060), // >= stop
	}
	exitPrice, hit := p.CheckExit(c)
	assert.True(t, hit)
	assert.Equal(t, p.Stop, exitPrice)
}

func TestPosition_CheckExit_ShortTakeProfitHit(t *testing.T) {
	p := &Position{
		Side:  -1,
		Entry: types.PriceFromFloat(1.1000),
		Stop:  types.PriceFromFloat(1.1050),
		Take:  types.PriceFromFloat(1.0950),
		Open:  true,
	}
	c := market.Candle{
		Low:  types.PriceFromFloat(1.0940), // <= take
		High: types.PriceFromFloat(1.1020), // < stop
	}
	exitPrice, hit := p.CheckExit(c)
	assert.True(t, hit)
	assert.Equal(t, p.Take, exitPrice)
}

func TestPosition_CheckExit_ShortNoExit(t *testing.T) {
	p := &Position{
		Side:  -1,
		Entry: types.PriceFromFloat(1.1000),
		Stop:  types.PriceFromFloat(1.1050),
		Take:  types.PriceFromFloat(1.0950),
		Open:  true,
	}
	c := market.Candle{
		Low:  types.PriceFromFloat(1.0960),
		High: types.PriceFromFloat(1.1040),
	}
	_, hit := p.CheckExit(c)
	assert.False(t, hit)
}

// ---------------------------------------------------------------------------
// Trade method: triggerStopLoss
// ---------------------------------------------------------------------------

func TestTrade_TriggerStopLoss(t *testing.T) {
	t.Parallel()

	sl := types.PriceFromFloat(1.0950)
	tests := []struct {
		name  string
		units types.Units
		sl    *types.Price
		price types.Price
		want  bool
	}{
		{"nil_sl", 1000, nil, types.PriceFromFloat(1.0940), false},
		{"long_hit", 1000, &sl, types.PriceFromFloat(1.0940), true},
		{"long_at", 1000, &sl, types.PriceFromFloat(1.0950), true},
		{"long_not_hit", 1000, &sl, types.PriceFromFloat(1.0960), false},
		{"short_hit", -1000, &sl, types.PriceFromFloat(1.0960), true},
		{"short_at", -1000, &sl, types.PriceFromFloat(1.0950), true},
		{"short_not_hit", -1000, &sl, types.PriceFromFloat(1.0940), false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := &Trade{Units: tt.units, StopLoss: tt.sl}
			assert.Equal(t, tt.want, tr.triggerStopLoss(tt.price))
		})
	}
}

// ---------------------------------------------------------------------------
// Trade method: triggerTakeProfit
// ---------------------------------------------------------------------------

func TestTrade_TriggerTakeProfit(t *testing.T) {
	t.Parallel()

	tp := types.PriceFromFloat(1.1050)
	tests := []struct {
		name  string
		units types.Units
		tp    *types.Price
		price types.Price
		want  bool
	}{
		{"nil_tp", 1000, nil, types.PriceFromFloat(1.1060), false},
		{"long_hit", 1000, &tp, types.PriceFromFloat(1.1060), true},
		{"long_at", 1000, &tp, types.PriceFromFloat(1.1050), true},
		{"long_not_hit", 1000, &tp, types.PriceFromFloat(1.1040), false},
		{"short_hit", -1000, &tp, types.PriceFromFloat(1.1040), true},
		{"short_at", -1000, &tp, types.PriceFromFloat(1.1050), true},
		{"short_not_hit", -1000, &tp, types.PriceFromFloat(1.1060), false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := &Trade{Units: tt.units, TakeProfit: tt.tp}
			assert.Equal(t, tt.want, tr.triggerTakeProfit(tt.price))
		})
	}
}

// ---------------------------------------------------------------------------
// Trade method: UnrealizedPL
// ---------------------------------------------------------------------------

func TestTrade_UnrealizedPL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		units          types.Units
		entry          float64
		current        float64
		quoteToAccount types.Price
		wantSign       int // +1 profit, -1 loss, 0 flat
	}{
		{"long_profit", 1000, 1.2000, 1.2050, 100000, 1},
		{"long_loss", 1000, 1.2000, 1.1950, 100000, -1},
		{"short_profit", -1000, 1.2000, 1.1950, 100000, 1},
		{"short_loss", -1000, 1.2000, 1.2050, 100000, -1},
		{"flat", 1000, 1.2000, 1.2000, 100000, 0},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := &Trade{
				Units:      tt.units,
				EntryPrice: types.PriceFromFloat(tt.entry),
			}
			pl := tr.UnrealizedPL(types.PriceFromFloat(tt.current), tt.quoteToAccount)
			switch tt.wantSign {
			case 1:
				assert.Greater(t, int64(pl), int64(0), "expected profit")
			case -1:
				assert.Less(t, int64(pl), int64(0), "expected loss")
			case 0:
				assert.Equal(t, types.Money(0), pl)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TakeProfit and StopLoss trigger paths in UpdatePrice
// ---------------------------------------------------------------------------

func TestUpdatePrice_TakeProfitTriggered(t *testing.T) {
	e, j := newEngine(t, 100_000)

	setTick(t, e, "EURUSD", 1.1000, 1.1002, t0)
	tp := 1.1050
	id := buyEURUSD(t, e, 10_000, nil, &tp)

	setTick(t, e, "EURUSD", 1.1050, 1.1052, t1)

	assert.False(t, e.IsTradeOpen(id))
	require.Len(t, j.trades, 1)
	assert.Equal(t, "TakeProfit", j.trades[0].Reason)
}

func TestUpdatePrice_NoMatchingTrade(t *testing.T) {
	e, _ := newEngine(t, 100_000)
	// Open a EURUSD trade using zero-spread prices so there is no spread loss.
	setTick(t, e, "EURUSD", 1.1002, 1.1002, t0)
	id := buyEURUSD(t, e, 1_000, nil, nil)

	// Update a completely different instrument – must NOT close the EURUSD trade.
	setTick(t, e, "GBPUSD", 1.2700, 1.2700, t1)

	// The EURUSD trade should still be open.
	assert.True(t, e.IsTradeOpen(id))
}
