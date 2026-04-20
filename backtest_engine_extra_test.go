package trader

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── PipScaled ────────────────────────────────────────────────────────────────

func TestPipScaled(t *testing.T) {
	// EUR_USD pipLocation=-4, scale=PriceScale(100_000) => pip = 100_000/10_000 = 10
	got := PipScaled(-4)
	assert.Equal(t, Price(10), got)

	// pip location -1 => 100_000/10 = 10000
	got = PipScaled(-1)
	assert.Equal(t, Price(10000), got)

	// pip location 0 => 100_000/1 = 100_000
	got = PipScaled(0)
	assert.Equal(t, Price(100000), got)
}

// ─── BuyFirstBarStrategy.Name ────────────────────────────────────────────────

func TestBuyFirstBarStrategy_Name(t *testing.T) {
	s := &BuyFirstBarStrategy{}
	assert.Equal(t, "buy-first-bar", s.Name())
}

// ─── CandleEngine.Run nil guard ──────────────────────────────────────────────

func TestCandleEngineRun_NilFeed(t *testing.T) {
	e := NewCandleEngine("EURUSD", H1, testAccount())
	err := e.Run(nil, &BuyFirstBarStrategy{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil feed")
}

func TestCandleEngineRun_NilStrategy(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{}}
	e := NewCandleEngine("EURUSD", H1, testAccount())
	err := e.Run(feed, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil strategy")
}

// ─── CandleEngine.Run end-of-data close ──────────────────────────────────────

func TestCandleEngineRun_EmptyFeed_NoTrades(t *testing.T) {
	feed := &fakeFeed{bars: []fakeBar{}}
	e := NewCandleEngine("EURUSD", H1, testAccount())
	err := e.Run(feed, &BuyFirstBarStrategy{})
	require.NoError(t, err)
	assert.Len(t, e.Account.Trades, 0)
}

func TestCandleEngineRun_ShortSameBarStopAndTake(t *testing.T) {
	// Short position: both stop (High) and take (Low) hit on same bar → stop-first
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
				c:  Candle{Open: 100000, High: 100200, Low: 99800, Close: 100000, Ticks: 10},
			},
			{
				ts: FromTime(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)),
				c: Candle{
					Open:  100000,
					High:  101500, // hits short stop
					Low:   98900,  // hits short take
					Close: 100000,
					Ticks: 10,
				},
			},
		},
	}

	e := NewCandleEngine("EURUSD", H1, testAccount())
	err := e.Run(feed, &shortStopAndTakeStrat{})
	require.NoError(t, err)
	require.Len(t, e.Account.Trades, 1)
	assert.Equal(t, Price(101000), e.Account.Trades[0].FillPrice)
}

type shortStopAndTakeStrat struct{ done bool }

func (s *shortStopAndTakeStrat) Name() string { return "short-stop-take" }
func (s *shortStopAndTakeStrat) Reset()       { s.done = false }
func (s *shortStopAndTakeStrat) OnBar(ctx *CandleContext, c Candle) *OpenRequest {
	if s.done || ctx.Pos != nil {
		return nil
	}
	s.done = true
	return openReq(ctx.Instrument, Short, 1000, 101000, 99000)
}

// ─── closePosition with unknown instrument ────────────────────────────────────

func TestCandleEngineRun_UnknownInstrument_NoPanic(t *testing.T) {
	// Exercise the branch where instrument is not in Instruments
	feed := &fakeFeed{
		bars: []fakeBar{
			{
				ts: FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
				c:  Candle{Open: 100000, High: 100100, Low: 99900, Close: 100000, Ticks: 5},
			},
			{
				ts: FromTime(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)),
				c:  Candle{Open: 100000, High: 100200, Low: 99900, Close: 100100, Ticks: 5},
			},
		},
	}
	e := NewCandleEngine("XXXXXX", H1, testAccount())
	err := e.Run(feed, &BuyFirstBarStrategy{})
	require.Error(t, err)
}

// ─── RunCandles error paths ───────────────────────────────────────────────────

func TestRunCandles_SourceError(t *testing.T) {
	src := &fakeSource{err: errors.New("source failed")}
	req := CandleRunRequest{
		Scale: PriceScale,
	}
	_, err := RunCandles(context.Background(), src, req, &BuyFirstBarStrategy{}, testAccount())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source failed")
}

func TestRunCandles_RunError(t *testing.T) {
	// Use a feed that returns an error from Err() after iteration ends.
	src := &fakeSource{it: &errFeed{err: errors.New("feed error")}}
	req := CandleRunRequest{
		Scale: PriceScale,
	}
	_, err := RunCandles(context.Background(), src, req, &BuyFirstBarStrategy{}, testAccount())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "feed error")
}

// errFeed is a CandleFeed/CandleIterator that immediately stops iteration
// but returns an error from Err().
type errFeed struct {
	err error
}

func (f *errFeed) Next() bool     { return false }
func (f *errFeed) Candle() Candle { return Candle{} }
func (f *errFeed) CandleTime() CandleTime {
	return CandleTime{Candle: f.Candle(), Timestamp: f.Timestamp()}
}
func (f *errFeed) NextCandle() (Candle, bool) { return Candle{}, false }
func (f *errFeed) Timestamp() Timestamp       { return 1 }
func (f *errFeed) Err() error                 { return f.err }
func (f *errFeed) Close() error               { return nil }
