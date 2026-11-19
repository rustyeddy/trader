package trader

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
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

// The real-data tests below replay canonical OANDA bars that were copied byte for
// byte from the archive into testdata/data (see testdata/data/PROVENANCE.md), read
// back through the public market-data read path (marketdata/service.Load) as any
// consumer reads them. They need no archive, credential or network, so they run in
// CI; the same read path over the whole archive is the optional smoke test in
// docs/real-data-replay.md.

var (
	utc = time.UTC
	// The one week of March 2024 in which the US moved to daylight saving time
	// (Sunday 10 March), and the January 2007 window in which OANDA has no EURUSD
	// candles for 22 hours while it has every GBPUSD candle.
	marchFrom, marchTo = time.Date(2024, 3, 1, 0, 0, 0, 0, utc), time.Date(2024, 4, 1, 0, 0, 0, 0, utc)
	janFrom, janTo     = time.Date(2007, 1, 1, 0, 0, 0, 0, utc), time.Date(2007, 2, 1, 0, 0, 0, 0, utc)
)

// copyFixtureTree copies the checked-in real data into dst, so a test reads it
// through the store at a root of its own and never changes the fixture.
func copyFixtureTree(t *testing.T, src, dst string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if filepath.Ext(path) != ".csv" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	}))
}

// loadReal reads one instrument's real H1 bars for [from, to) through the public
// market-data read path, with the strict default policy: a month that is missing,
// bad or stale, or a gap the calendar and the reviewed list do not explain, fails.
func loadReal(t *testing.T, instrument string, from, to time.Time) mdservice.LoadResult {
	t.Helper()
	copyFixtureTree(t, filepath.Join("testdata", "data"), storetest.Root(t))
	res, err := mdservice.Load(context.Background(), mdservice.LoadRequest{
		Source: "oanda", Instrument: instrument, Interval: "h1", From: from, To: to})
	require.NoError(t, err)
	return res
}

func realPair(t *testing.T, from, to time.Time) (eur, gbp marketdata.Series) {
	t.Helper()
	return loadReal(t, "EURUSD", from, to).Series, loadReal(t, "GBPUSD", from, to).Series
}

// engineSeries is realPair as the engine takes it in (RunSeries does the same): the
// prices rounded to the market's tick, since a real mid between two ticks (an odd
// spread) is not itself on one.
func engineSeries(t *testing.T, load func(*testing.T, time.Time, time.Time) (marketdata.Series, marketdata.Series), from, to time.Time) (marketdata.Series, marketdata.Series) {
	t.Helper()
	markets, err := defaultCatalog()
	require.NoError(t, err)
	a, b := load(t, from, to)
	for _, s := range []*marketdata.Series{&a, &b} {
		s.Bars = slices.Clone(s.Bars)
		require.NoError(t, markets.roundToTicks(s))
		require.NoError(t, markets.normalize(s))
	}
	return a, b
}

func TestRealData_PublicReadPathYieldsCanonicalBarsWithTheirProvenance(t *testing.T) {
	for _, instrument := range []string{"EURUSD", "GBPUSD"} {
		res := loadReal(t, instrument, marchFrom, marchTo)

		s := res.Series
		assert.Equal(t, "oanda", s.Source)
		assert.Equal(t, instrument, s.Instrument)
		assert.Equal(t, "h1", s.Interval)
		require.NoError(t, s.Validate())
		require.Len(t, res.Months, 1)
		assert.Equal(t, mdservice.Loaded, res.Months[0].State)
		assert.Equal(t, len(s.Bars), res.Months[0].Good)
		assert.Equal(t, 505, len(s.Bars), "March 2024 H1 in the archive's canonical file")
		for _, g := range res.Gaps {
			assert.NotEqual(t, mdservice.Unexplained, g.Kind, "no gap is unexplained: %v", g)
		}
		for _, b := range s.Bars {
			assert.Equal(t, b.Time.Add(time.Hour), b.AvailableAt, "persisted availability is the close of the hour")
			assert.False(t, b.Bid.IsZero() || b.Ask.IsZero() || b.Mid.IsZero(), "bid, ask and mid are all present")
		}
	}
}

