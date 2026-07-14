package service

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedWeekdayCandles writes one calendar month of candles for
// instrument/tf into the active (test) global store, populated only on
// weekdays (Mon-Fri UTC) to mimic FX markets being closed weekends.
// startPrice is the price at the first weekday slot; price increments by
// one PriceScale unit per valid slot so the series is monotonic and never
// flat (real, if arbitrary, variation for ADX/EMA/etc. to compute against).
func seedWeekdayCandles(t *testing.T, instrument string, tf types.Timeframe, monthStart time.Time, startPrice types.Price) {
	t.Helper()

	step := time.Duration(tf) * time.Second
	end := monthStart.AddDate(0, 1, 0)
	n := int(end.Sub(monthStart) / step)
	candles := make([]market.Candle, n)

	price := startPrice
	for i := range n {
		ts := monthStart.Add(time.Duration(i) * step)
		if wd := ts.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		price += 10
		candles[i] = market.Candle{
			Open: price, High: price + 5, Low: price - 5, Close: price + 2, Ticks: 1,
		}
	}
	datamanager.WriteCandles(t, market.SourceOanda, instrument, tf, monthStart, candles)
}

// seedReviewHistory seeds enough D1 and H4 weekday candle history ending at
// or before asOf for reviewOneInstrumentAsOf to succeed: D1 across
// d1Months calendar months (reviewWeeklyLookbackDays=340 valid weekday
// candles needs roughly 17 months at ~21 weekdays/month, per issue #175's
// ADX-convergence-driven widening), H4 across h4Months
// (reviewCandleCounts["H4"]=200 valid candles needs roughly 1.75 months at
// ~6 H4 candles/weekday, so 2-3 months is ample).
func seedReviewHistory(t *testing.T, instrument string, asOf time.Time, d1Months, h4Months int) {
	t.Helper()

	asOfMonth := time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := d1Months; i >= 0; i-- {
		seedWeekdayCandles(t, instrument, types.D1, asOfMonth.AddDate(0, -i, 0), types.PriceFromFloat(1.0))
	}
	for i := h4Months; i >= 0; i-- {
		seedWeekdayCandles(t, instrument, types.H4, asOfMonth.AddDate(0, -i, 0), types.PriceFromFloat(1.0))
	}
}

// seedTrendingMonths writes numMonths of continuous weekday candles for
// instrument/tf, with price carried forward across month boundaries (unlike
// seedWeekdayCandles, which resets to startPrice every call) so the series
// is a true multi-month monotonic trend rather than a repeating sawtooth.
func seedTrendingMonths(t *testing.T, instrument string, tf types.Timeframe, startMonth time.Time, numMonths int, startPrice types.Price) {
	t.Helper()
	price := startPrice
	step := time.Duration(tf) * time.Second
	for m := range numMonths {
		monthStart := startMonth.AddDate(0, m, 0)
		end := monthStart.AddDate(0, 1, 0)
		n := int(end.Sub(monthStart) / step)
		candles := make([]market.Candle, n)
		for i := range n {
			ts := monthStart.Add(time.Duration(i) * step)
			if wd := ts.Weekday(); wd == time.Saturday || wd == time.Sunday {
				continue
			}
			price += 10
			candles[i] = market.Candle{Open: price, High: price + 5, Low: price - 5, Close: price + 2, Ticks: 1}
		}
		datamanager.WriteCandles(t, market.SourceOanda, instrument, tf, monthStart, candles)
	}
}

// seedH4TradeablePullback writes one month of H4 candles that classify as
// "tradeable": a strong uptrend for most of the month, then an 8-candle
// pullback so the close lands within the Tradeable gate's H4 value zone
// (|price-EMA20| in [0.5, 1.5] ATR multiples) while ADX/CI/EMA-separation
// stay favorable. Calibrated empirically against the shipped Classify gates.
func seedH4TradeablePullback(t *testing.T, instrument string, monthStart time.Time) {
	t.Helper()
	step := 4 * time.Hour
	end := monthStart.AddDate(0, 1, 0)
	n := int(end.Sub(monthStart) / step)
	candles := make([]market.Candle, n)

	price := types.PriceFromFloat(1.10000)
	pullbackStart := n - 8
	for i := range n {
		ts := monthStart.Add(time.Duration(i) * step)
		if wd := ts.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		if i < pullbackStart {
			price += 15
		} else {
			price -= 8
		}
		candles[i] = market.Candle{Open: price, High: price + 36, Low: price - 36, Close: price + 2, Ticks: 1}
	}
	datamanager.WriteCandles(t, market.SourceOanda, instrument, types.H4, monthStart, candles)
}

