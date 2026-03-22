package backtest

import (
	"testing"
	"time"

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

	require.True(t, engine.Pos.Open)
	require.Equal(t, Long, engine.Pos.Side)
	require.Equal(t, types.Units(1000), engine.Pos.Units)
	require.Equal(t, types.Price(100500), engine.Pos.EntryPrice)
	require.Len(t, engine.Trades, 0)
}
