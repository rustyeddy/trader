package service

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quote is a raw hourly candle of the instrument at the given bid close with a
// spread of two ticks, so its mid is a whole tick (an odd spread has a half-tick
// mid).
func quote(t time.Time, bid string, spreadTicks int64) oanda.Record {
	b := num.MustParsePrice(bid)
	tick := num.MustParsePrice("0.00001")
	a := b
	for range spreadTicks {
		var err error
		a, err = a.Add(tick)
		if err != nil {
			panic(err)
		}
	}
	hi, lo := num.MustParsePrice("0.00100"), num.MustParsePrice("0.00100")
	bh, _ := b.Add(hi)
	bl, _ := b.Sub(lo)
	ah, _ := a.Add(hi)
	al, _ := a.Sub(lo)
	return oanda.Record{Time: t, BidOpen: b, BidHigh: bh, BidLow: bl, BidClose: b,
		AskOpen: a, AskHigh: ah, AskLow: al, AskClose: a, Volume: 100, Complete: true}
}

// month puts one raw month of hourly quotes for the instrument in the store: the
// bids follow the given points (ticks above the base, in ticks), two ticks of spread.
func month(t *testing.T, instrument string, m time.Month, base int64, points ...int64) {
	t.Helper()
	var rows []oanda.Record
	for h, p := range points {
		v := base + p
		rows = append(rows, quote(utc(2024, m, 2, h), fmt.Sprintf("%d.%05d", v/100000, v%100000), 2))
	}
	putRaw(t, instrument, 2024, m, rows...)
}

var wave = []int64{0, 0, 0, 0, 500, 1000, 1500, 1000, 500, 0, -500, -1000}

func backtestReq(instruments ...string) BacktestRequest {
	return BacktestRequest{Instruments: instruments, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0)}
}

func buildCanonical(t *testing.T, req BuildRequest) {
	t.Helper()
	_, err := run(t, req)
	require.NoError(t, err)
}

func TestRunBacktest_LoadsTwoInstrumentsFromCanonicalStorageAndRunsThem(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	month(t, "GBPUSD", time.January, 125000, wave...)
	buildCanonical(t, janToMar("EURUSD", "GBPUSD"))

	res, err := RunBacktest(context.Background(), nil, backtestReq("EURUSD", "GBPUSD"))

	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, res.Write(&out))
	assert.Contains(t, out.String(), "oanda EURUSD,GBPUSD h1 [2024-01-01T00:00:00Z, 2024-02-01T00:00:00Z): 24 bars")
	assert.Contains(t, out.String(), "orders: submitted")
	assert.NotContains(t, out.String(), "submitted 0,", "the crossovers produced orders on the shared account")
}

func TestRunBacktest_ARangeIsCutByOpeningTime(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	req := backtestReq("EURUSD")
	req.From, req.To = utc(2024, 1, 2, 2), utc(2024, 1, 2, 8) // opening times 02:00 to 07:00

	res, err := RunBacktest(context.Background(), nil, req)

	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, res.Write(&out))
	assert.Contains(t, out.String(), ": 6 bars")
}

func TestRunBacktest_AMissingMonthFailsUnlessAllowed(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	req := backtestReq("EURUSD")
	req.To = utc(2024, 3, 1, 0) // February has no data

	res, err := RunBacktest(context.Background(), nil, req)

	require.ErrorIs(t, err, ErrNotLoadable)
	assert.Nil(t, res)
	for _, want := range []string{"oanda EURUSD h1", "2024-02 missing"} {
		assert.Contains(t, err.Error(), want, "the error names source, instrument, interval and the month")
	}
	req.AllowMissing = true
	res, err = RunBacktest(context.Background(), nil, req)
	require.NoError(t, err)
	assert.NotNil(t, res)
}

func TestRunBacktest_ABadMonthFailsUnlessAllowedAndNothingRunsWhenOneInstrumentFails(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	// GBPUSD's raw month has a candle still forming: a bad record.
	rows := []oanda.Record{quote(utc(2024, 1, 2, 0), "1.25000", 2), quote(utc(2024, 1, 2, 1), "1.25000", 2)}
	rows[1].Complete = false
	putRaw(t, "GBPUSD", 2024, time.January, rows...)
	buildCanonical(t, janToMar("EURUSD", "GBPUSD"))

	res, err := RunBacktest(context.Background(), nil, backtestReq("EURUSD", "GBPUSD"))

	require.ErrorIs(t, err, ErrNotLoadable)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "GBPUSD")
	assert.Contains(t, err.Error(), "2024-01 bad")
	assert.NotContains(t, err.Error(), "EURUSD: ", "the instrument that loaded is not blamed")

	req := backtestReq("EURUSD", "GBPUSD")
	req.AllowBad = true
	res, err = RunBacktest(context.Background(), nil, req)
	require.NoError(t, err)
	assert.NotNil(t, res)
}

func TestRunBacktest_RefusesWhatCannotBeRun(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	reversed := backtestReq("EURUSD")
	reversed.From, reversed.To = reversed.To, reversed.From
	otherSource := backtestReq("EURUSD")
	otherSource.Source = "alpaca"
	noInstrument := backtestReq()
	badInterval := backtestReq("EURUSD")
	badInterval.Interval = "w1"
	nothing := backtestReq("EURUSD")
	nothing.From, nothing.To = utc(2024, 1, 20, 0), utc(2024, 1, 21, 0) // the month loads, no bar opens in it
	for name, tc := range map[string]struct {
		req  BacktestRequest
		want string
	}{
		"reversed range": {reversed, "--to must be after --from"},
		"other source":   {otherSource, `source "alpaca" is not supported`},
		"no instrument":  {noInstrument, "no instrument"},
		"bad interval":   {badInterval, "w1"},
		"no bars":        {nothing, "no bars in the range"},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := RunBacktest(context.Background(), nil, tc.req)

			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestRunBacktest_AnInstrumentTheEngineHasNoMarketForIsRefusedByTheEngine(t *testing.T) {
	storetest.Root(t)
	month(t, "USDJPY", time.January, 14000000, wave...)
	buildCanonical(t, janToMar("USDJPY"))

	res, err := RunBacktest(context.Background(), nil, backtestReq("USDJPY"))

	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "USDJPY")
	assert.Contains(t, err.Error(), "oanda USDJPY h1")
}

func TestRunBacktest_ACancelledContextStopsTheRun(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RunBacktest(ctx, nil, backtestReq("EURUSD"))

	require.ErrorIs(t, err, context.Canceled)
}
