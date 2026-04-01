package backtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── PipScaled ────────────────────────────────────────────────────────────────

func TestPipScaled(t *testing.T) {
	// EUR_USD pipLocation=-4, scale=PriceScale(100_000) => pip = 100_000/10_000 = 10
	got := PipScaled(-4)
	assert.Equal(t, types.Price(10), got)

	// pip location -1 => 100_000/10 = 10000
	got = PipScaled(-1)
	assert.Equal(t, types.Price(10000), got)

	// pip location 0 => 100_000/1 = 100_000
	got = PipScaled(0)
	assert.Equal(t, types.Price(100000), got)
}

// ─── BuyFirstBarStrategy.Name ────────────────────────────────────────────────

func TestBuyFirstBarStrategy_Name(t *testing.T) {
	s := &BuyFirstBarStrategy{}
	assert.Equal(t, "buy-first-bar", s.Name())
}

// ─── CandleEngine.Run nil guard ──────────────────────────────────────────────

func TestCandleEngineRun_NilFeed(t *testing.T) {
	e := NewCandleEngine("EURUSD", types.H1, testAccount)
	err := e.Run(nil, &BuyFirstBarStrategy{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil feed")
}

func TestCandleEngineRun_NilStrategy(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{}}
	e := NewCandleEngine("EURUSD", types.H1, testAccount)
	err := e.Run(feed, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil strategy")
}

// ─── CandleEngine.Run end-of-data close ──────────────────────────────────────

func TestCandleEngineRun_EmptyFeed_NoTrades(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{}}
	e := NewCandleEngine("EURUSD", types.H1, testAccount)
	err := e.Run(feed, &BuyFirstBarStrategy{})
	require.NoError(t, err)
	assert.Len(t, e.Account.Trades, 0)
}

func TestCandleEngineRun_ShortSameBarStopAndTake(t *testing.T) {
	// Short position: both stop (High) and take (Low) hit on same bar → stop-first
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: types.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
				c:  market.Candle{Open: 100000, High: 100200, Low: 99800, Close: 100000, Ticks: 10},
			},
			{
				ts: types.FromTime(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)),
				c: market.Candle{
					Open:  100000,
					High:  101500, // hits short stop
					Low:   98900,  // hits short take
					Close: 100000,
					Ticks: 10,
				},
			},
		},
	}

	e := NewCandleEngine("EURUSD", types.H1, testAccount)
	err := e.Run(feed, &shortStopAndTakeStrat{})
	require.NoError(t, err)
	require.Len(t, e.Account.Trades, 1)
	assert.Equal(t, "STOP&TAKE same bar (stop-first)", e.Account.Trades.Get(0).Reason)
}

type shortStopAndTakeStrat struct{ done bool }

func (s *shortStopAndTakeStrat) Name() string { return "short-stop-take" }
func (s *shortStopAndTakeStrat) Reset()       { s.done = false }
func (s *shortStopAndTakeStrat) OnBar(ctx *CandleContext, c market.Candle) *portfolio.OpenRequest {
	if s.done || ctx.Pos != nil {
		return nil
	}
	s.done = true
	return &portfolio.OpenRequest{
		Side:  types.Short,
		Units: types.Units(1000),
		Stop:  types.Price(101000),
		Take:  types.Price(99000),
	}
}

// ─── closePosition with unknown instrument ────────────────────────────────────

func TestCandleEngineRun_UnknownInstrument_NoPanic(t *testing.T) {
	// Exercise the branch where instrument is not in market.Instruments
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: types.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
				c:  market.Candle{Open: 100000, High: 100100, Low: 99900, Close: 100000, Ticks: 5},
			},
			{
				ts: types.FromTime(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)),
				c:  market.Candle{Open: 100000, High: 100200, Low: 99900, Close: 100100, Ticks: 5},
			},
		},
	}
	e := NewCandleEngine("XXXXXX", types.H1, testAccount)
	err := e.Run(feed, &BuyFirstBarStrategy{})
	require.NoError(t, err)
	require.Equal(t, e.Account.Trades.Len(), 1)
}

// ─── RunCandles error paths ───────────────────────────────────────────────────

func TestRunCandles_SourceError(t *testing.T) {
	src := &fakeSource{err: errors.New("source failed")}
	req := CandleRunRequest{
		Scale: types.PriceScale,
	}
	_, err := RunCandles(context.Background(), src, req, &BuyFirstBarStrategy{}, testAccount)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source failed")
}

func TestRunCandles_RunError(t *testing.T) {
	// Use a feed that returns an error from Err() after iteration ends.
	src := &fakeSource{it: &errFeed{err: errors.New("feed error")}}
	req := CandleRunRequest{
		Scale: types.PriceScale,
	}
	_, err := RunCandles(context.Background(), src, req, &BuyFirstBarStrategy{}, testAccount)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "feed error")
}

// errFeed is a CandleFeed/CandleIterator that immediately stops iteration
// but returns an error from Err().
type errFeed struct {
	err error
}

func (f *errFeed) Next() bool                        { return false }
func (f *errFeed) Candle() market.Candle             { return market.Candle{} }
func (f *errFeed) NextCandle() (market.Candle, bool) { return market.Candle{}, false }
func (f *errFeed) Timestamp() types.Timestamp        { return 1 }
func (f *errFeed) Err() error                        { return f.err }
func (f *errFeed) Close() error                      { return nil }

// ─── backtest_db stubs ────────────────────────────────────────────────────────

func TestBacktestDB_Stubs(t *testing.T) {
	ctx := context.Background()

	err := RecordBacktest(ctx, BacktestRun{})
	assert.NoError(t, err)

	_, err = GetBacktestRun(ctx, "run-id")
	assert.NoError(t, err)

	_, err = ListTradesByRunID(ctx, "run-id")
	assert.NoError(t, err)

	_, err = ListEquityByRunID(ctx, "run-id")
	assert.NoError(t, err)

	_, err = ExportBacktestOrg(ctx, "run-id")
	assert.NoError(t, err)
}
