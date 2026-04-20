package trader

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testAccount() *Account {
	acct := NewAccount("test", MoneyFromFloat(1000.0))
	acct.RiskPct = RateFromFloat(0.005)
	return acct
}

type fakeFeed struct {
	bars []fakeBar
	idx  int
	err  error
}

type fakeBar struct {
	ts Timestamp
	c  Candle
}

func (f *fakeFeed) Next() bool {
	if f.idx >= len(f.bars) {
		return false
	}
	f.idx++
	return true
}

func (f *fakeFeed) Candle() Candle { return f.bars[f.idx-1].c }
func (f *fakeFeed) CandleTime() candleTime {
	return candleTime{Candle: f.Candle(), Timestamp: f.Timestamp()}
}
func (f *fakeFeed) NextCandle() (Candle, bool) {
	if f.Next() {
		return f.Candle(), true
	}
	return Candle{}, false
}
func (f *fakeFeed) Timestamp() Timestamp { return f.bars[f.idx-1].ts }
func (f *fakeFeed) Err() error           { return f.err }
func (f *fakeFeed) Close() error         { return nil }

func openReq(instrument string, side Side, units Units, stop, take Price) *OpenRequest {
	th := NewTradeHistory(instrument)
	th.Side = side
	th.Units = units
	th.Stop = stop
	th.Take = take
	return &OpenRequest{Request: Request{TradeCommon: th.TradeCommon}}
}

type fixedStrategy struct {
	name string
	make func(*CandleContext, Candle) *OpenRequest
}

func (s *fixedStrategy) Name() string { return s.name }
func (s *fixedStrategy) Reset()       {}
func (s *fixedStrategy) OnBar(ctx *CandleContext, c Candle) *OpenRequest {
	if s.make == nil {
		return nil
	}
	return s.make(ctx, c)
}

func TestCandleEngineRun_BuyFirstBarStrategy(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{{
		ts: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 101000, Low: 99000, Close: 100500, Ticks: 10},
	}, {
		ts: FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100500, High: 101500, Low: 100000, Close: 101000, Ticks: 12},
	}}}

	engine := NewCandleEngine("EURUSD", H1, testAccount())
	require.NoError(t, engine.Run(feed, &BuyFirstBarStrategy{}))
	require.Nil(t, engine.Pos)
	require.Len(t, engine.Account.Trades, 1)
	require.Equal(t, Price(101000), engine.Account.Trades[0].FillPrice)
}

func TestCandleEngineRun_TakeProfitClosesTrade(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{{
		ts: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100500, Low: 99500, Close: 100000, Ticks: 10},
	}, {
		ts: FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 101500, Low: 99900, Close: 101000, Ticks: 12},
	}}}
	strat := &fixedStrategy{name: "take", make: func(ctx *CandleContext, c Candle) *OpenRequest {
		if ctx.Pos != nil {
			return nil
		}
		return openReq(ctx.Instrument, Long, 1000, 0, 101000)
	}}
	engine := NewCandleEngine("EURUSD", H1, testAccount())
	require.NoError(t, engine.Run(feed, strat))
	require.Len(t, engine.Account.Trades, 1)
	require.Equal(t, Long, engine.Account.Trades[0].Side)
	require.Equal(t, Price(101000), engine.Account.Trades[0].FillPrice)
}

func TestCandleEngineRun_StopLossClosesTrade(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{{
		ts: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100500, Low: 99500, Close: 100000, Ticks: 10},
	}, {
		ts: FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100100, Low: 98900, Close: 99000, Ticks: 12},
	}}}
	strat := &fixedStrategy{name: "stop", make: func(ctx *CandleContext, c Candle) *OpenRequest {
		if ctx.Pos != nil {
			return nil
		}
		return openReq(ctx.Instrument, Long, 1000, 99000, 0)
	}}
	engine := NewCandleEngine("EURUSD", H1, testAccount())
	require.NoError(t, engine.Run(feed, strat))
	require.Len(t, engine.Account.Trades, 1)
	require.Equal(t, Price(99000), engine.Account.Trades[0].FillPrice)
}

func TestCandleEngineRun_SameBarStopAndTake_UsesStopFirst(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{{
		ts: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100100, Low: 99900, Close: 100000, Ticks: 10},
	}, {
		ts: FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 101500, Low: 98900, Close: 100500, Ticks: 12},
	}}}
	strat := &fixedStrategy{name: "stop-take", make: func(ctx *CandleContext, c Candle) *OpenRequest {
		if ctx.Pos != nil {
			return nil
		}
		return openReq(ctx.Instrument, Long, 1000, 99000, 101000)
	}}
	engine := NewCandleEngine("EURUSD", H1, testAccount())
	require.NoError(t, engine.Run(feed, strat))
	require.Len(t, engine.Account.Trades, 1)
	require.Equal(t, Price(99000), engine.Account.Trades[0].FillPrice)
}

