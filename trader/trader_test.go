package trader

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/data"
	tlog "github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/strategies"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testIterator struct {
	nextFn       func() bool
	candleTimeFn func() types.CandleTime
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

func (it *testIterator) Candle() types.Candle {
	return it.CandleTime().Candle
}

func (it *testIterator) CandleTime() types.CandleTime {
	if it.candleTimeFn == nil {
		return types.CandleTime{}
	}
	return it.candleTimeFn()
}

func (it *testIterator) NextCandle() (types.Candle, bool) {
	if !it.Next() {
		return types.Candle{}, false
	}
	return it.Candle(), true
}

func (it *testIterator) Timestamp() types.Timestamp {
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
	updateFn func(context.Context, *types.CandleTime, *types.Positions) *strategies.Plan
}

func (s testStrategy) Name() string {
	if s.name == "" {
		return "test"
	}
	return s.name
}

func (s testStrategy) Update(ctx context.Context, candle *types.CandleTime, positions *types.Positions) *strategies.Plan {
	if s.updateFn == nil {
		return &strategies.DefaultPlan
	}
	return s.updateFn(ctx, candle, positions)
}

func newTestTrader() *Trader {
	am := account.NewAccountManager()
	return &Trader{
		Account: am.CreateAccount("test", 1000),
		Broker: &broker.Broker{
			ID: types.NewULID(),
			OpenOrders: broker.OpenOrders{
				Orders: make(map[string]*types.Order),
			},
		},
	}
}

func TestTrader(t *testing.T) {
	err := tlog.Setup(tlog.Config{Level: "debug", Format: "text"})
	assert.NoError(t, err)

	instrument := "EURUSD"

	start := time.Date(2022, time.Month(time.January), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, time.Month(time.January), 0, 0, 0, 0, 0, time.UTC)

	cfg := &ConfigBackTest{
		Instrument: instrument,
		Strategy:   "fake",
		Start:      start,
		End:        end,
		TimeFrame:  types.M1,
		Account:    "test",
	}

	am := account.NewAccountManager()
	trader := Trader{
		Account:     am.CreateAccount("test", 1000),
		DataManager: data.NewDataManager([]string{"EURUSD"}, start, end),
		Broker: &broker.Broker{
			ID: types.NewULID(),
			OpenOrders: broker.OpenOrders{
				Orders: make(map[string]*types.Order),
			},
		},
	}

	ctx := context.TODO()
	err = trader.BackTest(ctx, cfg)
	assert.NoError(t, err)
}

func TestBackTestRejectsUnknownStrategy(t *testing.T) {
	trader := newTestTrader()
	trader.DataManager = &data.DataManager{}

	cfg := &ConfigBackTest{
		Instrument: "EURUSD",
		Strategy:   "does-not-exist",
		TimeFrame:  types.M1,
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
		candleTimeFn: func() types.CandleTime {
			return types.CandleTime{
				Candle:    types.Candle{Close: types.Price(1100000)},
				Timestamp: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
			}
		},
	}

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "noop", TimeFrame: types.M1}
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

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "noop", TimeFrame: types.M1}
	err := trader.backTestWithIterator(context.Background(), cfg, testStrategy{name: "noop"}, iter)
	require.ErrorIs(t, err, iterErr)
	assert.True(t, iter.closed)
}

func TestBackTestWithIteratorReturnsBrokerEventError(t *testing.T) {
	trader := newTestTrader()
	iter := &testIterator{
		nextFn: func() bool { return false },
	}

	position := &types.Position{
		TradeCommon: types.NewTradeHistory("EURUSD").TradeCommon,
		FillPrice:   types.Price(1100000),
		State:       types.PositionOpen,
	}

	strategy := testStrategy{
		name: "bad-close",
		updateFn: func(ctx context.Context, candle *types.CandleTime, positions *types.Positions) *strategies.Plan {
			return &strategies.Plan{
				Closes: []*types.CloseRequest{{
					Request: types.Request{
						TradeCommon: position.TradeCommon,
						RequestType: types.RequestClose,
						Price:       types.Price(1090000),
						Timestamp:   types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
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
	iter.candleTimeFn = func() types.CandleTime {
		return types.CandleTime{
			Candle:    types.Candle{Close: types.Price(1100000)},
			Timestamp: types.FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		}
	}

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "fake", TimeFrame: types.M1}
	err := trader.backTestWithIterator(context.Background(), cfg, strategy, iter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing position")
}
