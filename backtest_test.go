package trader

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/marketdata"
	mdservice "github.com/rustyeddy/trader/marketdata/service"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptStrategy returns a fixed order per bar index (nil means no order) and
// records the bars it was shown.
type scriptStrategy struct {
	orders []*order
	seen   []marketdata.Bar
}

func (s *scriptStrategy) name() string { return "script" }

func (s *scriptStrategy) init() error { return nil }

func (s *scriptStrategy) onBar(_ context.Context, b marketdata.Bar) *order {
	i := len(s.seen)
	s.seen = append(s.seen, b)
	if i < len(s.orders) && s.orders[i] != nil {
		s.orders[i].Time = b.Time // a strategy stamps its orders with the bar
		return s.orders[i]
	}
	return nil
}

func (s *scriptStrategy) onBars(ctx context.Context, bars []marketdata.Bar) ([]*order, error) {
	return eachBar(ctx, bars, s.onBar)
}

// openOrder is a valid open-long order for EURUSD.
func openOrder() *order {
	return &order{Time: testStart, instrument: "EURUSD", direction: long, action: open, orderType: marketOrder, price: eurusd1_10, stopLoss: px(108900)}
}

// eurBars is closes for EURUSD, the instrument the test account can size.
func eurBars(vals ...float64) []marketdata.Bar {
	bars := closes(vals...)
	for i := range bars {
		bars[i].Instrument = "EURUSD"
	}
	return bars
}

// runTrader runs the bars through a trader built on the strategy.
func runTrader(ctx context.Context, tr *trader, strat strategy, bars iter.Seq2[marketdata.Bar, error]) error {
	tr.strategy = strat
	return (&backtest{}).run(ctx, bars, tr)
}

// opens returns n distinct valid open orders, one per bar.
func opens(n int) []*order {
	orders := make([]*order, n)
	for i := range orders {
		orders[i] = openOrder()
	}
	return orders
}

func seq(bars []marketdata.Bar, errAt int, err error) iter.Seq2[marketdata.Bar, error] {
	return func(yield func(marketdata.Bar, error) bool) {
		for i, b := range bars {
			if i == errAt {
				yield(marketdata.Bar{}, err)
				return
			}
			if !yield(b, nil) {
				return
			}
		}
	}
}

func TestBacktestRun_AcceptedRequests(t *testing.T) {
	captureLogs(t)
	bars := eurBars(1.1, 1.2, 1.3)
	o1 := openOrder()
	o3 := &order{Time: testStart, instrument: "EURUSD", action: close, orderType: marketOrder}
	strat := &scriptStrategy{orders: []*order{o1, nil, o3}}

	err := runTrader(context.Background(), newTestTrader(), strat, seq(bars, -1, nil))

	require.NoError(t, err)
	assert.Equal(t, bars, strat.seen, "every bar reaches the strategy")
	assert.Equal(t, stateFilled, o1.state, "filled on the bar after it was made")
	assert.Equal(t, stateSubmitted, o3.state, "made on the last bar, so still waiting")
	assert.Equal(t, "a", o1.accountID)
}

func TestBacktestRun_NoOrderNothingSubmitted(t *testing.T) {
	strat := &scriptStrategy{} // returns nil on every bar
	tr := newTestTrader()

	require.NoError(t, runTrader(context.Background(), tr, strat, seq(eurBars(1.1, 1.2, 1.3), -1, nil)))
	assert.Len(t, strat.seen, 3)
	assert.Zero(t, tr.broker.seq, "nothing reached the broker")
}

func TestBacktestRun_NoBarsNoCalls(t *testing.T) {
	strat := &scriptStrategy{}

	require.NoError(t, runTrader(context.Background(), newTestTrader(), strat, seq(nil, -1, nil)))
	assert.Empty(t, strat.seen)
}

func TestBacktestRun_RecordsLifecycleInHistory(t *testing.T) {
	captureLogs(t)
	o := openOrder()
	strat := &scriptStrategy{orders: []*order{nil, o}}
	bars := eurBars(1.1, 1.2)

	require.NoError(t, runTrader(context.Background(), newTestTrader(), strat, seq(bars, -1, nil)))

	require.Len(t, o.history, 3)
	for _, e := range o.history {
		assert.Equal(t, bars[1].Time, e.time, "history carries the bar time, not the wall clock")
	}
	assert.Equal(t, []orderState{stateRequested, stateSized, stateSubmitted},
		[]orderState{o.history[0].state, o.history[1].state, o.history[2].state})
}

