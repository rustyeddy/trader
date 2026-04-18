package trader

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testIterator struct {
	nextFn       func() bool
	candleTimeFn func() CandleTime
	err          error
	closeErr     error
	closed       bool
}

func (it *testIterator) Next() bool {
	if it.nextFn == nil {
		return false
	}
	return it.nextFn()
}

func (it *testIterator) Candle() Candle {
	return it.CandleTime().Candle
}

func (it *testIterator) CandleTime() CandleTime {
	if it.candleTimeFn == nil {
		return CandleTime{}
	}
	return it.candleTimeFn()
}

func (it *testIterator) NextCandle() (Candle, bool) {
	if !it.Next() {
		return Candle{}, false
	}
	return it.Candle(), true
}

func (it *testIterator) Timestamp() Timestamp {
	return it.CandleTime().Timestamp
}

func (it *testIterator) Err() error {
	return it.err
}

func (it *testIterator) Close() error {
	it.closed = true
	return it.closeErr
}

type testStrategy struct {
	name     string
	updateFn func(context.Context, *CandleTime, *Positions) *StrategyPlan
}

func (s testStrategy) Name() string {
	if s.name == "" {
		return "test"
	}
	return s.name
}

func (s testStrategy) Update(ctx context.Context, candle *CandleTime, positions *Positions) *StrategyPlan {
	if s.updateFn == nil {
		return &DefaultStrategyPlan
	}
	return s.updateFn(ctx, candle, positions)
}

func newTestTrader() *Trader {
	am := NewAccountManager()
	return &Trader{
		Account: am.CreateAccount("test", 1000),
		Broker: &Broker{
			ID: NewULID(),
			OpenOrders: OpenOrders{
				Orders: make(map[string]*Order),
			},
		},
	}
}

func TestTrader(t *testing.T) {
	trader := newTestTrader()

	bars := []CandleTime{
		{
			Candle:    Candle{Open: 1100000, High: 1101000, Low: 1099000, Close: 1100500, Ticks: 10},
			Timestamp: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		},
		{
			Candle:    Candle{Open: 1100500, High: 1102000, Low: 1100000, Close: 1101500, Ticks: 11},
			Timestamp: FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
		},
		{
			Candle:    Candle{Open: 1101500, High: 1103000, Low: 1101000, Close: 1102500, Ticks: 12},
			Timestamp: FromTime(time.Date(2026, time.January, 1, 2, 0, 0, 0, time.UTC)),
		},
	}

	idx := -1
	iter := &testIterator{
		nextFn: func() bool {
			idx++
			return idx < len(bars)
		},
		candleTimeFn: func() CandleTime {
			if idx < 0 || idx >= len(bars) {
				return CandleTime{}
			}
			return bars[idx]
		},
	}

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "noop", TimeFrame: M1}
	err := trader.backTestWithIterator(context.Background(), cfg, testStrategy{name: "noop"}, iter)
	assert.NoError(t, err)
	assert.True(t, iter.closed)
}

func TestBackTestRejectsUnknownStrategy(t *testing.T) {
	trader := newTestTrader()
	trader.DataManager = &DataManager{}

	cfg := &ConfigBackTest{
		Instrument: "EURUSD",
		Strategy:   "does-not-exist",
		TimeFrame:  M1,
		Start:      time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC),
		Account:    "test",
	}

	err := trader.BackTest(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported strategy")
}

func TestBackTestWithIteratorReturnsContextCancellation(t *testing.T) {
	trader := newTestTrader()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	iter := &testIterator{
		nextFn: func() bool {
			cancel()
			return true
		},
		candleTimeFn: func() CandleTime {
			return CandleTime{
				Candle:    Candle{Close: Price(1100000)},
				Timestamp: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
			}
		},
	}

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "noop", TimeFrame: M1}
	err := trader.backTestWithIterator(ctx, cfg, testStrategy{name: "noop"}, iter)
	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, iter.closed)
}

func TestBackTestWithIteratorReturnsIteratorError(t *testing.T) {
	trader := newTestTrader()
	iterErr := errors.New("iterator failed")
	iter := &testIterator{
		nextFn: func() bool { return false },
		err:    iterErr,
	}

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "noop", TimeFrame: M1}
	err := trader.backTestWithIterator(context.Background(), cfg, testStrategy{name: "noop"}, iter)
	require.ErrorIs(t, err, iterErr)
	assert.True(t, iter.closed)
}

func TestBackTestWithIteratorReturnsBrokerEventError(t *testing.T) {
	trader := newTestTrader()
	iter := &testIterator{
		nextFn: func() bool { return false },
	}

	position := &Position{
		TradeCommon: NewTradeHistory("EURUSD").TradeCommon,
		FillPrice:   Price(1100000),
		State:       PositionOpen,
	}

	strategy := testStrategy{
		name: "bad-close",
		updateFn: func(ctx context.Context, candle *CandleTime, positions *Positions) *StrategyPlan {
			return &StrategyPlan{
				Closes: []*CloseRequest{{
					Request: Request{
						TradeCommon: position.TradeCommon,
						RequestType: RequestClose,
						Price:       Price(1090000),
						Timestamp:   FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
					},
				}},
			}
		},
	}

	iter.nextFn = func() bool {
		if iter.closed {
			return false
		}
		iter.nextFn = func() bool { return false }
		return true
	}
	iter.candleTimeFn = func() CandleTime {
		return CandleTime{
			Candle:    Candle{Close: Price(1100000)},
			Timestamp: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		}
	}

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "fake", TimeFrame: M1}
	err := trader.backTestWithIterator(context.Background(), cfg, strategy, iter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing position")
}
