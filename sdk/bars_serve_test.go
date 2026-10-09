package sdk_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
	"github.com/rustyeddy/trader/sdk"
)

// testBarsGuest is a pure sdk.BarsConsumer: it has no OnBar.
type testBarsGuest struct {
	descriptor sdk.Descriptor
	startedCh  chan sdk.Environment
	onBars     func(event sdk.BarsEvent, view sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error)
}

func newTestBarsGuest(d sdk.Descriptor) *testBarsGuest {
	return &testBarsGuest{descriptor: d, startedCh: make(chan sdk.Environment, 1)}
}

func (g *testBarsGuest) Describe() sdk.Descriptor { return g.descriptor }
func (g *testBarsGuest) Start(_ context.Context, env sdk.Environment) error {
	g.startedCh <- env
	return nil
}
func (g *testBarsGuest) OnBars(_ context.Context, e sdk.BarsEvent, v sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
	if g.onBars == nil {
		return nil, nil, nil
	}
	return g.onBars(e, v)
}

var _ sdk.BarsConsumer = (*testBarsGuest)(nil)

func gbpUSD(t *testing.T) instrument.ID {
	t.Helper()
	return instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
}

func barsDescriptor(t *testing.T) sdk.Descriptor {
	t.Helper()
	iv := mustInterval(t)
	return sdk.Descriptor{
		Name:    "scanner",
		Version: "1.0",
		Requirements: []sdk.DataRequirement{
			{Instrument: eurUSD(t), Interval: iv},
			{Instrument: gbpUSD(t), Interval: iv},
		},
	}
}

// historyView is fakeView plus strategy.History, so GetHistoryBars has
// something to answer from during an OnBars callback.
type historyView struct {
	fakeView
	bars map[instrument.ID][]marketdata.Bar
}

func (v historyView) HistoryBars(id instrument.ID, _ marketdata.Interval, n int) ([]marketdata.Bar, bool) {
	b, ok := v.bars[id]
	if !ok {
		return nil, false
	}
	if n < len(b) {
		b = b[len(b)-n:]
	}
	return b, true
}

// startBarsSession serves guest against a real Host and returns the
// host-side strategy, already Started.
func startBarsSession(t *testing.T, h *testHarness, guest sdk.ConsumerBase) strategy.Strategy {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	conn := h.dial()
	go func() { _ = sdk.ServeConn(ctx, conn, guest) }()

	strat, err := h.host.Strategy(context.Background())
	require.NoError(t, err)
	require.NoError(t, strat.Start(context.Background(), testEnvironment(t)))
	return strat
}

func TestBarsConsumer_SnapshotRoundTrip(t *testing.T) {
	h := newTestHarness(t)
	eur, gbp := eurUSD(t), gbpUSD(t)
	bar := testBar(t)
	earlier := bar
	earlier.Time = bar.Time.Add(-time.Hour)

	var got sdk.BarsEvent
	var history []marketdata.Bar
	var historyOK bool
	var undeclaredOK bool
	guest := newTestBarsGuest(barsDescriptor(t))
	guest.onBars = func(event sdk.BarsEvent, view sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
		got = event
		var err error
		history, historyOK, err = view.HistoryBars(eur, event.Interval, 5)
		require.NoError(t, err)
		_, undeclaredOK, err = view.HistoryBars(instrument.CurrencyPairID(num.MustParseCurrency("AUD"), num.MustParseCurrency("USD")), event.Interval, 5)
		require.NoError(t, err)
		return []sdk.DescribedIntent{sdk.Enter(eur, order.Buy)},
			[]sdk.DescribedSignal{{Strategy: "scanner", Values: map[string]string{"rank": "1"}}}, nil
	}

	strat := startBarsSession(t, h, guest)
	<-guest.startedCh

	bh, ok := strat.(strategy.BarsHandler)
	require.True(t, ok, "negotiated bars delivery must select the BarsHandler wrapper")

	// Only EUR/USD has a bar at this boundary: GBP/USD must be reported
	// Missing, not silently dropped.
	event := strategy.NewBarsEvent(bar.Time, []strategy.DataRequirement{
		{Instrument: eur, Interval: mustInterval(t)},
		{Instrument: gbp, Interval: mustInterval(t)},
	}, []strategy.BarEvent{{Instrument: eur, Interval: mustInterval(t), Bar: bar}})

	view := historyView{fakeView: fakeView{acct: testFlatSnapshot(t)}, bars: map[instrument.ID][]marketdata.Bar{eur: {earlier}}}
	intents, err := bh.OnBars(context.Background(), event, view)
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentEnter, intents[0].Kind)
	require.False(t, intents[0].IntentID.IsZero())

	require.True(t, got.Boundary.Equal(bar.Time))
	require.Equal(t, mustInterval(t), got.Interval)
	require.Len(t, got.Bars, 1)
	require.True(t, got.Bars[0].Instrument.Equal(eur))
	require.Equal(t, bar.Close, got.Bars[0].Bar.Close)
	require.Len(t, got.Missing, 1)
	require.True(t, got.Missing[0].Equal(gbp))

	require.True(t, historyOK)
	require.Len(t, history, 1)
	require.True(t, history[0].Time.Before(got.Boundary), "history must be strictly before the snapshot boundary")
	require.False(t, undeclaredOK, "an undeclared requirement has no history")

	// OnBar on a snapshot session is a contract violation.
	_, err = strat.OnBar(context.Background(), strategy.BarEvent{Instrument: eur, Interval: mustInterval(t), Bar: bar}, view)
	require.ErrorContains(t, err, "OnBars")
}

