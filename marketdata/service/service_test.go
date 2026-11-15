package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/marketdata/internal/canonical"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/store"
	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain points the store at a temporary directory for the whole package, so
// a test that forgets storetest.Root still never touches the real data root.
func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

func px(s string) num.Price { return num.MustParsePrice(s) }

func utc(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

var builtAt = utc(2026, 11, 15, 12)

// quote is a well-formed bid and ask for one candle.
func quote() (bid, ask marketdata.OHLC) {
	bid = marketdata.OHLC{Open: px("1.10000"), High: px("1.10100"), Low: px("1.09900"), Close: px("1.10050")}
	ask = marketdata.OHLC{Open: px("1.10010"), High: px("1.10110"), Low: px("1.09910"), Close: px("1.10060")}
	return bid, ask
}

// row is raw row n, opening at t, complete, with a well-formed quote.
func row(n int, t time.Time) marketdata.SourceRow {
	bid, ask := quote()
	return marketdata.SourceRow{Row: n, Complete: true, Bar: marketdata.Bar{Time: t, Volume: 100, Bid: bid, Ask: ask}}
}

func h1Source(rows ...marketdata.SourceRow) marketdata.SourcePartition {
	return marketdata.SourcePartition{
		Source: "oanda", Instrument: "EURUSD", Interval: "h1", Year: 2024, Month: time.January,
		Fingerprint: "sha256:aaa", Rows: rows,
	}
}

func readBack(t *testing.T, p marketdata.SourcePartition) canonical.File {
	t.Helper()
	f, err := canonical.Read(context.Background(), canonical.Key{
		Source: p.Source, Symbol: p.Instrument, Interval: canonical.Interval(p.Interval), Year: p.Year, Month: p.Month})
	require.NoError(t, err)
	return f
}

func TestBuild_GoodRowsBecomeBarsWithAvailabilityAndMid(t *testing.T) {
	storetest.Root(t)
	p := h1Source(row(1, utc(2024, 1, 2, 0)), row(2, utc(2024, 1, 2, 1)), row(3, utc(2024, 1, 2, 2)))

	res, err := Build(context.Background(), p, builtAt)

	require.NoError(t, err)
	assert.Equal(t, Result{Source: "oanda", Instrument: "EURUSD", Interval: "h1", Year: 2024, Month: time.January,
		Path: "oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv", Action: Built, Good: 3, Bad: 0}, res)
	f := readBack(t, p)
	require.Len(t, f.Bars, 3)
	assert.True(t, f.Clean())
	first := f.Bars[0]
	assert.Equal(t, utc(2024, 1, 2, 1), first.AvailableAt, "an h1 bar is available at the next hour")
	bid, ask := quote()
	assert.Equal(t, bid, first.Bid, "the provider's bid is stored unchanged")
	assert.Equal(t, ask, first.Ask, "the provider's ask is stored unchanged")
	assert.Equal(t, px("1.10005"), first.Mid.Open, "the mid is derived here, not by the provider")
	assert.Equal(t, int64(100), first.Volume)
	assert.Equal(t, "EURUSD", first.Instrument, "the bars are named from the partition's key")
	assert.Equal(t, "sha256:aaa", f.Meta.RawFingerprint)
	assert.Equal(t, BuilderVersion, f.Meta.BuilderVersion)
	assert.Equal(t, ValidatorVersion, f.Meta.ValidatorVersion)
	assert.Equal(t, canonical.PolicyScheduledCloseV1, f.Meta.AvailabilityPolicy)
	assert.Equal(t, builtAt, f.Meta.BuiltAt)
}

func TestBuild_EveryRejectionIsKeptWithItsReasonAndRawRow(t *testing.T) {
	storetest.Root(t)
	incomplete := row(2, utc(2024, 1, 2, 1))
	incomplete.Complete = false
	misaligned := row(3, time.Date(2024, 1, 2, 2, 30, 0, 0, time.UTC))
	shape := row(4, utc(2024, 1, 2, 3))
	shape.Bar.Bid.High = px("1.09000") // high below the low
	zero := row(5, utc(2024, 1, 2, 4))
	zero.Bar.Bid.Open = px("0")
	crossed := row(6, utc(2024, 1, 2, 5))
	crossed.Bar.Ask.Open = px("1.09990") // ask below the bid
	oneSided := row(7, utc(2024, 1, 2, 6))
	oneSided.Bar.Ask = marketdata.OHLC{}
	duplicate := row(9, utc(2024, 1, 2, 7))
	outOfOrder := row(10, utc(2024, 1, 2, 3))
	p := h1Source(row(1, utc(2024, 1, 2, 0)), incomplete, misaligned, shape, zero, crossed, oneSided,
		row(8, utc(2024, 1, 2, 7)), duplicate, outOfOrder, row(11, utc(2024, 1, 2, 8)))

	res, err := Build(context.Background(), p, builtAt)

	require.NoError(t, err)
	assert.Equal(t, 3, res.Good, "rows 1, 8 and 11 are good")
	assert.Equal(t, 8, res.Bad)
	f := readBack(t, p)
	assert.False(t, f.Clean())
	got := map[int]canonical.Reason{}
	for _, b := range f.Bad {
		got[b.RawRow] = b.Reason
	}
	assert.Equal(t, map[int]canonical.Reason{
		2: canonical.ReasonIncomplete, 3: canonical.ReasonMisaligned, 4: canonical.ReasonInvalid,
		5: canonical.ReasonInvalid, 6: canonical.ReasonCrossed, 7: canonical.ReasonInvalid,
		9: canonical.ReasonDuplicate, 10: canonical.ReasonOutOfOrder,
	}, got)
	assert.Equal(t, utc(2024, 1, 2, 7), f.Bars[1].Time, "the first of two rows with one time is the good one")
}

func TestBuild_ADuplicateOfABadRowIsStillGoodAtItsFirstCompleteAppearance(t *testing.T) {
	storetest.Root(t)
	first := row(1, utc(2024, 1, 2, 0))
	first.Complete = false
	p := h1Source(first, row(2, utc(2024, 1, 2, 0)))

	res, err := Build(context.Background(), p, builtAt)

	require.NoError(t, err)
	assert.Equal(t, 1, res.Good)
	assert.Equal(t, 1, res.Bad)
}

func TestBuild_AMidOverflowIsABadRecordNotAFailure(t *testing.T) {
	storetest.Root(t)
	huge := row(1, utc(2024, 1, 2, 0))
	huge.Bar.Bid.Open, huge.Bar.Ask.Open = px("90000000000"), px("90000000000")

	res, err := Build(context.Background(), h1Source(huge), builtAt)

	require.NoError(t, err)
	assert.Equal(t, 1, res.Bad)
}

func TestBuild_ARowOutsideThePartitionMonthFailsAndWritesNothing(t *testing.T) {
	storetest.Root(t)
	p := h1Source(row(1, utc(2024, 1, 2, 0)), row(2, utc(2024, 2, 1, 0)))

	_, err := Build(context.Background(), p, builtAt)

	require.ErrorIs(t, err, marketdata.ErrInvalidSource)
	_, rerr := canonical.Read(context.Background(), canonical.Key{Source: "oanda", Symbol: "EURUSD", Interval: canonical.H1, Year: 2024, Month: time.January})
	assert.ErrorIs(t, rerr, canonical.ErrNotFound)
}

func TestBuild_DailyBarsAlignToNewYorkAcrossTheDaylightSavingChange(t *testing.T) {
	storetest.Root(t)
	// 2024-03-10 is the US spring-forward Sunday: 17:00 New York is 22:00 UTC
	// before it (EST) and 21:00 UTC after it (EDT).
	saturday := row(1, utc(2024, 3, 9, 22))          // Sat 17:00 EST
	monday := row(2, utc(2024, 3, 11, 21))           // Mon 17:00 EDT
	wrongAfterChange := row(3, utc(2024, 3, 12, 22)) // the EST hour, but EDT is in force
	p := marketdata.SourcePartition{Source: "oanda", Instrument: "EURUSD", Interval: "d1", Year: 2024, Month: time.March,
		Fingerprint: "sha256:ddd", Rows: []marketdata.SourceRow{saturday, monday, wrongAfterChange}}

	res, err := Build(context.Background(), p, builtAt)

	require.NoError(t, err)
	assert.Equal(t, 2, res.Good)
	assert.Equal(t, 1, res.Bad)
	f := readBack(t, p)
	assert.Equal(t, utc(2024, 3, 10, 21), f.Bars[0].AvailableAt, "the day that crosses the change closes at 17:00 EDT: 23 hours later")
	assert.Equal(t, utc(2024, 3, 12, 21), f.Bars[1].AvailableAt, "a day entirely in EDT is 24 hours")
	assert.Equal(t, canonical.ReasonMisaligned, f.Bad[0].Reason)
}

func TestBuild_RebuildsDoNothingUnlessTheBuildChanged(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	p := h1Source(row(1, utc(2024, 1, 2, 0)))

	first, err := Build(ctx, p, builtAt)
	require.NoError(t, err)
	again, err := Build(ctx, p, builtAt.Add(24*time.Hour))
	require.NoError(t, err)
	p.Fingerprint = "sha256:changed"
	changed, err := Build(ctx, p, builtAt.Add(48*time.Hour))
	require.NoError(t, err)

	assert.Equal(t, Built, first.Action)
	assert.Equal(t, Unchanged, again.Action)
	assert.Equal(t, Replaced, changed.Action)
	assert.Equal(t, "sha256:changed", readBack(t, p).Meta.RawFingerprint)
}

func TestBuild_AnEmptyPartitionIsAFileWithNoBars(t *testing.T) {
	storetest.Root(t)

	res, err := Build(context.Background(), h1Source(), builtAt)

	require.NoError(t, err)
	assert.Equal(t, 0, res.Good)
	assert.Equal(t, Built, res.Action)
}

func TestBuild_RefusesWhatItCannotBuild(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	h4 := h1Source(row(1, utc(2024, 1, 2, 0)))
	h4.Interval = "h4"
	noPrint := h1Source(row(1, utc(2024, 1, 2, 0)))
	noPrint.Fingerprint = ""

	_, errInterval := Build(ctx, h4, builtAt)
	_, errSource := Build(ctx, noPrint, builtAt)
	_, errCtx := Build(cancelled, h1Source(), builtAt)

	assert.ErrorIs(t, errInterval, ErrUnsupportedInterval)
	assert.ErrorIs(t, errSource, marketdata.ErrInvalidSource)
	assert.ErrorIs(t, errCtx, context.Canceled)
}

func TestBuild_WritesOnlyUnderTheStoreRoot(t *testing.T) {
	root := storetest.Root(t)

	_, err := Build(context.Background(), h1Source(row(1, utc(2024, 1, 2, 0))), builtAt)

	require.NoError(t, err)
	files, err := store.List("")
	require.NoError(t, err)
	assert.Equal(t, []string{"oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv"}, files)
	assert.DirExists(t, root)
}

func TestBuild_ARowOfAnotherInstrumentIsRefused(t *testing.T) {
	storetest.Root(t)
	other := row(2, utc(2024, 1, 2, 1))
	other.Bar.Instrument = "GBPUSD"
	named := row(1, utc(2024, 1, 2, 0))
	named.Bar.Instrument = "EURUSD" // the partition's own name is fine

	ok, err := Build(context.Background(), h1Source(named), builtAt)
	_, errOther := Build(context.Background(), h1Source(named, other), builtAt)

	require.NoError(t, err)
	assert.Equal(t, 1, ok.Good)
	assert.ErrorIs(t, errOther, marketdata.ErrInvalidSource)
}
