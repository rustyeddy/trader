package trader

import (
	"bytes"
	"context"
	"testing"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wave is an hourly series of the instrument whose closes follow the given
// points, each bar opening at the previous close: up, then down, so a
// short/long EMA crossover happens on both.
func wave(instrument string, scale int64, points ...int64) marketdata.Series {
	s := marketdata.Series{Source: "test", Instrument: instrument, Interval: "h1"}
	prev := points[0]
	for h, p := range points {
		s.Bars = append(s.Bars, hbar(instrument, hour(h), prev*scale, p*scale))
		prev = p
	}
	return s
}

var upThenDown = []int64{100000, 100000, 100000, 100000, 100500, 101000, 101500, 101000, 100500, 100000, 99500, 99000}

func TestRunSeries_TradesTwoInstrumentsOnOneAccountAndSummarises(t *testing.T) {
	captureLogs(t)
	eur, gbp := wave("EURUSD", 1, upThenDown...), wave("GBPUSD", 2, upThenDown...)

	res, err := RunSeries(context.Background(), "oanda h1 test", gbp, eur)

	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, res.Write(&out))
	assert.Contains(t, out.String(), "oanda h1 test: 24 bars")
	assert.Contains(t, out.String(), "orders: submitted")
	assert.NotContains(t, out.String(), "submitted 0,", "the crossovers produced orders")
}

func TestRunSeries_IsTheSameWhateverTheSeriesOrder(t *testing.T) {
	captureLogs(t)
	eur, gbp := wave("EURUSD", 1, upThenDown...), wave("GBPUSD", 2, upThenDown...)
	write := func(series ...marketdata.Series) string {
		res, err := RunSeries(context.Background(), "x", series...)
		require.NoError(t, err)
		var out bytes.Buffer
		require.NoError(t, res.Write(&out))
		return out.String()
	}

	assert.Equal(t, write(eur, gbp), write(gbp, eur))
}

func TestRunSeries_LeavesTheCallersSeriesAlone(t *testing.T) {
	captureLogs(t)
	eur := wave("EURUSD", 1, upThenDown...)
	want := append([]marketdata.Bar(nil), eur.Bars...)

	_, err := RunSeries(context.Background(), "x", eur)

	require.NoError(t, err)
	assert.Equal(t, want, eur.Bars)
}

func TestRunSeries_RefusesWhatItCannotRunWithAClearError(t *testing.T) {
	noSource := wave("EURUSD", 1, upThenDown...)
	noSource.Source = ""
	daily := wave("GBPUSD", 2, upThenDown...)
	daily.Interval = "d1"
	offTick := wave("EURUSD", 1, upThenDown...)
	half := num.MustParsePrice("1.000005") // half a tick of the market
	offTick.Bars[3].Mid = marketdata.OHLC{Open: half, High: half, Low: half, Close: half}
	for name, tc := range map[string]struct {
		series []marketdata.Series
		want   string
		is     error
	}{
		"no series":           {nil, "no series", nil},
		"no market":           {[]marketdata.Series{wave("AUDUSD", 1, upThenDown...)}, "AUDUSD", errUnknownInstrument},
		"invalid series":      {[]marketdata.Series{noSource}, "EURUSD", marketdata.ErrInvalidSeries},
		"mixed intervals":     {[]marketdata.Series{wave("EURUSD", 1, upThenDown...), daily}, "one interval", nil},
		"off the tick":        {[]marketdata.Series{offTick}, "not a multiple of the tick", nil},
		"same instrument two": {[]marketdata.Series{wave("EURUSD", 1, upThenDown...), wave("EURUSD", 1, upThenDown...)}, "twice", nil},
	} {
		t.Run(name, func(t *testing.T) {
			captureLogs(t)

			res, err := RunSeries(context.Background(), "x", tc.series...)

			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tc.want)
			if tc.is != nil {
				assert.ErrorIs(t, err, tc.is)
			}
		})
	}
}

func TestRunSeries_ACancelledContextStopsTheRun(t *testing.T) {
	captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RunSeries(ctx, "x", wave("EURUSD", 1, upThenDown...))

	require.ErrorIs(t, err, context.Canceled)
}