func TestRealData_FramesGroupByPersistedAvailabilityInInstrumentOrder(t *testing.T) {
	eur, gbp := realPair(t, marchFrom, marchTo)

	var got []frame
	for f, err := range frames(mergedBars(context.Background(), gbp, eur)) {
		require.NoError(t, err)
		got = append(got, f)
	}

	require.NotEmpty(t, got)
	total := 0
	for i, f := range got {
		if i > 0 {
			assert.True(t, f.availableAt.After(got[i-1].availableAt), "frames are strictly ascending")
		}
		for j, b := range f.bars {
			assert.True(t, b.AvailableAt.Equal(f.availableAt), "a frame holds only the bars available at its instant")
			assert.True(t, b.Time.Before(f.availableAt), "a bar still open at the instant is never in the frame")
			if j > 0 {
				assert.Less(t, f.bars[j-1].Instrument, b.Instrument, "one bar per instrument, in instrument order")
			}
		}
		total += len(f.bars)
	}
	assert.Equal(t, len(eur.Bars)+len(gbp.Bars), total, "every bar is delivered once and none is added")
	assert.Equal(t, 505, len(got), "the two pairs share every hour of the month: one frame each")
}

func TestRealData_AMissingInstrumentIsAbsentFromItsFramesNotFilled(t *testing.T) {
	eur, gbp := realPair(t, janFrom, janTo)
	// OANDA has no EURUSD candle opening from 2007-01-08 23:00Z to 2007-01-09 20:00Z.
	holeStart, holeEnd := time.Date(2007, 1, 8, 23, 0, 0, 0, utc), time.Date(2007, 1, 9, 21, 0, 0, 0, utc)
	for _, b := range eur.Bars {
		require.False(t, !b.Time.Before(holeStart) && b.Time.Before(holeEnd), "the fixture's EURUSD hole: %s", b.Time)
	}

	sparse, both := 0, 0
	for f, err := range frames(mergedBars(context.Background(), eur, gbp)) {
		require.NoError(t, err)
		opened := f.availableAt.Add(-time.Hour)
		if !opened.Before(holeStart) && opened.Before(holeEnd) {
			sparse++
			require.Len(t, f.bars, 1, "frame %s", f.availableAt)
			assert.Equal(t, "GBPUSD", f.bars[0].Instrument)
			assert.True(t, f.bars[0].Time.Equal(opened), "the GBPUSD bar of that hour, not an older one")
		} else if len(f.bars) == 2 {
			both++
		}
	}

	assert.Equal(t, 22, sparse, "22 sparse frames: one per missing EURUSD hour")
	assert.Positive(t, both)
}

func TestRealData_TheStrategySeesEachBarOnlyOnceItHasClosedAndNothingCarriedForward(t *testing.T) {
	eur, gbp := engineSeries(t, realPair, janFrom, janTo)
	rec := &recordingStrategy{}
	tr, _ := pairTrader(usd(10000), rec)

	require.NoError(t, (&backtest{}).runSeries(context.Background(), tr, gbp, eur))

	require.NotEmpty(t, rec.calls)
	delivered := map[string]int{}
	var prev time.Time
	for _, call := range rec.calls {
		at := call[0].AvailableAt
		assert.True(t, at.After(prev), "calls are in availability order, one per instant")
		prev = at
		for _, b := range call {
			assert.True(t, b.AvailableAt.Equal(at))
			assert.True(t, b.Time.Add(time.Hour).Equal(at), "the bar had closed when it was delivered, so no open or future bar was used")
			delivered[b.Instrument]++
		}
	}
	assert.Equal(t, len(eur.Bars), delivered["EURUSD"], "no EURUSD bar was synthesized for its hole")
	assert.Equal(t, len(gbp.Bars), delivered["GBPUSD"])
}

