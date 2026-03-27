package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/data"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

type fakeFeed struct {
	bars []fakeBar
	idx  int
}

type fakeBar struct {
	ts types.Timestamp
	c  market.Candle
}

func (f *fakeFeed) Next() bool {
	if f.idx >= len(f.bars) {
		return false
	}
	f.idx++
	return true
}

func (f *fakeFeed) Candle() market.Candle {
	return f.bars[f.idx-1].c
}

func (f *fakeFeed) Timestamp() types.Timestamp {
	return f.bars[f.idx-1].ts
}

func (f *fakeFeed) Err() error {
	return nil
}

func (f *fakeFeed) Close() error {
	return nil
}

func TestCandleEngineRun_BuyFirstBarStrategy(t *testing.T) {
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  101000,
					Low:   99000,
					Close: 100500,
					Ticks: 10,
				},
			},
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100500,
					High:  101500,
					Low:   100000,
					Close: 101000,
					Ticks: 12,
				},
			},
		},
	}

	engine := NewCandleEngine(
		"EURUSD",
		types.H1,
		types.PriceScale,
		types.Money(10000),
		"USD",
	)

	err := engine.Run(feed, &BuyFirstBarStrategy{})
	require.NoError(t, err)

	require.False(t, engine.Pos.Open)
	require.Equal(t, Long, engine.Pos.Side)
	require.Equal(t, types.Units(1000), engine.Pos.Units)
	require.Equal(t, types.Price(100500), engine.Pos.EntryPrice)
	require.Len(t, engine.Trades, 1)
	require.Equal(t, types.Price(101000), engine.Trades[0].ExitPrice)

}

func TestCandleEngineRun_TakeProfitClosesTrade(t *testing.T) {
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100500,
					Low:   99500,
					Close: 100000,
					Ticks: 10,
				},
			},
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  101500,
					Low:   99900,
					Close: 101000,
					Ticks: 12,
				},
			},
		},
	}

	engine := NewCandleEngine(
		"EURUSD",
		types.H1,
		types.PriceScale,
		types.Money(10000),
		"USD",
	)

	err := engine.Run(feed, &testTakeProfitStrategy{})
	require.NoError(t, err)

	require.False(t, engine.Pos.Open)
	require.Len(t, engine.Trades, 1)

	tr := engine.Trades[0]
	require.Equal(t, Long, tr.Side)
	require.Equal(t, types.Price(100000), tr.EntryPrice)
	require.Equal(t, types.Price(101000), tr.ExitPrice)
	require.Equal(t, "TAKE", tr.Reason)
}

type testTakeProfitStrategy struct {
	done bool
}

func (s *testTakeProfitStrategy) Name() string {
	return "test-take-profit"
}

func (s *testTakeProfitStrategy) Reset() {
	s.done = false
}

func (s *testTakeProfitStrategy) OnBar(ctx *CandleContext, c market.Candle) *OrderRequest {
	if s.done || ctx.Pos.Open {
		return nil
	}
	s.done = true

	return &OrderRequest{
		Side:   Long,
		Units:  types.Units(1000),
		Take:   types.Price(101000),
		Reason: "enter with take",
	}
}

func TestCandleEngineRun_StopLossClosesTrade(t *testing.T) {
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100500,
					Low:   99500,
					Close: 100000,
					Ticks: 10,
				},
			},
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100100,
					Low:   98900,
					Close: 99000,
					Ticks: 12,
				},
			},
		},
	}

	engine := NewCandleEngine(
		"EURUSD",
		types.H1,
		types.PriceScale,
		types.Money(10000),
		"USD",
	)

	err := engine.Run(feed, &testStopLossStrategy{})
	require.NoError(t, err)

	require.False(t, engine.Pos.Open)
	require.Len(t, engine.Trades, 1)

	tr := engine.Trades[0]
	require.Equal(t, Long, tr.Side)
	require.Equal(t, types.Price(100000), tr.EntryPrice)
	require.Equal(t, types.Price(99000), tr.ExitPrice)
	require.Equal(t, "STOP", tr.Reason)
}

type testStopLossStrategy struct {
	done bool
}

func (s *testStopLossStrategy) Name() string {
	return "test-stop-loss"
}

func (s *testStopLossStrategy) Reset() {
	s.done = false
}

func (s *testStopLossStrategy) OnBar(ctx *CandleContext, c market.Candle) *OrderRequest {
	if s.done || ctx.Pos.Open {
		return nil
	}
	s.done = true

	return &OrderRequest{
		Side:   Long,
		Units:  types.Units(1000),
		Stop:   types.Price(99000),
		Reason: "enter with stop",
	}
}

