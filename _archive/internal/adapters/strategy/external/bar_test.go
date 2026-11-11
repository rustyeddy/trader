package external_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/account"
	"github.com/rustyeddy/trader/internal/adapters/strategy/external"
	"github.com/rustyeddy/trader/internal/strategy"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func testBar(t *testing.T) marketdata.Bar {
	t.Helper()
	return marketdata.Bar{
		Time:      time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC),
		Open:      num.MustParsePrice("1.1000"),
		High:      num.MustParsePrice("1.1050"),
		Low:       num.MustParsePrice("1.0990"),
		Close:     num.MustParsePrice("1.1020"),
		AvgSpread: num.MustParsePrice("0.0001"),
		MaxSpread: num.MustParsePrice("0.0002"),
		Ticks:     123,
	}
}

func TestToWireBar(t *testing.T) {
	b := testBar(t)
	w := external.ToWireBar(b)

	require.Equal(t, b.Time.UTC().UnixNano(), w.GetTimeUnixNanos())
	require.Equal(t, "1.1", w.GetOpen())
	require.Equal(t, "1.105", w.GetHigh())
	require.Equal(t, "1.099", w.GetLow())
	require.Equal(t, "1.102", w.GetClose())
	require.Equal(t, "0.0001", w.GetAvgSpread())
	require.Equal(t, "0.0002", w.GetMaxSpread())
	require.Equal(t, int64(123), w.GetTicks())
}

func TestToWireBars_PreservesOrder(t *testing.T) {
	b1 := testBar(t)
	b2 := b1
	b2.Time = b1.Time.Add(time.Hour)

	got := external.ToWireBars([]marketdata.Bar{b1, b2})
	require.Len(t, got, 2)
	require.Equal(t, b1.Time.UTC().UnixNano(), got[0].GetTimeUnixNanos())
	require.Equal(t, b2.Time.UTC().UnixNano(), got[1].GetTimeUnixNanos())
}

func TestToWireBarEvent(t *testing.T) {
	inst := eurUSD(t)
	iv, err := marketdata.NewInterval(marketdata.UnitHour, 1)
	require.NoError(t, err)
	event := strategy.BarEvent{Instrument: inst, Interval: iv, Bar: testBar(t)}
	snap := testSnapshot(t)

	w, err := external.ToWireBarEvent(42, event, snap)
	require.NoError(t, err)

	require.Equal(t, uint64(42), w.GetSequence())
	require.Equal(t, inst.String(), w.GetInstrumentId())
	require.Equal(t, v1.IntervalUnit_INTERVAL_UNIT_HOUR, w.GetInterval().GetUnit())
	require.Equal(t, int32(1), w.GetInterval().GetCount())
	require.Equal(t, "1.1", w.GetBar().GetOpen())
	require.NotNil(t, w.GetAccount())
	require.Equal(t, snap.AccountID().String(), w.GetAccount().GetAccountId())
}

func TestToWireBarsEvent(t *testing.T) {
	eur := eurUSD(t)
	gbp := instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	event := strategy.BarsEvent{
		Boundary: testBar(t).Time,
		Interval: marketdata.H1,
		Bars:     []strategy.BarEvent{{Instrument: eur, Interval: marketdata.H1, Bar: testBar(t)}},
		Missing:  []instrument.ID{gbp},
	}
	w, err := external.ToWireBarsEvent(7, event, testSnapshot(t))
	require.NoError(t, err)

	require.Equal(t, uint64(7), w.GetSequence())
	require.Equal(t, event.Boundary.UnixNano(), w.GetBoundaryUnixNanos())
	require.Equal(t, v1.IntervalUnit_INTERVAL_UNIT_HOUR, w.GetInterval().GetUnit())
	require.Len(t, w.GetBars(), 1)
	require.Equal(t, eur.String(), w.GetBars()[0].GetInstrumentId())
	require.Equal(t, "1.102", w.GetBars()[0].GetBar().GetClose())
	require.Equal(t, []string{gbp.String()}, w.GetMissingInstrumentIds())
	require.NotNil(t, w.GetAccount())
}

func TestToWireBarsEvent_ZeroAccountSnapshotRejected(t *testing.T) {
	_, err := external.ToWireBarsEvent(1, strategy.BarsEvent{Boundary: testBar(t).Time, Interval: marketdata.H1}, account.Snapshot{})
	require.Error(t, err)
}
