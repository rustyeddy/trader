package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

// sendCapture is a v1.StrategyHostService_RunClient that records Send.
type sendCapture struct {
	v1.StrategyHostService_RunClient
	sent []*v1.RunClientMessage
}

func (c *sendCapture) Send(m *v1.RunClientMessage) error {
	c.sent = append(c.sent, m)
	return nil
}

func wireSnapshot() *v1.AccountSnapshot {
	return &v1.AccountSnapshot{AccountId: "a", Currency: "USD"}
}

// A host that sends the wrong delivery shape is answered with an
// explicit CAPABILITY_MISMATCH error response, never a panic or a
// silently dropped callback.
func TestGuestRun_DeliveryShapeMismatchIsExplicit(t *testing.T) {
	t.Run("bars event to bar consumer", func(t *testing.T) {
		stream := &sendCapture{}
		g := &guestRun{ctx: context.Background(), stream: stream, barHandler: nil, barsHandler: nil}
		iv := &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1}
		bar := testWireBar()
		require.NoError(t, g.handleBarsEvent(&v1.BarsEvent{
			Sequence: 3, Interval: iv, BoundaryUnixNanos: bar.TimeUnixNanos, Account: wireSnapshot(),
		}))
		require.Len(t, stream.sent, 1)
		resp := stream.sent[0].GetOnBarsResponse()
		require.Equal(t, uint64(3), resp.GetSequence())
		require.Equal(t, v1.ErrorCode_ERROR_CODE_CAPABILITY_MISMATCH, resp.GetError().GetCode())
	})

	t.Run("bar event to bars consumer", func(t *testing.T) {
		stream := &sendCapture{}
		g := &guestRun{ctx: context.Background(), stream: stream}
		require.NoError(t, g.handleBarEvent(&v1.BarEvent{
			Sequence: 4, InstrumentId: "fx:EUR/USD",
			Interval: &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
			Bar:      testWireBar(), Account: wireSnapshot(),
		}))
		resp := stream.sent[0].GetOnBarResponse()
		require.Equal(t, uint64(4), resp.GetSequence())
		require.Equal(t, v1.ErrorCode_ERROR_CODE_CAPABILITY_MISMATCH, resp.GetError().GetCode())
	})
}

func TestValidateBarsPartition(t *testing.T) {
	eur := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	gbp := instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	aud := instrument.CurrencyPairID(num.MustParseCurrency("AUD"), num.MustParseCurrency("USD"))
	h1, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	d1, err := marketdata.NewInterval(marketdata.UnitDay, 1)
	require.NoError(t, err)
	reqs := []DataRequirement{{Instrument: eur, Interval: h1}, {Instrument: gbp, Interval: h1}, {Instrument: aud, Interval: h1}}

	bar := func(ids ...instrument.ID) []BarEvent {
		out := make([]BarEvent, len(ids))
		for i, id := range ids {
			out[i] = BarEvent{Instrument: id, Interval: h1}
		}
		return out
	}
	tests := []struct {
		name string
		ev   BarsEvent
		want string // empty: valid
	}{
		{"all present", BarsEvent{Interval: h1, Bars: bar(eur, gbp, aud)}, ""},
		{"partition across both lists", BarsEvent{Interval: h1, Bars: bar(eur, aud), Missing: []instrument.ID{gbp}}, ""},
		{"all missing", BarsEvent{Interval: h1, Missing: []instrument.ID{eur, gbp, aud}}, ""},
		{"omitted from both", BarsEvent{Interval: h1, Bars: bar(eur, gbp)}, "neither bars nor missing"},
		{"in both lists", BarsEvent{Interval: h1, Bars: bar(eur, gbp, aud), Missing: []instrument.ID{gbp}}, "both or twice"},
		{"duplicate in bars", BarsEvent{Interval: h1, Bars: bar(eur, eur, gbp, aud)}, "both or twice"},
		{"duplicate in missing", BarsEvent{Interval: h1, Bars: bar(eur), Missing: []instrument.ID{gbp, gbp, aud}}, "both or twice"},
		{"undeclared instrument", BarsEvent{Interval: h1, Bars: bar(eur, gbp, aud, instrument.CurrencyPairID(num.MustParseCurrency("CHF"), num.MustParseCurrency("USD")))}, "never declared"},
		{"bars out of order", BarsEvent{Interval: h1, Bars: bar(gbp, eur, aud)}, "out of descriptor order"},
		{"missing out of order", BarsEvent{Interval: h1, Bars: bar(eur), Missing: []instrument.ID{aud, gbp}}, "out of descriptor order"},
		{"wrong interval", BarsEvent{Interval: d1, Bars: bar(eur, gbp, aud)}, "declared interval"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBarsPartition(tt.ev, reqs)
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalidWireValue)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

// A malformed snapshot is rejected before the consumer is ever called.
func TestGuestRun_RejectsMalformedSnapshotBeforeCallback(t *testing.T) {
	eur := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	h1, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	called := false
	g := &guestRun{
		ctx:          context.Background(),
		stream:       &sendCapture{},
		requirements: []DataRequirement{{Instrument: eur, Interval: h1}},
		barsHandler:  barsFunc(func() { called = true }),
	}
	// Declares EUR/USD but the snapshot lists nothing at all.
	err = g.handleBarsEvent(&v1.BarsEvent{
		Interval: &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1}, Account: wireSnapshot(),
	})
	require.ErrorIs(t, err, ErrInvalidWireValue)
	require.False(t, called)
}

type barsFunc func()

func (f barsFunc) OnBars(context.Context, BarsEvent, View) ([]DescribedIntent, []DescribedSignal, error) {
	f()
	return nil, nil, nil
}

func TestVerifyNegotiatedCapabilities_RejectsUnsolicited(t *testing.T) {
	fill, bars := v1.Capability_CAPABILITY_FILL_HANDLER, v1.Capability_CAPABILITY_BARS_DELIVERY
	require.NoError(t, verifyNegotiatedCapabilities(nil, nil))
	require.NoError(t, verifyNegotiatedCapabilities([]v1.Capability{fill, bars}, []v1.Capability{bars, fill}))
	require.ErrorIs(t, verifyNegotiatedCapabilities([]v1.Capability{fill}, nil), ErrInvalidWireValue, "dropped")
	err := verifyNegotiatedCapabilities(nil, []v1.Capability{bars})
	require.ErrorIs(t, err, ErrInvalidWireValue, "a bar-by-bar guest must not be handed snapshot delivery")
	require.ErrorContains(t, err, "did not request")
}