func TestCandleEngineRun_SameBarStopAndTake_UsesStopFirst(t *testing.T) {
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100100,
					Low:   99900,
					Close: 100000,
					Ticks: 10,
				},
			},
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  101500, // hits take
					Low:   98900,  // hits stop
					Close: 100500,
					Ticks: 12,
				},
			},
		},
	}

	engine := NewCandleEngine(
		"EURUSD",
		types.H1,
		types.PriceScale,
		types.Money(10000),
		"USD",
	)

	err := engine.Run(feed, &testStopAndTakeStrategy{})
	require.NoError(t, err)

	require.False(t, engine.Pos.Open)
	require.Len(t, engine.Trades, 1)

	tr := engine.Trades[0]
	require.Equal(t, types.Price(100000), tr.EntryPrice)
	require.Equal(t, types.Price(99000), tr.ExitPrice)
	require.Equal(t, "STOP&TAKE same bar (stop-first)", tr.Reason)
}

type testStopAndTakeStrategy struct {
	done bool
}

func (s *testStopAndTakeStrategy) Name() string {
	return "test-stop-and-take"
}

func (s *testStopAndTakeStrategy) Reset() {
	s.done = false
}

func (s *testStopAndTakeStrategy) OnBar(ctx *CandleContext, c market.Candle) *OrderRequest {
	if s.done || ctx.Pos.Open {
		return nil
	}
	s.done = true

	return &OrderRequest{
		Side:   Long,
		Units:  types.Units(1000),
		Stop:   types.Price(99000),
		Take:   types.Price(101000),
		Reason: "enter with stop and take",
	}
}

func TestCandleEngineRun_GapBarsReported(t *testing.T) {
	strat := &captureGapStrategy{}

	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100100,
					Low:   99900,
					Close: 100000,
					Ticks: 10,
				},
			},
			{
				// skips 01:00 and 02:00, next bar is 03:00
				ts: types.FromTime(time.Date(2026, time.January, 1, 3, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100200,
					Low:   99800,
					Close: 100100,
					Ticks: 12,
				},
			},
		},
	}

	engine := NewCandleEngine(
		"EURUSD",
		types.H1,
		types.PriceScale,
		types.Money(10000),
		"USD",
	)

	err := engine.Run(feed, strat)
	require.NoError(t, err)

	require.Equal(t, []int{0, 2}, strat.gaps)
}

type captureGapStrategy struct {
	gaps []int
}

func (s *captureGapStrategy) Name() string {
	return "capture-gap"
}

func (s *captureGapStrategy) Reset() {
	s.gaps = nil
}

func (s *captureGapStrategy) OnBar(ctx *CandleContext, c market.Candle) *OrderRequest {
	s.gaps = append(s.gaps, ctx.GapBars)
	return nil
}

func TestCandleEngineRun_ShortTakeProfitClosesTrade(t *testing.T) {
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100500,
					Low:   99500,
					Close: 100000,
					Ticks: 10,
				},
			},
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100100,
					Low:   98900, // hits short take
					Close: 99000,
					Ticks: 12,
				},
			},
		},
	}

	engine := NewCandleEngine(
		"EURUSD",
		types.H1,
		types.PriceScale,
		types.Money(10000),
		"USD",
	)

	err := engine.Run(feed, &testShortTakeProfitStrategy{})
	require.NoError(t, err)

	require.False(t, engine.Pos.Open)
	require.Len(t, engine.Trades, 1)

	tr := engine.Trades[0]
	require.Equal(t, Short, tr.Side)
	require.Equal(t, types.Price(100000), tr.EntryPrice)
	require.Equal(t, types.Price(99000), tr.ExitPrice)
	require.Equal(t, "TAKE", tr.Reason)
}

type testShortTakeProfitStrategy struct {
	done bool
}

func (s *testShortTakeProfitStrategy) Name() string {
	return "test-short-take-profit"
}

func (s *testShortTakeProfitStrategy) Reset() {
	s.done = false
}