func TestBacktestRun_FatalHandlerErrorStopsRun(t *testing.T) {
	captureLogs(t)
	strat := &scriptStrategy{orders: opens(4)}
	tr := &trader{broker: newBroker("t", testMarkets()), account: newAccount("ghost", usd(1000), testMarkets())} // the broker has no such account

	err := runTrader(context.Background(), tr, strat, seq(eurBars(1.1, 1.2, 1.3, 1.4), -1, nil))

	require.ErrorIs(t, err, errUnknownAccount)
	assert.Len(t, strat.seen, 1, "no bars processed after the failure")
}

func TestBacktestRun_FatalHandlerErrorHasInstrumentAndTimeContext(t *testing.T) {
	captureLogs(t)
	strat := &scriptStrategy{orders: []*order{nil, openOrder()}}
	tr := &trader{broker: newBroker("t", testMarkets()), account: newAccount("ghost", usd(1000), testMarkets())}

	err := runTrader(context.Background(), tr, strat, seq(eurBars(1.1, 1.2, 1.3), -1, nil))

	require.ErrorIs(t, err, errUnknownAccount, "cause preserved")
	assert.Contains(t, err.Error(), "EURUSD")
	assert.Contains(t, err.Error(), "2010-01-05", "time of the second bar")
}

func TestBacktestRun_FatalErrorIsNotLoggedAsRejection(t *testing.T) {
	logs := captureLogs(t)
	o := openOrder()
	tr := &trader{broker: newBroker("t", testMarkets()), account: newAccount("ghost", usd(1000), testMarkets())}

	err := runTrader(context.Background(), tr, &scriptStrategy{orders: []*order{o}}, seq(eurBars(1.1, 1.2), -1, nil))

	require.Error(t, err)
	assert.NotContains(t, logs.String(), "order rejected")
	assert.Equal(t, stateRequested, o.state, "a fatal error is not recorded as a rejection")
}

func TestBacktestRun_DataErrorStopsRun(t *testing.T) {
	boom := errors.New("bad data")
	strat := &scriptStrategy{}

	err := runTrader(context.Background(), newTestTrader(), strat, seq(eurBars(1.1, 1.2, 1.3), 2, boom))

	require.ErrorIs(t, err, boom)
	assert.Len(t, strat.seen, 2, "bars before the error are processed, none after")
}

func TestBacktestRun_DataErrorHasContext(t *testing.T) {
	boom := errors.New("bad data")

	// error on the first bar: nothing processed yet
	err := runTrader(context.Background(), newTestTrader(), &scriptStrategy{}, seq(eurBars(1.1, 1.2), 0, boom))
	require.ErrorIs(t, err, boom)

	// error after bars were processed: mentions the last good bar
	err = runTrader(context.Background(), newTestTrader(), &scriptStrategy{}, seq(eurBars(1.1, 1.2, 1.3), 2, boom))
	require.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "EURUSD")
	assert.Contains(t, err.Error(), "2010-01-05", "last successfully processed bar")
}

// cancelStrategy cancels the context as it sees its first bar.
type cancelStrategy struct {
	scriptStrategy
	cancel context.CancelFunc
}

func (s *cancelStrategy) onBar(ctx context.Context, b marketdata.Bar) *order {
	s.cancel()
	return s.scriptStrategy.onBar(ctx, b)
}

func (s *cancelStrategy) onBars(ctx context.Context, bars []marketdata.Bar) ([]*order, error) {
	return eachBar(ctx, bars, s.onBar)
}