func TestRealData_ReplayAcrossTheDaylightSavingChangeFollowsPersistedAvailability(t *testing.T) {
	eur, gbp := realPair(t, marchFrom, marchTo)
	// The week closes Friday 17:00 New York: 22:00Z in winter. It reopens Sunday 17:00:
	// 22:00Z before the change (3 March) and 21:00Z after it (10 March).
	opens := map[time.Time]bool{}
	for _, b := range eur.Bars {
		if b.Time.Weekday() == time.Sunday {
			opens[b.Time] = true
		}
	}
	assert.True(t, opens[time.Date(2024, 3, 3, 22, 0, 0, 0, utc)], "reopening before the change")
	assert.True(t, opens[time.Date(2024, 3, 10, 21, 0, 0, 0, utc)], "reopening after the change, an hour earlier in UTC")

	var instants []time.Time
	for f, err := range frames(mergedBars(context.Background(), eur, gbp)) {
		require.NoError(t, err)
		instants = append(instants, f.availableAt)
	}

	assert.True(t, slices.IsSortedFunc(instants, func(a, b time.Time) int { return a.Compare(b) }))
	assert.Contains(t, instants, time.Date(2024, 3, 10, 22, 0, 0, 0, utc), "the first frame after the change is at 22:00Z, the close of the 21:00Z bar")
	assert.NotContains(t, instants, time.Date(2024, 3, 10, 21, 0, 0, 0, utc), "nothing is available at 21:00Z, when the first bar opens")
}

func TestRealData_RunSeriesIsDeterministicWhateverTheSeriesOrder(t *testing.T) {
	captureLogs(t)
	eur, gbp := realPair(t, marchFrom, marchTo)
	write := func(series ...marketdata.Series) string {
		res, err := RunSeries(context.Background(), "oanda h1 2024-03", time.Time{}, series...)
		require.NoError(t, err)
		var out bytes.Buffer
		require.NoError(t, res.Write(&out))
		return out.String()
	}

	first := write(eur, gbp)

	assert.Equal(t, first, write(eur, gbp), "a repeat run")
	assert.Equal(t, first, write(gbp, eur), "the other order")
	assert.Contains(t, first, "oanda h1 2024-03: 1010 bars")
	assert.Contains(t, first, "orders: submitted 636, filled 423, rejected 0, cancelled 211, pending 2", "EMAX traded the real month")
	assert.Contains(t, first, "trades: closed 211, wins 60, losses 150, break-even 1")
	assert.Contains(t, first, "money: realized -$97.80, unrealized $9.49, balance $9902.20, equity $9911.69", "the result is reproducible to the cent")
}

func TestRealData_ATRStudyReadsTheSamePublicSeries(t *testing.T) {
	eur, _ := realPair(t, marchFrom, marchTo)
	const window = 14

	obs, err := runATRStudy(seriesBars(context.Background(), eur), window)

	require.NoError(t, err)
	require.Len(t, obs, len(eur.Bars)-window, "one value per bar once the window and the close before it are known")
	assert.True(t, obs[0].opened.Equal(eur.Bars[window].Time), "the first value waits for a full window of closed bars")
	again, err := runATRStudy(seriesBars(context.Background(), eur), window)
	require.NoError(t, err)
	assert.Equal(t, obs, again)
}

// recordingStrategy keeps what each callback was shown and trades nothing.
type recordingStrategy struct{ calls [][]marketdata.Bar }

func (*recordingStrategy) name() string { return "recording" }
func (*recordingStrategy) init() error  { return nil }
func (r *recordingStrategy) onBars(_ context.Context, bars []marketdata.Bar) []*order {
	r.calls = append(r.calls, slices.Clone(bars))
	return nil
}
