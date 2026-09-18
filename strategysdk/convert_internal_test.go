package strategysdk

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func testWireBar() *v1.Bar {
	return &v1.Bar{
		TimeUnixNanos: time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC).UnixNano(),
		Open:          "1.1",
		High:          "1.105",
		Low:           "1.099",
		Close:         "1.102",
		AvgSpread:     "0.0001",
		MaxSpread:     "0.0002",
		Ticks:         123,
	}
}

func TestFromWireBar(t *testing.T) {
	b, err := fromWireBar(testWireBar())
	require.NoError(t, err)
	require.True(t, b.Time.Equal(time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC)))
	require.Equal(t, "1.1", b.Open.String())
	require.Equal(t, int64(123), b.Ticks)
}

func TestFromWireBar_NilRejected(t *testing.T) {
	_, err := fromWireBar(nil)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireBar_InvalidOHLCRejected(t *testing.T) {
	w := testWireBar()
	w.High = "1.0" // now below Low ("1.099")
	_, err := fromWireBar(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireBar_AvgSpreadAboveMaxRejected(t *testing.T) {
	w := testWireBar()
	w.AvgSpread = "1.0" // now above MaxSpread ("0.0002")
	_, err := fromWireBar(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireBar_NegativeTicksRejected(t *testing.T) {
	w := testWireBar()
	w.Ticks = -1
	_, err := fromWireBar(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireBars_PreservesOrder(t *testing.T) {
	b1 := testWireBar()
	b2 := testWireBar()
	b2.TimeUnixNanos = b1.TimeUnixNanos + int64(time.Hour)

	bars, err := fromWireBars([]*v1.Bar{b1, b2})
	require.NoError(t, err)
	require.Len(t, bars, 2)
	require.True(t, bars[0].Time.Before(bars[1].Time))
}

func TestFromWireBarEvent(t *testing.T) {
	w := &v1.BarEvent{
		Sequence:     7,
		InstrumentId: "fx:EUR/USD",
		Interval:     &v1.Interval{Unit: v1.IntervalUnit_INTERVAL_UNIT_HOUR, Count: 1},
		Bar:          testWireBar(),
	}
	event, err := fromWireBarEvent(w)
	require.NoError(t, err)
	require.Equal(t, "fx:EUR/USD", event.Instrument.String())
	require.Equal(t, "1.1", event.Bar.Open.String())

	wantInterval, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	require.Equal(t, wantInterval, event.Interval)
}

func TestFromWirePositionSnapshot_Flat(t *testing.T) {
	w := &v1.PositionSnapshot{
		InstrumentId: "fx:EUR/USD",
		Side:         v1.PositionSide_POSITION_SIDE_FLAT,
	}
	p, err := fromWirePositionSnapshot(w)
	require.NoError(t, err)
	require.Equal(t, order.Flat, p.Side)
	require.Nil(t, p.AvgPrice)
}

func TestFromWirePositionSnapshot_Long(t *testing.T) {
	w := &v1.PositionSnapshot{
		InstrumentId: "fx:EUR/USD",
		Side:         v1.PositionSide_POSITION_SIDE_LONG,
		AvgPrice:     "1.1000",
	}
	p, err := fromWirePositionSnapshot(w)
	require.NoError(t, err)
	require.Equal(t, order.Long, p.Side)
	require.NotNil(t, p.AvgPrice)
	require.Equal(t, "1.1", p.AvgPrice.String())
}

func TestFromWireAccountSnapshot(t *testing.T) {
	w := &v1.AccountSnapshot{
		AccountId:     "acc_123",
		Currency:      "USD",
		AsOfUnixNanos: time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC).UnixNano(),
		Positions: []*v1.PositionSnapshot{
			{InstrumentId: "fx:EUR/USD", Side: v1.PositionSide_POSITION_SIDE_LONG, AvgPrice: "1.1"},
		},
	}
	acct, err := fromWireAccountSnapshot(w)
	require.NoError(t, err)
	require.Equal(t, "acc_123", acct.AccountID)
	require.Equal(t, "USD", acct.Currency)
	require.Len(t, acct.Positions, 1)
}

func TestFromWireFillEvent(t *testing.T) {
	w := &v1.FillEvent{
		Sequence:      3,
		OrderId:       "ord_1",
		InstrumentId:  "fx:EUR/USD",
		Side:          v1.Side_SIDE_BUY,
		Price:         "1.1005",
		Quantity:      "1000",
		CorrelationId: "cor_1",
		CausationId:   "",
	}
	fe, err := fromWireFillEvent(w)
	require.NoError(t, err)
	require.Equal(t, "ord_1", fe.OrderID)
	require.Equal(t, order.Buy, fe.Side)
	require.Equal(t, "1.1005", fe.Price.String())
	require.Equal(t, "1000", fe.Quantity.String())
	require.Equal(t, "cor_1", fe.CorrelationID)
	require.Empty(t, fe.CausationID)
}

func TestToWireDescriptor(t *testing.T) {
	iv, err := marketdata.NewInterval(marketdata.UnitDay, 1)
	require.NoError(t, err)
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))

	d := Descriptor{
		Name:    "sdk_guest",
		Version: "1.0",
		Requirements: []DataRequirement{
			{Instrument: inst, Interval: iv, WarmupBars: 20},
		},
	}
	w, err := toWireDescriptor(d)
	require.NoError(t, err)
	require.Equal(t, "sdk_guest", w.GetName())
	require.Len(t, w.GetRequirements(), 1)
	require.Equal(t, inst.String(), w.GetRequirements()[0].GetInstrumentId())
	require.Equal(t, int32(20), w.GetRequirements()[0].GetWarmupBars())
}

func TestToWireDescribedIntent_Enter(t *testing.T) {
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	w, err := toWireDescribedIntent(Enter(inst, order.Buy))
	require.NoError(t, err)
	require.Equal(t, v1.IntentKind_INTENT_KIND_ENTER, w.GetKind())
	require.Equal(t, v1.Side_SIDE_BUY, w.GetSide())
	require.Empty(t, w.GetQuantity())
	require.Empty(t, w.GetStopPrice())
}

func TestToWireDescribedIntent_ForbiddenFieldsRejected(t *testing.T) {
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	stop := num.MustParsePrice("1.09")

	bad := Exit(inst)
	bad.StopPrice = &stop // Exit forbids stop_price

	_, err := toWireDescribedIntent(bad)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestToWireDescribedIntent_MissingRequiredFieldRejected(t *testing.T) {
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	bad := DescribedIntent{Kind: order.IntentAdjustStop, Instrument: inst} // missing StopPrice
	_, err := toWireDescribedIntent(bad)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireBarEvent_NilRejected(t *testing.T) {
	_, err := fromWireBarEvent(nil)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWirePositionSnapshot_NilRejected(t *testing.T) {
	_, err := fromWirePositionSnapshot(nil)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireAccountSnapshot_NilRejected(t *testing.T) {
	_, err := fromWireAccountSnapshot(nil)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireFillEvent_NilRejected(t *testing.T) {
	_, err := fromWireFillEvent(nil)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestToWireDataRequirement_NegativeWarmupRejected(t *testing.T) {
	iv, err := marketdata.NewInterval(marketdata.UnitDay, 1)
	require.NoError(t, err)
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	_, err = toWireDataRequirement(DataRequirement{Instrument: inst, Interval: iv, WarmupBars: -1})
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestToWireDescribedIntent_ZeroInstrumentRejected(t *testing.T) {
	_, err := toWireDescribedIntent(Exit(instrument.ID{}))
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireFillEvent_InvalidInstrumentRejected(t *testing.T) {
	w := &v1.FillEvent{InstrumentId: "garbage", Side: v1.Side_SIDE_BUY, Price: "1.1", Quantity: "1"}
	_, err := fromWireFillEvent(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireFillEvent_InvalidSideRejected(t *testing.T) {
	w := &v1.FillEvent{InstrumentId: "fx:EUR/USD", Side: v1.Side_SIDE_UNSPECIFIED, Price: "1.1", Quantity: "1"}
	_, err := fromWireFillEvent(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireFillEvent_InvalidPriceRejected(t *testing.T) {
	w := &v1.FillEvent{InstrumentId: "fx:EUR/USD", Side: v1.Side_SIDE_BUY, Price: "", Quantity: "1"}
	_, err := fromWireFillEvent(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireFillEvent_InvalidQuantityRejected(t *testing.T) {
	w := &v1.FillEvent{InstrumentId: "fx:EUR/USD", Side: v1.Side_SIDE_BUY, Price: "1.1", Quantity: ""}
	_, err := fromWireFillEvent(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWirePositionSnapshot_InvalidSideRejected(t *testing.T) {
	w := &v1.PositionSnapshot{InstrumentId: "fx:EUR/USD", Side: v1.PositionSide(99)}
	_, err := fromWirePositionSnapshot(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWirePositionSnapshot_FlatWithAvgPriceRejected(t *testing.T) {
	w := &v1.PositionSnapshot{InstrumentId: "fx:EUR/USD", Side: v1.PositionSide_POSITION_SIDE_FLAT, AvgPrice: "1.1"}
	_, err := fromWirePositionSnapshot(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWirePositionSnapshot_LongWithoutAvgPriceRejected(t *testing.T) {
	w := &v1.PositionSnapshot{InstrumentId: "fx:EUR/USD", Side: v1.PositionSide_POSITION_SIDE_LONG}
	_, err := fromWirePositionSnapshot(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestToWireDataRequirement_WarmupBarsAboveInt32RangeRejected(t *testing.T) {
	iv, err := marketdata.NewInterval(marketdata.UnitDay, 1)
	require.NoError(t, err)
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	_, err = toWireDataRequirement(DataRequirement{Instrument: inst, Interval: iv, WarmupBars: math.MaxInt32 + 1})
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestToWireDescribedIntent_TargetExposureZeroQuantityRejected(t *testing.T) {
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	zero := num.Quantity{}
	bad := DescribedIntent{Kind: order.IntentTargetExposure, Instrument: inst, Side: order.Buy, Quantity: &zero}
	_, err := toWireDescribedIntent(bad)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestFromWireAccountSnapshot_InvalidPositionRejected(t *testing.T) {
	w := &v1.AccountSnapshot{
		AccountId: "acc", Currency: "USD",
		Positions: []*v1.PositionSnapshot{{InstrumentId: "garbage"}},
	}
	_, err := fromWireAccountSnapshot(w)
	require.ErrorIs(t, err, ErrInvalidWireValue)
}

func TestToWireDescribedIntent_TargetExposure(t *testing.T) {
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	qty := num.MustParseQuantity("500")
	w, err := toWireDescribedIntent(TargetExposure(inst, order.Sell, qty))
	require.NoError(t, err)
	require.Equal(t, v1.IntentKind_INTENT_KIND_TARGET_EXPOSURE, w.GetKind())
	require.Equal(t, "500", w.GetQuantity())
}

func TestToWireDescribedIntent_EnterWithStop(t *testing.T) {
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	stop := num.MustParsePrice("1.09")
	w, err := toWireDescribedIntent(EnterWithStop(inst, order.Buy, stop))
	require.NoError(t, err)
	require.Equal(t, v1.IntentKind_INTENT_KIND_ENTER_WITH_STOP, w.GetKind())
	require.Equal(t, "1.09", w.GetStopPrice())
}

func TestToWireDescribedIntent_AdjustStop(t *testing.T) {
	inst := instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	stop := num.MustParsePrice("1.08")
	w, err := toWireDescribedIntent(AdjustStop(inst, stop))
	require.NoError(t, err)
	require.Equal(t, v1.IntentKind_INTENT_KIND_ADJUST_STOP, w.GetKind())
	require.Equal(t, "1.08", w.GetStopPrice())
}

func TestFromWireBar_EveryFieldRejectsMalformedValue(t *testing.T) {
	tests := []func(*v1.Bar){
		func(w *v1.Bar) { w.Open = "" },
		func(w *v1.Bar) { w.High = "" },
		func(w *v1.Bar) { w.Low = "" },
		func(w *v1.Bar) { w.Close = "" },
		func(w *v1.Bar) { w.AvgSpread = "" },
		func(w *v1.Bar) { w.MaxSpread = "" },
	}
	for i, mutate := range tests {
		w := testWireBar()
		mutate(w)
		_, err := fromWireBar(w)
		require.ErrorIsf(t, err, ErrInvalidWireValue, "case %d", i)
	}
}

func TestToWireDescribedSignal(t *testing.T) {
	w := toWireDescribedSignal(Signal("sdk_guest", map[string]string{"k": "v"}).WithCorrelation("grp"))
	require.Equal(t, "sdk_guest", w.GetStrategy())
	require.Equal(t, "v", w.GetValues()["k"])
	require.Equal(t, "grp", w.GetCorrelationToken())
}