func TestBacktestRun_ContextCancelled(t *testing.T) {
	md := marketdata.Series{Bars: eurBars(1.1, 1.2, 1.3, 1.4)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	strat := &cancelStrategy{cancel: cancel}

	err := runTrader(ctx, newTestTrader(), strat, seriesBars(ctx, md))

	require.ErrorIs(t, err, context.Canceled)
	assert.Len(t, strat.seen, 1)
}

// captureLogs routes the default slog logger to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestBacktestRun_ExpectedRejectionIsRecordedLoggedAndRunContinues(t *testing.T) {
	logs := captureLogs(t)
	o := openOrder()
	strat := &scriptStrategy{orders: []*order{o}}

	err := runTrader(context.Background(), newTestTraderWithBalance(usd(0)), strat, seq(eurBars(1.1, 1.2, 1.3), -1, nil))

	require.NoError(t, err, "a margin rejection must not stop the run")
	assert.Len(t, strat.seen, 3, "bars after the rejection are still processed")

	assert.Equal(t, stateRejected, o.state)
	require.Len(t, o.history, 2)
	assert.Equal(t, stateRequested, o.history[0].state)
	assert.Equal(t, stateRejected, o.history[1].state)
	assert.Contains(t, o.history[1].reason, "insufficient margin")
	assert.Equal(t, closes(1)[0].Time, o.history[1].time, "stamped with the bar time")

	out := logs.String()
	assert.Contains(t, out, "to=rejected", "the state change is logged too")
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "order rejected")
	assert.Contains(t, out, "insufficient margin available")
	assert.Contains(t, out, "time=2010-01-04")
	assert.Contains(t, out, "instrument=EURUSD")
	assert.Contains(t, out, "action=open")
	assert.Contains(t, out, "direction=long")
}

func TestBacktestRun_OrderTooSmallIsARejectionToo(t *testing.T) {
	captureLogs(t)
	// A balance of 1 buys less than one unit: a rejection, not a failure.
	o := openOrder()

	err := runTrader(context.Background(), newTestTraderWithBalance(usd(1)), &scriptStrategy{orders: []*order{o, nil}}, seq(eurBars(1.1, 1.2), -1, nil))

	require.NoError(t, err)
	assert.Equal(t, stateRejected, o.state)
}

func TestBacktestRun_InvalidOrderStopsRunBeforeSizing(t *testing.T) {
	bad := &order{Time: testStart, instrument: "EURUSD", action: open, orderType: marketOrder, price: eurusd1_10, stopLoss: px(108900)} // open without a direction
	strat := &scriptStrategy{orders: []*order{nil, bad, openOrder()}}
	tr := newTestTrader()

	err := runTrader(context.Background(), tr, strat, seq(eurBars(1.1, 1.2, 1.3, 1.4), -1, nil))

	require.ErrorIs(t, err, errInvalidOrder)
	assert.Contains(t, err.Error(), "EURUSD")
	assert.Contains(t, err.Error(), "2010-01-05", "time of the offending bar")
	assert.Len(t, strat.seen, 2, "no bars processed after the invalid order")
	assert.Empty(t, bad.history, "an invalid order is never recorded as requested")
	assert.Zero(t, tr.broker.seq)
}

func TestBacktestRun_OpenWithoutAStopStopsRunBeforeSizing(t *testing.T) {
	// The strategy, not the account, must set the stop.
	noStop := &order{Time: testStart, instrument: "EURUSD", direction: long, action: open, orderType: marketOrder, price: eurusd1_10}
	strat := &scriptStrategy{orders: []*order{nil, noStop, openOrder()}}

	err := runTrader(context.Background(), newTestTrader(), strat, seq(eurBars(1.1, 1.2, 1.3, 1.4), -1, nil))

	require.ErrorIs(t, err, errInvalidOrder)
	assert.Contains(t, err.Error(), "stop loss")
	assert.Contains(t, err.Error(), "2010-01-05", "time of the offending bar")
	assert.Len(t, strat.seen, 2, "no bars processed after the invalid order")
	assert.Empty(t, noStop.history)
}

// rejectFirstOpen wraps a strategy, records every order it emits with the bar
// index, and starves the account of margin until the bar after its first open,
// so that open is rejected and every later one is accepted.
type rejectFirstOpen struct {
	strategy
	act  *account
	cash money
	bars int
	got  []*order
	at   []int
}