func (s *testShortTakeProfitStrategy) OnBar(ctx *CandleContext, c market.Candle) *OrderRequest {
	if s.done || ctx.Pos.Open {
		return nil
	}
	s.done = true

	return &OrderRequest{
		Side:   Short,
		Units:  types.Units(1000),
		Take:   types.Price(99000),
		Reason: "enter short with take",
	}
}
func TestCandleEngineRun_ShortStopLossClosesTrade(t *testing.T) {
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100500,
					Low:   99500,
					Close: 100000,
					Ticks: 10,
				},
			},
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  101100, // hits short stop
					Low:   99900,
					Close: 101000,
					Ticks: 12,
				},
			},
		},
	}

	engine := NewCandleEngine(
		"EURUSD",
		types.H1,
		types.PriceScale,
		types.Money(10000),
		"USD",
	)

	err := engine.Run(feed, &testShortStopLossStrategy{})
	require.NoError(t, err)

	require.False(t, engine.Pos.Open)
	require.Len(t, engine.Trades, 1)

	tr := engine.Trades[0]
	require.Equal(t, Short, tr.Side)
	require.Equal(t, types.Price(100000), tr.EntryPrice)
	require.Equal(t, types.Price(101000), tr.ExitPrice)
	require.Equal(t, "STOP", tr.Reason)
}

type testShortStopLossStrategy struct {
	done bool
}

func (s *testShortStopLossStrategy) Name() string {
	return "test-short-stop-loss"
}

func (s *testShortStopLossStrategy) Reset() {
	s.done = false
}

func (s *testShortStopLossStrategy) OnBar(ctx *CandleContext, c market.Candle) *OrderRequest {
	if s.done || ctx.Pos.Open {
		return nil
	}
	s.done = true

	return &OrderRequest{
		Side:   Short,
		Units:  types.Units(1000),
		Stop:   types.Price(101000),
		Reason: "enter short with stop",
	}
}

func TestCandleEngineRun_StrategyEntersOnlyOnce(t *testing.T) {
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100100,
					Low:   99900,
					Close: 100000,
					Ticks: 10,
				},
			},
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  100200,
					Low:   99950,
					Close: 100100,
					Ticks: 11,
				},
			},
			{
				ts: types.FromTime(time.Date(2026, time.January, 1, 2, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100100,
					High:  100300,
					Low:   100000,
					Close: 100200,
					Ticks: 12,
				},
			},
		},
	}

	engine := NewCandleEngine(
		"EURUSD",
		types.H1,
		types.PriceScale,
		types.Money(10000),
		"USD",
	)

	strat := &countingEntryStrategy{}
	err := engine.Run(feed, strat)
	require.NoError(t, err)

	require.Equal(t, 1, strat.entries)
	require.True(t, engine.Pos.Open)
	require.Len(t, engine.Trades, 0)
	require.Equal(t, types.Price(100000), engine.Pos.EntryPrice)
}

type countingEntryStrategy struct {
	entries int
	done    bool
}

func (s *countingEntryStrategy) Name() string {
	return "counting-entry"
}

func (s *countingEntryStrategy) Reset() {
	s.entries = 0
	s.done = false
}

func (s *countingEntryStrategy) OnBar(ctx *CandleContext, c market.Candle) *OrderRequest {
	if s.done || ctx.Pos.Open {
		return nil
	}
	s.done = true
	s.entries++

	return &OrderRequest{
		Side:   Long,
		Units:  types.Units(1000),
		Reason: "single entry",
	}
}

type fakeSource struct {
	it  CandleFeed
	err error
}

func (s *fakeSource) Candles(ctx context.Context, req data.CandleRequest) (data.CandleIterator, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.it.(data.CandleIterator), nil
}

func TestRunCandles_Smoke(t *testing.T) {
	src := &fakeSource{
		it: &fakeFeed{
			bars: []fakeBar{
				{
					ts: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
					c: market.Candle{
						Open:  100000,
						High:  100100,
						Low:   99900,
						Close: 100000,
						Ticks: 10,
					},
				},
				{
					ts: types.FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
					c: market.Candle{
						Open:  100000,
						High:  100200,
						Low:   99950,
						Close: 100100,
						Ticks: 11,
					},
				},
			},
		},
	}

	req := CandleRunRequest{
		DataRequest: data.CandleRequest{
			Instrument: "EURUSD",
			Timeframe:  types.H1,
			Range: types.TimeRange{
				Start: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
				End:   types.FromTime(time.Date(2026, time.January, 1, 2, 0, 0, 0, time.UTC)),
			},
			Strict: true,
		},
		StartingBalance: types.Money(10000),
		AccountCCY:      "USD",
		Scale:           types.PriceScale,
	}

	engine, err := RunCandles(context.Background(), src, req, &BuyFirstBarStrategy{})
	require.NoError(t, err)
	require.NotNil(t, engine)

	require.True(t, engine.Pos.Open)
	require.Equal(t, Long, engine.Pos.Side)
	require.Equal(t, types.Units(1000), engine.Pos.Units)
}
