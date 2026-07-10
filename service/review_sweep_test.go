package service

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedWeekdayCandles writes one calendar month of candles for
// instrument/tf into the active (test) global store, populated only on
// weekdays (Mon-Fri UTC) to mimic FX markets being closed weekends.
// startPrice is the price at the first weekday slot; price increments by
// one PriceScale unit per valid slot so the series is monotonic and never
// flat (real, if arbitrary, variation for ADX/EMA/etc. to compute against).
func seedWeekdayCandles(t *testing.T, instrument string, tf market.Timeframe, monthStart time.Time, startPrice market.Price) {
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
// d1Months calendar months (reviewWeeklyLookbackDays=220 valid weekday
// candles needs roughly 11 months at ~21 weekdays/month), H4 across
// h4Months (reviewCandleCounts["H4"]=60 valid candles needs roughly 2
// weeks, so 1-2 months is ample).
func seedReviewHistory(t *testing.T, instrument string, asOf time.Time, d1Months, h4Months int) {
	t.Helper()

	asOfMonth := time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := d1Months; i >= 0; i-- {
		seedWeekdayCandles(t, instrument, market.D1, asOfMonth.AddDate(0, -i, 0), market.PriceFromFloat(1.0))
	}
	for i := h4Months; i >= 0; i-- {
		seedWeekdayCandles(t, instrument, market.H4, asOfMonth.AddDate(0, -i, 0), market.PriceFromFloat(1.0))
	}
}

func TestReviewWatchlistRange_SingleDateProducesOneResultPerInstrument(t *testing.T) {
	datamanager.UseTempDataDir(t)
	asOf := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC) // a Wednesday, far from any month boundary
	seedReviewHistory(t, "EURUSD", asOf, 12, 2)

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
	seedReviewHistory(t, "EURUSD", asOf, 12, 2)

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
	seedReviewHistory(t, "EURUSD", to, 12, 2)

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
	seedReviewHistory(t, "EURUSD", asOf, 1, 2) // far short of the ~11 months D1 needs

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
	seedWeekdayCandles(t, "EURUSD", market.D1, monthStart, market.PriceFromFloat(1.0))

	// A Wednesday: its own D1 bar (open 00:00, close +24h) has NOT closed
	// yet at its own open moment, and must therefore be excluded.
	wed := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)
	asOf := wed // exactly at the in-progress bar's open time

	candles, err := getClosedCandles(context.Background(), datamanager.GetDataManager(), "EURUSD", market.D1, asOf, 5)
	require.NoError(t, err)
	for _, ct := range candles {
		closeTime := ct.Timestamp.Time().Add(time.Duration(market.D1) * time.Second)
		assert.False(t, closeTime.After(asOf), "candle closing at %s must not extend past asOf %s", closeTime, asOf)
	}
	// The most recent returned candle must be Tuesday's, not Wednesday's.
	require.NotEmpty(t, candles)
	last := candles[len(candles)-1]
	assert.True(t, last.Timestamp.Time().Before(wed), "the most recent candle must be strictly before asOf's own in-progress bar")
}