func (s *rejectFirstOpen) onBar(ctx context.Context, b marketdata.Bar) *order {
	if len(s.got) > 0 {
		s.act.balance = s.cash // margin becomes available on the bar after the open
	}
	o := first(s.strategy.onBars(ctx, []marketdata.Bar{b}))
	if o != nil {
		s.got = append(s.got, o)
		s.at = append(s.at, s.bars)
	}
	s.bars++
	return o
}

func (s *rejectFirstOpen) onBars(ctx context.Context, bars []marketdata.Bar) ([]*order, error) {
	return eachBar(ctx, bars, s.onBar)
}

func actions(orders []*order) (as []action) {
	for _, o := range orders {
		as = append(as, o.action)
	}
	return as
}

func newRejectFirstOpen(t *testing.T) (*trader, *rejectFirstOpen) {
	t.Helper()
	tr := newTestTrader()
	// These price series swing far enough to wipe out the account; the tests
	// are about missed entries, not liquidation.
	tr.broker.margin = func(*account) (marginStatus, error) { return marginHealthy, nil }
	s := &rejectFirstOpen{strategy: newTestEmax(t, 2, 4), act: tr.account, cash: tr.account.balance}
	tr.account.balance = usd(0)
	return tr, s
}

// §2 critical regression: a rejected entry must not become a late fill.
func TestMissedEntry_RejectedOpenIsNotRetried(t *testing.T) {
	captureLogs(t)
	tr, s := newRejectFirstOpen(t)

	// Bullish from the first valid bar (3) and bullish for many bars after,
	// while margin "becomes available".
	err := runTrader(context.Background(), tr, s, seq(eurBars(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12), -1, nil))

	require.NoError(t, err)
	require.Equal(t, []action{open}, actions(s.got), "the strategy asked once and never again")
	assert.Equal(t, stateRejected, s.got[0].state)
	assert.Equal(t, []int{3}, s.at, "the one entry opportunity, bar 3")
}

// §2: a rejected entry must not suppress re-entry forever.
func TestMissedEntry_FreshSignalAfterRejectionIsAllowed(t *testing.T) {
	captureLogs(t)
	tr, s := newRejectFirstOpen(t)

	// Rising (open long at 3, rejected), a drop (close at 8, open short at 9),
	// then a rise again (close at 12, open long at 13).
	err := runTrader(context.Background(), tr, s, seq(eurBars(1, 2, 3, 4, 5, 6, 7, 8, 4, 2, 1, 0.5, 6, 9, 12), -1, nil))

	require.NoError(t, err)
	require.Equal(t, []action{open, close, open, close, open}, actions(s.got))
	assert.Equal(t, []int{3, 8, 9, 12, 13}, s.at)
	assert.Equal(t, stateRejected, s.got[0].state, "first opportunity was rejected")
	assert.Equal(t, short, s.got[2].direction)
	assert.Equal(t, stateRejected, s.got[1].state, "the close has no position: its open was rejected")
	assert.Contains(t, s.got[1].history[len(s.got[1].history)-1].reason, "no position")
	assert.Equal(t, stateFilled, s.got[2].state, "the fresh opportunity is accepted")
	// The rise to 6 gaps through the short's protective stop, so the position
	// is already closed when the strategy's own close arrives.
	assert.Equal(t, stateRejected, s.got[3].state, "stopped out before the strategy closed")
	assert.Contains(t, s.got[3].history[len(s.got[3].history)-1].reason, "no position")
	assert.Equal(t, stateFilled, s.got[4].state)
	require.Contains(t, tr.account.positions, "EURUSD")
	assert.Equal(t, long, tr.account.positions["EURUSD"].direction)
}

// seenStrategy records every bar it is shown as instrument@availability, in the
// order it was shown, and places no order.
type seenStrategy struct{ seen []string }

func (s *seenStrategy) name() string { return "seen" }
func (s *seenStrategy) init() error  { return nil }
func (s *seenStrategy) onBar(_ context.Context, b marketdata.Bar) *order {
	s.seen = append(s.seen, b.Instrument+"@"+b.AvailableAt.Format("Mon 15:04"))
	return nil
}

func (s *seenStrategy) onBars(ctx context.Context, bars []marketdata.Bar) ([]*order, error) {
	return eachBar(ctx, bars, s.onBar)
}

