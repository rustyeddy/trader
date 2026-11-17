package trader

import (
	"bytes"
	"context"
	"testing"
	"time"

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

	res, err := RunSeries(context.Background(), "oanda h1 test", time.Time{}, gbp, eur)

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
		res, err := RunSeries(context.Background(), "x", time.Time{}, series...)
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

	_, err := RunSeries(context.Background(), "x", time.Time{}, eur)

	require.NoError(t, err)
	assert.Equal(t, want, eur.Bars)
}

func TestRunSeries_RefusesWhatItCannotRunWithAClearError(t *testing.T) {
	noSource := wave("EURUSD", 1, upThenDown...)
	noSource.Source = ""
	crossed := wave("EURUSD", 1, upThenDown...)
	crossed.Bars[2].Bid = marketdata.OHLC{Open: px(100100), High: px(100100), Low: px(100100), Close: px(100100)}
	crossed.Bars[2].Ask = marketdata.OHLC{Open: px(100000), High: px(100000), Low: px(100000), Close: px(100000)}
	daily := wave("GBPUSD", 2, upThenDown...)
	daily.Interval = "d1"
	for name, tc := range map[string]struct {
		series []marketdata.Series
		want   string
		is     error
	}{
		"no series":           {nil, "no series", nil},
		"no market":           {[]marketdata.Series{wave("AUDUSD", 1, upThenDown...)}, "AUDUSD", errUnknownInstrument},
		"invalid series":      {[]marketdata.Series{noSource}, "EURUSD", marketdata.ErrInvalidSeries},
		"crossed bid and ask": {[]marketdata.Series{crossed}, "EURUSD", marketdata.ErrCrossed},
		"mixed intervals":     {[]marketdata.Series{wave("EURUSD", 1, upThenDown...), daily}, "one interval", nil},
		"same instrument two": {[]marketdata.Series{wave("EURUSD", 1, upThenDown...), wave("EURUSD", 1, upThenDown...)}, "twice", nil},
	} {
		t.Run(name, func(t *testing.T) {
			captureLogs(t)

			res, err := RunSeries(context.Background(), "x", time.Time{}, tc.series...)

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

	_, err := RunSeries(ctx, "x", time.Time{}, wave("EURUSD", 1, upThenDown...))

	require.ErrorIs(t, err, context.Canceled)
}

func TestRunSeries_WarmBarsPrefillTheIndicatorsAndAreNotCounted(t *testing.T) {
	captureLogs(t)
	s := wave("EURUSD", 1, 100000, 100100, 100200, 100300, 100400, 100500) // rising throughout
	traded := func(withWarm bool) string {
		series := s
		if !withWarm {
			series.Bars = s.Bars[4:] // only the two bars that are traded
		}
		res, err := RunSeries(context.Background(), "x", s.Bars[4].Time, series)
		require.NoError(t, err)
		var out bytes.Buffer
		require.NoError(t, res.Write(&out))
		return out.String()
	}

	warm, cold := traded(true), traded(false)

	assert.Contains(t, warm, "x: 2 bars", "the four warm-up bars are not counted")
	assert.Contains(t, cold, "x: 2 bars")
	assert.NotContains(t, warm, "submitted 0,", "with the EMAs prefilled the trend is traded from the first bar")
	assert.Contains(t, cold, "submitted 0,", "cold, two bars are too few for the long EMA")
}

// A mid price half a tick from a whole tick, which an odd bid/ask spread gives,
// trades like any other: the engine rounds on its own copy and the series passed in
// is not changed.
func TestRunSeries_RunsOnHalfTickMidsWithoutChangingThem(t *testing.T) {
	captureLogs(t)
	s := wave("EURUSD", 1, upThenDown...)
	half := num.MustParsePrice("0.000005")
	for i := range s.Bars {
		for _, p := range []*num.Price{&s.Bars[i].Mid.Open, &s.Bars[i].Mid.High, &s.Bars[i].Mid.Low, &s.Bars[i].Mid.Close} {
			v, err := p.Add(half)
			require.NoError(t, err)
			*p = v
		}
	}
	want := append([]marketdata.Bar(nil), s.Bars...)

	res, err := RunSeries(context.Background(), "x", time.Time{}, s)

	require.NoError(t, err, "no off-tick order stops the run")
	var out bytes.Buffer
	require.NoError(t, res.Write(&out))
	assert.NotContains(t, out.String(), "submitted 0,")
	assert.NotContains(t, out.String(), "rejected 1", "no order was refused")
	assert.Equal(t, want, s.Bars, "the observations are untouched")
}