func TestBarsConsumer_CallbackErrorContributesNothing(t *testing.T) {
	h := newTestHarness(t)
	eur := eurUSD(t)
	guest := newTestBarsGuest(barsDescriptor(t))
	guest.onBars = func(sdk.BarsEvent, sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
		return []sdk.DescribedIntent{sdk.Enter(eur, order.Buy)}, nil, errors.New("scan failed")
	}
	strat := startBarsSession(t, h, guest)
	<-guest.startedCh

	event := strategy.NewBarsEvent(testBar(t).Time, nil, nil)
	event.Interval = mustInterval(t)
	intents, err := strat.(strategy.BarsHandler).OnBars(context.Background(), event, fakeView{acct: testFlatSnapshot(t)})
	require.ErrorContains(t, err, "scan failed")
	require.Empty(t, intents)
}

func TestBarsConsumer_FillHandlerCombination(t *testing.T) {
	h := newTestHarness(t)
	guest := &barsWithFill{testBarsGuest: newTestBarsGuest(barsDescriptor(t))}
	strat := startBarsSession(t, h, guest)
	<-guest.startedCh

	_, isBars := strat.(strategy.BarsHandler)
	_, isFill := strat.(strategy.FillHandler)
	require.True(t, isBars)
	require.True(t, isFill)
}

type barsWithFill struct{ *testBarsGuest }

func (barsWithFill) OnFill(context.Context, sdk.FillEvent, sdk.View) error { return nil }

// A bar-by-bar guest must not be offered snapshot delivery.
func TestBarConsumer_HostStrategyIsNotBarsHandler(t *testing.T) {
	h := newTestHarness(t)
	guest := newTestGuestStrategy(simpleSDKDescriptor(t, "plain"))
	strat := startBarsSession(t, h, guest)
	<-guest.startedCh
	_, isBars := strat.(strategy.BarsHandler)
	require.False(t, isBars)
}

func TestBarsConsumer_MixedIntervalsRejected(t *testing.T) {
	d := barsDescriptor(t)
	day, err := marketdata.NewInterval(marketdata.UnitDay, 1)
	require.NoError(t, err)
	d.Requirements[1].Interval = day

	// Guest side fails fast.
	err = sdk.ServeConn(context.Background(), nil, newTestBarsGuest(d))
	require.ErrorContains(t, err, "single interval")

	// Host side enforces it independently, for any guest.
	h := newTestHarness(t)
	wire := &v1.StrategyDescriptor{Name: "x", Version: "1", Requirements: []*v1.DataRequirement{
		{InstrumentId: eurUSD(t).String(), Interval: &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1}},
		{InstrumentId: gbpUSD(t).String(), Interval: &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_DAY, Count: 1}},
	}}
	_, err = v1.NewStrategyHostServiceClient(h.dial()).Handshake(context.Background(), &v1.HandshakeRequest{
		ProtocolVersion:    v1.ProtocolVersion,
		StrategyDescriptor: wire,
		Capabilities:       []v1.Capability{v1.Capability_CAPABILITY_BARS_DELIVERY},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