// hSeries is an hourly series of the instrument with a bar opening at each given
// hour (hour(h) = pairStart + h), available an hour later.
func hSeries(instrument string, hours ...int) marketdata.Series {
	s := marketdata.Series{Source: "test", Instrument: instrument, Interval: "h1"}
	for _, h := range hours {
		s.Bars = append(s.Bars, hbar(instrument, hour(h), 100000, 100000))
	}
	return s
}

func runSeriesSeen(t *testing.T, series ...marketdata.Series) ([]string, error) {
	t.Helper()
	spy := &seenStrategy{}
	tr, _ := pairTrader(usd(1000), spy)
	tr.broker.markets = withMarket(t, testMarkets(), testInstrument("USDCHF"))
	err := (&backtest{}).runSeries(context.Background(), tr, series...)
	return spy.seen, err
}

func TestBacktestRunSeries_ThreeInstrumentsStaggeredAndSparse(t *testing.T) {
	// EURUSD opens at hours 0, 1, 2; GBPUSD at 0 and 2 (none at 1); USDCHF at 1 and 2.
	got, err := runSeriesSeen(t, hSeries("USDCHF", 1, 2), hSeries("EURUSD", 0, 1, 2), hSeries("GBPUSD", 0, 2))

	require.NoError(t, err)
	assert.Equal(t, []string{
		"EURUSD@" + hour(1).Format("Mon 15:04"), "GBPUSD@" + hour(1).Format("Mon 15:04"), // group 1: USDCHF has no bar yet
		"EURUSD@" + hour(2).Format("Mon 15:04"), "USDCHF@" + hour(2).Format("Mon 15:04"), // group 2: GBPUSD absent, not carried forward
		"EURUSD@" + hour(3).Format("Mon 15:04"), "GBPUSD@" + hour(3).Format("Mon 15:04"), "USDCHF@" + hour(3).Format("Mon 15:04"),
	}, got, "by availability, then instrument name; the argument order does not matter")
}

func TestBacktestRunSeries_AGroupSpansTheUTCDayBoundary(t *testing.T) {
	// Bars opening at 22:00 and 23:00 UTC: the 23:00 bar is available at 00:00 of the next day.
	late := func(instrument string, hours ...int) marketdata.Series {
		s := marketdata.Series{Source: "test", Instrument: instrument, Interval: "h1"}
		for _, h := range hours {
			s.Bars = append(s.Bars, hbar(instrument, time.Date(2010, time.January, 8, h, 0, 0, 0, time.UTC), 100000, 100000))
		}
		return s
	}

	got, err := runSeriesSeen(t, late("EURUSD", 22, 23), late("GBPUSD", 23))

	require.NoError(t, err)
	assert.Equal(t, []string{"EURUSD@Fri 23:00", "EURUSD@Sat 00:00", "GBPUSD@Sat 00:00"}, got)
}

func TestBacktestRunSeries_AnEmptySeriesContributesNothing(t *testing.T) {
	got, err := runSeriesSeen(t, hSeries("EURUSD"), hSeries("GBPUSD", 0))
	require.NoError(t, err)
	assert.Equal(t, []string{"GBPUSD@" + hour(1).Format("Mon 15:04")}, got)

	got, err = runSeriesSeen(t, hSeries("EURUSD"), hSeries("GBPUSD"))
	require.NoError(t, err)
	assert.Empty(t, got, "all empty: no group, no error")

	got, err = runSeriesSeen(t)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// An opening time never authorises delivery: a bar is delivered at its stored
// availability, and the broker refuses a group whose bars opened before an earlier
// group was available, which is what mixing bar lengths would do.
func TestBacktestRunSeries_RefusesABarThatOpenedBeforeAnEarlierGroupWasAvailable(t *testing.T) {
	long := hSeries("EURUSD", 0)
	long.Bars[0].AvailableAt = hour(24) // opens first, lasts a day
	short := hSeries("GBPUSD", 1, 2, 3)

	got, err := runSeriesSeen(t, long, short)

	require.ErrorContains(t, err, "overlaps the previous one")
	assert.Len(t, got, 3, "the day-long bar was never delivered before it was available")
}

func TestBacktestRunSeries_RefusesMixedIntervalsBeforeDeliveringAnything(t *testing.T) {
	daily := hSeries("GBPUSD", 0)
	daily.Interval = "d1"

	got, err := runSeriesSeen(t, hSeries("EURUSD", 0), daily)

	require.ErrorContains(t, err, "one interval")
	assert.Empty(t, got)
}

func TestBacktestRunSeries_ACancelledContextStopsTheRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	spy := &seenStrategy{}
	tr, _ := pairTrader(usd(1000), spy)

	err := (&backtest{}).runSeries(ctx, tr, hSeries("EURUSD", 0, 1))

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, spy.seen)
}