// seedTradeableReviewHistory seeds D1/H4/H1 history that classifies EURUSD
// as "tradeable" as of asOf: a trending D1/W1 (satisfies the Hot gate) plus
// an H4 pullback into the value zone (satisfies the Tradeable gate), plus a
// trending H1 month so EnrichWithH1 has data to consume.
func seedTradeableReviewHistory(t *testing.T, instrument string, asOf time.Time) {
	t.Helper()
	asOfMonth := time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, time.UTC)

	seedTrendingMonths(t, instrument, types.D1, asOfMonth.AddDate(0, -18, 0), 19, types.PriceFromFloat(1.0))
	// H4 needs reviewCandleCounts["H4"]=200 valid candles ending at the
	// pullback; a leading trending month gives the window enough depth
	// (~126 H4 candles/month) before the pullback-shaped tail two months
	// still supplies the value-zone setup right at asOf.
	seedTrendingMonths(t, instrument, types.H4, asOfMonth.AddDate(0, -2, 0), 1, types.PriceFromFloat(1.0))
	seedH4TradeablePullback(t, instrument, asOfMonth.AddDate(0, -1, 0))
	seedH4TradeablePullback(t, instrument, asOfMonth)
	seedTrendingMonths(t, instrument, types.H1, asOfMonth.AddDate(0, -1, 0), 2, types.PriceFromFloat(1.0))
}

// TestReviewWatchlistRange_H1FetchedOnlyForTradeablePairs is the end-to-end
// proof for issue #166: a pair that classifies "tradeable" gets an H1
// enrichment (Setup.H1Aligned/H1EntryDist populated, H1 snapshot non-zero)
// computed in the very same sweep step — never a follow-up call. A pair
// left at Watch/Hot (no H4 pullback seeded) gets no H1 data at all.
func TestReviewWatchlistRange_H1FetchedOnlyForTradeablePairs(t *testing.T) {
	datamanager.UseTempDataDir(t)
	asOf := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)
	seedTradeableReviewHistory(t, "EURUSD", asOf)

	svc := &Service{Log: discardLogger()}
	resp, err := svc.ReviewWatchlistRange(context.Background(), ReviewRangeRequest{
		Instruments: []string{"EURUSD"},
		From:        asOf,
		To:          asOf,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)

	r := resp.Results[0]
	require.Equal(t, "tradeable", r.Bucket, "fixture must classify tradeable for this test to prove anything")
	assert.NotZero(t, r.H1.EMA20, "H1 snapshot must be populated for a tradeable pair")
	assert.NotContains(t, r.Notes, "H1 unavailable")
}

func TestReviewWatchlistRange_NonTradeablePairGetsNoH1(t *testing.T) {
	datamanager.UseTempDataDir(t)
	asOf := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)
	// Plain trending D1/H4 (via seedReviewHistory) lands on Hot or Watch,
	// never Tradeable — no H4 pullback into the value zone is seeded.
	seedReviewHistory(t, "EURUSD", asOf, 18, 2)

	svc := &Service{Log: discardLogger()}
	resp, err := svc.ReviewWatchlistRange(context.Background(), ReviewRangeRequest{
		Instruments: []string{"EURUSD"},
		From:        asOf,
		To:          asOf,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)

	r := resp.Results[0]
	require.NotEqual(t, "tradeable", r.Bucket, "fixture must not classify tradeable for this test to prove anything")
	assert.Zero(t, r.H1, "H1 must never be computed for a non-tradeable pair")
	assert.NotContains(t, r.Notes, "H1 unavailable", `a non-tradeable pair never attempts H1, so it can't be "unavailable"`)
}

func TestReviewWatchlistRange_SingleDateProducesOneResultPerInstrument(t *testing.T) {
	datamanager.UseTempDataDir(t)
	asOf := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC) // a Wednesday, far from any month boundary
	seedReviewHistory(t, "EURUSD", asOf, 18, 2)

	svc := &Service{Log: discardLogger()}
	resp, err := svc.ReviewWatchlistRange(context.Background(), ReviewRangeRequest{
		Instruments: []string{"EURUSD"},
		From:        asOf,
		To:          asOf,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "EURUSD", resp.Results[0].Instrument)
	assert.NotEmpty(t, resp.Results[0].Bucket)
	assert.True(t, resp.Results[0].ScannedAt.Equal(asOf), "ScannedAt must be the historical asOf, not time.Now()")
}