func TestCandleEngineRun_GapBarsReported(t *testing.T) {
	var gaps []int
	strat := &fixedStrategy{name: "gap", make: func(ctx *CandleContext, c Candle) *OpenRequest {
		gaps = append(gaps, ctx.GapBars)
		return nil
	}}
	feed := &fakeFeed{bars: []fakeBar{{
		ts: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100100, Low: 99900, Close: 100000, Ticks: 10},
	}, {
		ts: FromTime(time.Date(2026, time.January, 1, 3, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100200, Low: 99800, Close: 100100, Ticks: 12},
	}}}
	engine := NewCandleEngine("EURUSD", H1, testAccount())
	require.NoError(t, engine.Run(feed, strat))
	require.Equal(t, []int{0, 2}, gaps)
}

func TestCandleEngineRun_ShortTakeProfitClosesTrade(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{{
		ts: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100500, Low: 99500, Close: 100000, Ticks: 10},
	}, {
		ts: FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100100, Low: 98900, Close: 99000, Ticks: 12},
	}}}
	strat := &fixedStrategy{name: "short-take", make: func(ctx *CandleContext, c Candle) *OpenRequest {
		if ctx.Pos != nil {
			return nil
		}
		return openReq(ctx.Instrument, Short, 1000, 0, 99000)
	}}
	engine := NewCandleEngine("EURUSD", H1, testAccount())
	require.NoError(t, engine.Run(feed, strat))
	require.Len(t, engine.Account.Trades, 1)
	require.Equal(t, Short, engine.Account.Trades[0].Side)
	require.Equal(t, Price(99000), engine.Account.Trades[0].FillPrice)
}

func TestCandleEngineRun_ShortStopLossClosesTrade(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{{
		ts: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100500, Low: 99500, Close: 100000, Ticks: 10},
	}, {
		ts: FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 101100, Low: 99900, Close: 101000, Ticks: 12},
	}}}
	strat := &fixedStrategy{name: "short-stop", make: func(ctx *CandleContext, c Candle) *OpenRequest {
		if ctx.Pos != nil {
			return nil
		}
		return openReq(ctx.Instrument, Short, 1000, 101000, 0)
	}}
	engine := NewCandleEngine("EURUSD", H1, testAccount())
	require.NoError(t, engine.Run(feed, strat))
	require.Len(t, engine.Account.Trades, 1)
	require.Equal(t, Price(101000), engine.Account.Trades[0].FillPrice)
}

type countingEntryStrategy struct{ entries int }

func (s *countingEntryStrategy) Name() string { return "counting-entry" }
func (s *countingEntryStrategy) Reset()       { s.entries = 0 }
func (s *countingEntryStrategy) OnBar(ctx *CandleContext, c Candle) *OpenRequest {
	if s.entries > 0 || ctx.Pos != nil {
		return nil
	}
	s.entries++
	return openReq(ctx.Instrument, Long, 1000, 0, 0)
}

func TestCandleEngineRun_StrategyEntersOnlyOnce(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{{
		ts: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100100, Low: 99900, Close: 100000, Ticks: 10},
	}, {
		ts: FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100200, Low: 99950, Close: 100100, Ticks: 11},
	}, {
		ts: FromTime(time.Date(2026, time.January, 1, 2, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100100, High: 100300, Low: 100000, Close: 100200, Ticks: 12},
	}}}
	strat := &countingEntryStrategy{}
	engine := NewCandleEngine("EURUSD", H1, testAccount())
	require.NoError(t, engine.Run(feed, strat))
	require.Equal(t, 1, strat.entries)
	require.Len(t, engine.Account.Trades, 1)
}

type fakeSource struct {
	it  candleIterator
	err error
}

func (s *fakeSource) Candles(context.Context, CandleRequest) (candleIterator, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.it, nil
}

func TestRunCandles_Smoke(t *testing.T) {
	src := &fakeSource{it: &fakeFeed{bars: []fakeBar{{
		ts: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100100, Low: 99900, Close: 100000, Ticks: 10},
	}, {
		ts: FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
		c:  Candle{Open: 100000, High: 100200, Low: 99950, Close: 100100, Ticks: 11},
	}}}}
	req := CandleRunRequest{DataRequest: CandleRequest{Instrument: "EURUSD", Timeframe: H1, Range: TimeRange{Start: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)), End: FromTime(time.Date(2026, time.January, 1, 2, 0, 0, 0, time.UTC))}, Strict: true}, StartingBalance: Money(10000), AccountCCY: "USD", Scale: PriceScale}
	engine, err := RunCandles(context.Background(), src, req, &BuyFirstBarStrategy{}, testAccount())
	require.NoError(t, err)
	require.NotNil(t, engine)
	require.Len(t, engine.Account.Trades, 1)
	require.Nil(t, engine.Pos)
}