// A single-instrument run through runSeries is the same run as the per-bar one.
func TestBacktestRunSeries_SingleInstrumentMatchesThePerBarRun(t *testing.T) {
	for _, scenario := range []string{"a-reversal-exit", "b2-stop-cross", "c-reversal"} {
		t.Run(scenario, func(t *testing.T) {
			perBar := runEMAX(t, scenario)
			grouped := newEMAXRun(t, usd(10000))
			require.NoError(t, (&backtest{}).runSeries(context.Background(), grouped.trader, scenarioPrices(t, scenario)))

			want, err := newResult(scenario, 0, perBar.trader)
			require.NoError(t, err)
			got, err := newResult(scenario, 0, grouped.trader)
			require.NoError(t, err)
			var wantOut, gotOut bytes.Buffer
			require.NoError(t, want.write(&wantOut))
			require.NoError(t, got.write(&gotOut))
			assert.Equal(t, wantOut.String(), gotOut.String())
		})
	}
}

// first is the first of orders, or nil.
// first is the first order a strategy's onBars returned, or nil. The strategies it
// wraps cannot fail, so an error is a bug in the test.
func first(orders []*order, err error) *order {
	if err != nil {
		panic(err)
	}
	if len(orders) == 0 {
		return nil
	}
	return orders[0]
}