// TestReviewWatchlistRange_NeverTouchesOANDA confirms the sweep is a pure
// local-store replay: leaving Service.OANDA nil means any accidental
// network call (ensureCachedOandaCandles / fetchReviewCandleTimesFromOANDA,
// the live path's behaviors) would nil-pointer panic rather than silently
// succeed, so a passing test is itself proof no such call happened.
func TestReviewWatchlistRange_NeverTouchesOANDA(t *testing.T) {
	datamanager.UseTempDataDir(t)
	asOf := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)
	seedReviewHistory(t, "EURUSD", asOf, 18, 2)

	svc := &Service{Log: discardLogger()} // OANDA intentionally nil
	resp, err := svc.ReviewWatchlistRange(context.Background(), ReviewRangeRequest{
		Instruments: []string{"EURUSD"},
		From:        asOf,
		To:          asOf,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
}

func TestReviewWatchlistRange_MultiStepOrdersByInstrumentThenDate(t *testing.T) {
	datamanager.UseTempDataDir(t)
	to := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -2)
	seedReviewHistory(t, "EURUSD", to, 18, 2)

	svc := &Service{Log: discardLogger()}
	resp, err := svc.ReviewWatchlistRange(context.Background(), ReviewRangeRequest{
		Instruments: []string{"EURUSD"},
		From:        from,
		To:          to,
		Interval:    24 * time.Hour,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 3, "3 daily steps: from, from+1d, to")

	var gotDates []time.Time
	for _, r := range resp.Results {
		gotDates = append(gotDates, r.ScannedAt)
	}
	assert.True(t, gotDates[0].Equal(from))
	assert.True(t, gotDates[1].Equal(from.AddDate(0, 0, 1)))
	assert.True(t, gotDates[2].Equal(to))
}

// TestReviewWatchlistRange_MultiInstrumentOrdersByInstrumentThenDate is a
// regression test: the sweep loop appends in step-major order (date, then
// instrument) for fetch efficiency, but the documented — and CLI-relied-on
// — contract is instrument-major (grouped by pair, oldest date first) so a
// single pair's bucket transitions read as a contiguous time series.
// TestReviewWatchlistRange_MultiStepOrdersByInstrumentThenDate above only
// used one instrument, so step-major and instrument-major order were
// indistinguishable there; this uses two to actually exercise the ordering
// contract (per copilot review on PR #155).
func TestReviewWatchlistRange_MultiInstrumentOrdersByInstrumentThenDate(t *testing.T) {
	datamanager.UseTempDataDir(t)
	to := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -1)
	seedReviewHistory(t, "EURUSD", to, 18, 2)
	seedReviewHistory(t, "GBPUSD", to, 18, 2)

	svc := &Service{Log: discardLogger()}
	resp, err := svc.ReviewWatchlistRange(context.Background(), ReviewRangeRequest{
		Instruments: []string{"GBPUSD", "EURUSD"},
		From:        from,
		To:          to,
		Interval:    24 * time.Hour,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 4, "2 daily steps x 2 instruments")

	var got []string
	for _, r := range resp.Results {
		got = append(got, r.Instrument+"@"+r.ScannedAt.Format("2006-01-02"))
	}
	assert.Equal(t, []string{
		"EURUSD@2024-06-11", "EURUSD@2024-06-12",
		"GBPUSD@2024-06-11", "GBPUSD@2024-06-12",
	}, got, "results must be grouped by instrument, oldest date first within each group")
}

func TestReviewWatchlistRange_SkipsUnknownInstrument(t *testing.T) {
	datamanager.UseTempDataDir(t)
	asOf := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)

	svc := &Service{Log: discardLogger()}
	resp, err := svc.ReviewWatchlistRange(context.Background(), ReviewRangeRequest{
		Instruments: []string{"NOTAPAIR"},
		From:        asOf,
		To:          asOf,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Results)
}

// TestReviewWatchlistRange_SkipsInsufficientHistory confirms a step where
// the local store doesn't yet have enough D1 history (e.g. a date too
// close to the start of what's been seeded) is skipped, not failed.
func TestReviewWatchlistRange_SkipsInsufficientHistory(t *testing.T) {
	datamanager.UseTempDataDir(t)
	asOf := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)
	seedReviewHistory(t, "EURUSD", asOf, 1, 2) // far short of the ~17 months D1 needs

	svc := &Service{Log: discardLogger()}
	resp, err := svc.ReviewWatchlistRange(context.Background(), ReviewRangeRequest{
		Instruments: []string{"EURUSD"},
		From:        asOf,
		To:          asOf,
	})
	require.NoError(t, err, "insufficient history must be a skip, not a sweep-ending error")
	assert.Empty(t, resp.Results)
}

// TestGetClosedCandles_ExcludesBarNotYetClosedAtAsOf is the spec's explicit
// §4.2 acceptance check: pick a known date and assert GetCandles (via
// getClosedCandles) never returns a candle whose period extends past asOf.
func TestGetClosedCandles_ExcludesBarNotYetClosedAtAsOf(t *testing.T) {
	datamanager.UseTempDataDir(t)

	monthStart := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	seedWeekdayCandles(t, "EURUSD", types.D1, monthStart, types.PriceFromFloat(1.0))

	// A Wednesday: its own D1 bar (open 00:00, close +24h) has NOT closed
	// yet at its own open moment, and must therefore be excluded.
	wed := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)
	asOf := wed // exactly at the in-progress bar's open time

	candles, err := getClosedCandles(context.Background(), datamanager.GetDataManager(), "EURUSD", types.D1, asOf, 5)
	require.NoError(t, err)
	for _, ct := range candles {
		closeTime := ct.Timestamp.Time().Add(time.Duration(types.D1) * time.Second)
		assert.False(t, closeTime.After(asOf), "candle closing at %s must not extend past asOf %s", closeTime, asOf)
	}
	// The most recent returned candle must be Tuesday's, not Wednesday's.
	require.NotEmpty(t, candles)
	last := candles[len(candles)-1]
	assert.True(t, last.Timestamp.Time().Before(wed), "the most recent candle must be strictly before asOf's own in-progress bar")
}