// canonicalMonth builds one UTC month of raw hourly rows for the instrument into the
// canonical store (the market-data service's Build) and reads it back (its Load):
// the series a backtest gets from disk. A row is opened at each given hour of
// 2024-01-02, at the given bid close with a two-tick spread, so every mid is a
// whole tick. The partition is built from rows by hand, not read from a provider.
func canonicalMonth(t *testing.T, instrument string, bid int64, hours ...int) marketdata.Series {
	t.Helper()
	two := px(2)
	p := marketdata.SourcePartition{Source: "oanda", Instrument: instrument, Interval: "h1", Year: 2024, Month: time.January,
		Fingerprint: "sha256:" + instrument}
	for i, h := range hours {
		b := px(bid + int64(i))
		a, err := b.Add(two)
		require.NoError(t, err)
		ohlc := func(p num.Price) marketdata.OHLC { return marketdata.OHLC{Open: p, High: p, Low: p, Close: p} }
		p.Rows = append(p.Rows, marketdata.SourceRow{Row: i + 1, Complete: true, Bar: marketdata.Bar{
			Time: time.Date(2024, time.January, 2, h, 0, 0, 0, time.UTC), Volume: 10, Bid: ohlc(b), Ask: ohlc(a)}})
	}
	_, err := mdservice.Build(context.Background(), p, time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	got, err := mdservice.Load(context.Background(), mdservice.LoadRequest{Source: "oanda", Instrument: instrument, Interval: "h1",
		From: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	return got.Series
}

// The grouped replay on series that came through canonical storage: EURUSD has a
// bar every hour, GBPUSD none for the hours opening at 03:00 and 07:00. A group
// holds exactly the bars available at its instant, GBPUSD is absent where it has
// none (nothing carried forward), and a bar is never shown before its stored
// availability.
func TestBacktestRunSeries_OnSeriesLoadedFromCanonicalStorage(t *testing.T) {
	storetest.Root(t)
	captureLogs(t)
	eur := canonicalMonth(t, "EURUSD", 100000, 0, 1, 2, 3, 4, 5, 6, 7, 8)
	gbp := canonicalMonth(t, "GBPUSD", 200000, 0, 1, 2, 4, 5, 6, 8)
	require.Len(t, eur.Bars, 9)
	require.Len(t, gbp.Bars, 7)

	spy := &seenStrategy{}
	tr, _ := pairTrader(usd(10000), spy)
	require.NoError(t, (&backtest{}).runSeries(context.Background(), tr, gbp, eur))

	// Expected: every instant any bar is available, EURUSD then GBPUSD, from the
	// loaded series' own stored availability.
	has := map[string]map[time.Time]bool{"EURUSD": {}, "GBPUSD": {}}
	var instants []time.Time
	for _, s := range []marketdata.Series{eur, gbp} {
		for _, b := range s.Bars {
			has[s.Instrument][b.AvailableAt] = true
			instants = append(instants, b.AvailableAt)
		}
	}
	slices.SortFunc(instants, time.Time.Compare)
	instants = slices.Compact(instants)
	var want []string
	for _, at := range instants {
		for _, inst := range []string{"EURUSD", "GBPUSD"} {
			if has[inst][at] {
				want = append(want, inst+"@"+at.Format("Mon 15:04"))
			}
		}
	}
	assert.Equal(t, want, spy.seen, "one fresh group per stored availability instant, in instrument order")
	assert.Len(t, instants, 9)
	assert.Len(t, spy.seen, 16, "9 + 7 bars: nothing added, nothing carried forward")
	for _, b := range gbp.Bars {
		assert.False(t, b.AvailableAt.Equal(eur.Bars[3].AvailableAt) || b.AvailableAt.Equal(eur.Bars[7].AvailableAt),
			"GBPUSD has no bar at the instants EURUSD is alone")
	}
}

// warmSpy is a strategy that can be warmed: it records the bars it was warmed
// with and the groups it was asked about, and places no order.
type warmSpy struct {
	seenStrategy
	warmed []string
}

func (s *warmSpy) warm(_ context.Context, bars []marketdata.Bar) error {
	for _, b := range bars {
		s.warmed = append(s.warmed, b.Instrument+"@"+b.AvailableAt.Format("Mon 15:04"))
	}
	return nil
}

func TestBacktestRunSeriesFrom_WarmBarsPrefillTheStrategyAndAreNeverTraded(t *testing.T) {
	spy := &warmSpy{}
	tr, _ := pairTrader(usd(1000), spy)
	// EURUSD opens at hours 0..4, GBPUSD at 1 and 3; trading starts at hour 2.
	err := (&backtest{}).runSeriesFrom(context.Background(), tr, hour(2), hSeries("EURUSD", 0, 1, 2, 3, 4), hSeries("GBPUSD", 1, 3))

	require.NoError(t, err)
	at := func(h int) string { return hour(h).Format("Mon 15:04") }
	assert.Equal(t, []string{"EURUSD@" + at(1), "EURUSD@" + at(2), "GBPUSD@" + at(2)}, spy.warmed,
		"the bars that opened before hour 2, in availability order")
	assert.Equal(t, []string{"EURUSD@" + at(3), "EURUSD@" + at(4), "GBPUSD@" + at(4), "EURUSD@" + at(5)}, spy.seen,
		"the strategy decides only on bars that opened at or after hour 2")
}

func TestBacktestRunSeriesFrom_AStrategyThatCannotBeWarmedIsJustNotWarmed(t *testing.T) {
	spy := &seenStrategy{}
	tr, _ := pairTrader(usd(1000), spy)

	err := (&backtest{}).runSeriesFrom(context.Background(), tr, hour(2), hSeries("EURUSD", 0, 1, 2, 3))

	require.NoError(t, err)
	assert.Len(t, spy.seen, 2, "only the traded bars reach it")
}

func TestBacktestRunSeriesFrom_ZeroTradeFromTradesEverything(t *testing.T) {
	spy := &warmSpy{}
	tr, _ := pairTrader(usd(1000), spy)

	require.NoError(t, (&backtest{}).runSeriesFrom(context.Background(), tr, time.Time{}, hSeries("EURUSD", 0, 1, 2)))

	assert.Empty(t, spy.warmed)
	assert.Len(t, spy.seen, 3)
}
