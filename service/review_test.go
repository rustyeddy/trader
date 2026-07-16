package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/review"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// swapTempStore points the global candle store at a fresh temp directory for
// the duration of the test, so review's local-cache reads/writes never touch
// the live /srv/trading/data store.
func swapTempStore(t *testing.T) {
	t.Helper()
	datamanager.UseTempDataDir(t)
}

// fakeOANDACandlesServer serves a monotonically increasing synthetic candle
// series for any /v3/instruments/{i}/candles request, starting at the
// requested "from" query param and spaced at the requested granularity's
// real duration (so the local-store grid the review path now writes through
// lands one candle per day/4h/week slot, not several per slot). Enough
// candles are returned to satisfy any of the review timeframe windows
// (W/D/H4).
func fakeOANDACandlesServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		from, err := time.Parse(time.RFC3339Nano, q.Get("from"))
		require.NoError(t, err)

		var step time.Duration
		switch q.Get("granularity") {
		case "W":
			step = 7 * 24 * time.Hour
		case "D":
			step = 24 * time.Hour
		case "H4":
			step = 4 * time.Hour
		default:
			step = time.Hour
		}

		const n = 300
		type ohlc struct{ O, H, L, C string }
		candles := make([]map[string]any, 0, n)
		price := 1.10000
		for i := range n {
			open := price
			price += 0.00050
			close := price
			bid := ohlc{
				O: fmt.Sprintf("%.5f", open),
				H: fmt.Sprintf("%.5f", close+0.00005),
				L: fmt.Sprintf("%.5f", open-0.00005),
				C: fmt.Sprintf("%.5f", close),
			}
			candles = append(candles, map[string]any{
				"complete": true,
				"time":     from.Add(time.Duration(i) * step).Format(time.RFC3339Nano),
				"volume":   10,
				"bid":      bid,
				"ask":      bid,
			})
		}

		resp := map[string]any{
			"instrument":  "EUR_USD",
			"granularity": q.Get("granularity"),
			"candles":     candles,
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestReviewWatchlist_SingleInstrument(t *testing.T) {
	swapTempStore(t)
	srv := fakeOANDACandlesServer(t)
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"EURUSD"}})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "EURUSD", resp.Results[0].Instrument)
	assert.NotEmpty(t, resp.Results[0].Bucket)
	assert.False(t, resp.ScannedAt.IsZero())
}

// TestReviewWatchlist_CustomThresholdsChangeBucket proves ReviewRequest.Thresholds
// actually reaches review.ReviewPair's classification: an unreasonably strict
// Hot-gate ADX floor forces every pair to "watch" regardless of how strongly
// it's trending. See issue #165.
func TestReviewWatchlist_CustomThresholdsChangeBucket(t *testing.T) {
	swapTempStore(t)
	srv := fakeOANDACandlesServer(t)
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{
		Instruments: []string{"EURUSD"},
		Thresholds:  review.Thresholds{HotD1ADXFloor: 1000},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "watch", resp.Results[0].Bucket)
}

// TestReviewWatchlist_NoH1FetchForNonTradeablePair confirms the live path
// never issues an OANDA "H1" request for a pair that doesn't classify
// tradeable — fakeOANDACandlesServer's plain monotonic series lands this
// fixture on "watch" (see TestReviewWatchlist_CustomThresholdsChangeBucket's
// sibling assertion), so a zero H1 request count here is the same
// conditional-fetch proof as the sweep-path equivalent in
// review_sweep_test.go, exercised through the live OANDA fetch path instead
// of the local-store replay path.
func TestReviewWatchlist_NoH1FetchForNonTradeablePair(t *testing.T) {
	swapTempStore(t)

	var mu sync.Mutex
	requestsByGranularity := map[string]int{}
	base := fakeOANDACandlesServer(t)
	defer base.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestsByGranularity[r.URL.Query().Get("granularity")]++
		mu.Unlock()
		base.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"EURUSD"}})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.NotEqual(t, "tradeable", resp.Results[0].Bucket, "fixture must not classify tradeable for this test to prove anything")

	mu.Lock()
	defer mu.Unlock()
	assert.Zero(t, requestsByGranularity["H1"], "H1 must never be fetched for a non-tradeable pair")
}

func TestReviewWatchlist_DefaultsToAllInstruments(t *testing.T) {
	swapTempStore(t)
	srv := fakeOANDACandlesServer(t)
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{})
	require.NoError(t, err)
	assert.Len(t, resp.Results, len(market.AllInstruments()))
}

func TestReviewWatchlist_SkipsInstrumentOnFetchFailure(t *testing.T) {
	swapTempStore(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorMessage":"bad token"}`))
	}))
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "bad"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"EURUSD"}})
	require.NoError(t, err, "per-instrument failures must not fail the whole run")
	assert.Empty(t, resp.Results)
}

// TestReviewWatchlist_CachesCandlesLocally verifies that D1/H4 candles are
// served from the local candle store on a second run instead of re-fetching
// the same history from OANDA every time, and that W1 (derived from the D1
// series) never triggers its own OANDA "W" granularity request at all.
func TestReviewWatchlist_CachesCandlesLocally(t *testing.T) {
	swapTempStore(t)

	var mu sync.Mutex
	requestsByGranularity := map[string]int{}
	base := fakeOANDACandlesServer(t)
	defer base.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestsByGranularity[r.URL.Query().Get("granularity")]++
		mu.Unlock()
		base.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	_, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"EURUSD"}})
	require.NoError(t, err)

	mu.Lock()
	firstRunD, firstRunH4, firstRunW := requestsByGranularity["D"], requestsByGranularity["H4"], requestsByGranularity["W"]
	requestsByGranularity = map[string]int{}
	mu.Unlock()
	require.Positive(t, firstRunD, "first run must populate the D1 cache from OANDA")
	require.Positive(t, firstRunH4, "first run must populate the H4 cache from OANDA")
	assert.Zero(t, firstRunW, "W1 must be derived from D1, never fetched from OANDA directly")

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"EURUSD"}})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)

	mu.Lock()
	secondRunD, secondRunH4 := requestsByGranularity["D"], requestsByGranularity["H4"]
	mu.Unlock()
	assert.Zero(t, secondRunD, "second run should serve D1 candles from the local cache, not re-fetch from OANDA")
	assert.Zero(t, secondRunH4, "second run should serve H4 candles from the local cache, not re-fetch from OANDA")
}

// TestReviewWatchlist_SecondRunServesPastMonthsFromMemoryNotDisk validates
// that DataManager's in-memory candle cache (datamanager/store.go's
// ReadCSV cache, docs/archive/asof-review-sweep-spec.md §1) is actually exercised
// end-to-end through the review path, not just unit-tested against store
// in isolation: after a first run populates the cache, deleting every
// on-disk CSV for months other than the current one (which the cache
// deliberately never serves, since it's a moving target) must not break a
// second run in the same process — those reads have to come from memory.
//
// Deleting the files alone isn't a strong enough check on its own:
// Candles() is non-strict by default, so a month it can't read is silently
// skipped rather than erroring, and fetchReviewCandleTimes's short-cache
// fallback would then quietly re-fetch from OANDA and mask a cache
// regression as a passing test. Counting OANDA requests on the second run
// closes that gap — if the cache weren't actually being hit, the deleted
// months would come back short, and the fallback would show up here as a
// nonzero request count.
func TestReviewWatchlist_SecondRunServesPastMonthsFromMemoryNotDisk(t *testing.T) {
	dir := datamanager.UseTempDataDir(t)

	var mu sync.Mutex
	requestsByGranularity := map[string]int{}
	base := fakeOANDACandlesServer(t)
	defer base.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestsByGranularity[r.URL.Query().Get("granularity")]++
		mu.Unlock()
		base.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"EURUSD"}})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)

	mu.Lock()
	requestsByGranularity = map[string]int{}
	mu.Unlock()

	removePastMonthCSVs(t, dir)

	resp2, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"EURUSD"}})
	require.NoError(t, err)
	require.Len(t, resp2.Results, 1, "second run must still succeed by serving past-month candles from the in-memory cache after their on-disk CSVs are gone")
	assert.Equal(t, resp.Results[0].Bucket, resp2.Results[0].Bucket)

	mu.Lock()
	defer mu.Unlock()
	assert.Zero(t, requestsByGranularity["D"], "a D1 re-fetch means the deleted past months came back short, i.e. the cache was not actually hit")
	assert.Zero(t, requestsByGranularity["H4"], "an H4 re-fetch means the deleted past months came back short, i.e. the cache was not actually hit")
}

// removePastMonthCSVs deletes every .csv file under dir except those in the
// current calendar month's directory (candles/<source>/<instrument>/<year>/<month>/*.csv),
// leaving the current month's file(s) alone since store.ReadCSV never
// caches the current month.
func removePastMonthCSVs(t *testing.T, dir string) {
	t.Helper()

	now := time.Now().UTC()
	currentMonthDir := filepath.Join(fmt.Sprintf("%04d", now.Year()), fmt.Sprintf("%02d", int(now.Month())))

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".csv" {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), filepath.ToSlash(currentMonthDir)) {
			return nil
		}
		return os.Remove(path)
	})
	require.NoError(t, err)
}

// TestFetchReviewCandles_FallsBackWhenCachedSeriesIsShort reproduces the
// production incident where a cached month file had a "flagged valid" row at
// today's date (so lastNonZeroCandleDate reports the cache as current) but
// held far fewer than reviewCandleCounts[granularity] usable bars — e.g.
// because the file was corrupted or only partially populated. Trusting the
// timestamp alone made review treat the cache as sufficient and skip the
// instrument outright; fetchReviewCandles must instead notice the short
// series and fall back to OANDA so it always returns a full window.
func TestFetchReviewCandles_FallsBackWhenCachedSeriesIsShort(t *testing.T) {
	swapTempStore(t)
	srv := fakeOANDACandlesServer(t)
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	// Seed the store with a D1 month file for the current month that has
	// exactly one valid row, dated today, and nothing else — mimicking a
	// short/corrupt cache that still passes the "last valid date is today"
	// check.
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	daysInMonth := monthStart.AddDate(0, 1, 0).Sub(monthStart) / (24 * time.Hour)
	candles := make([]market.Candle, int(daysInMonth))
	todayIdx := now.Day() - 1
	candles[todayIdx] = market.Candle{
		Open:  types.PriceFromFloat(1.1),
		High:  types.PriceFromFloat(1.1),
		Low:   types.PriceFromFloat(1.1),
		Close: types.PriceFromFloat(1.1),
	}
	datamanager.WriteCandles(t, market.SourceOanda, "EURUSD", types.D1, monthStart, candles)

	got, err := svc.fetchReviewCandles(context.Background(), "EURUSD", "D")
	require.NoError(t, err)
	assert.Len(t, got, reviewCandleCounts["D"], "must retry a full-window download rather than trust the short cache")
}

// TestFetchReviewCandles_RetryDownloadRepairsCacheForSubsequentReads confirms
// the retry path (retryReviewCandleTimesDownload) actually repairs the local
// store — via DataManager's forced full-window re-download — rather than
// only serving one live read. A second call, against a server that fails
// every request, must still succeed by reading the now-repaired cache.
func TestFetchReviewCandles_RetryDownloadRepairsCacheForSubsequentReads(t *testing.T) {
	swapTempStore(t)
	srv := fakeOANDACandlesServer(t)

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	// Same corrupted-cache setup as TestFetchReviewCandles_FallsBackWhenCachedSeriesIsShort.
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	daysInMonth := monthStart.AddDate(0, 1, 0).Sub(monthStart) / (24 * time.Hour)
	candles := make([]market.Candle, int(daysInMonth))
	todayIdx := now.Day() - 1
	candles[todayIdx] = market.Candle{
		Open:  types.PriceFromFloat(1.1),
		High:  types.PriceFromFloat(1.1),
		Low:   types.PriceFromFloat(1.1),
		Close: types.PriceFromFloat(1.1),
	}
	datamanager.WriteCandles(t, market.SourceOanda, "EURUSD", types.D1, monthStart, candles)

	got, err := svc.fetchReviewCandles(context.Background(), "EURUSD", "D")
	require.NoError(t, err)
	require.Len(t, got, reviewCandleCounts["D"])

	// Close the working server and swap in one that always errors, so any
	// second network fetch would fail — the retry's downloaded candles must
	// have actually been written to the local store, not just returned once.
	srv.Close()
	failingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failingSrv.Close()
	svc.OANDA = &oanda.Client{BaseURL: failingSrv.URL, Token: "t"}

	got2, err := svc.fetchReviewCandles(context.Background(), "EURUSD", "D")
	require.NoError(t, err, "second read must be served from the repaired local cache, not a network call")
	assert.Len(t, got2, reviewCandleCounts["D"])
}

// TestEnsureCachedOandaCandles_SubDailyTimeframeStillDownloadsSameDayGap
// reproduces the bug fixed by advancing the resume cursor by one bar
// (time.Duration(tf)*time.Second) instead of a flat calendar day
// (AddDate(0,0,1)).
//
// LastCompleteDate truncates to midnight UTC of the day containing the last
// valid candle — it cannot tell us how much of that day is actually cached.
// For a sub-daily timeframe (H4 here), only the day's first slot is cached
// below (00:00 UTC); the remaining slots (04:00..20:00) are gaps, as if a
// prior download had been interrupted partway through the day.
//
// The old `AddDate(0, 0, 1)` cursor jumped straight to the next day,
// implicitly trusting that the whole of the last-valid day was already
// downloaded. With `to` set later the same day, that pushed dlFrom past `to`
// and the function returned early as "already up to date" — silently
// leaving the day's later H4 gaps undownloaded forever. The fixed cursor
// only advances by one bar (4h for H4), so it still lands before `to` and a
// download is correctly triggered.
func TestEnsureCachedOandaCandles_SubDailyTimeframeStillDownloadsSameDayGap(t *testing.T) {
	swapTempStore(t)

	var requests int32
	base := fakeOANDACandlesServer(t)
	defer base.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		base.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	// Seed January 2020 H4 candles for EURUSD with only the 15th's midnight
	// slot (index 14*6=84) marked valid; every other slot, including the
	// rest of the 15th (04:00, 08:00, ... 20:00), is a zero-valued gap.
	monthStart := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]market.Candle, 85)
	candles[84] = market.Candle{
		Open:  types.PriceFromFloat(1.10),
		High:  types.PriceFromFloat(1.10),
		Low:   types.PriceFromFloat(1.10),
		Close: types.PriceFromFloat(1.10),
	}
	datamanager.WriteCandles(t, market.SourceOanda, "EURUSD", types.H4, monthStart, candles)

	last, err := datamanager.GetDataManager().LastCompleteDate("EUR_USD", types.H4, market.SourceOanda)
	require.NoError(t, err)
	require.True(t, last.Equal(time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)),
		"sanity: LastCompleteDate must report the 15th, truncated to midnight")

	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2020, 1, 15, 20, 0, 0, 0, time.UTC)

	err = svc.ensureCachedOandaCandles(context.Background(), "EUR_USD", "H4", from, to)
	require.NoError(t, err)

	assert.Positive(t, atomic.LoadInt32(&requests),
		"a same-day gap after the last cached H4 slot must still trigger a download, not be treated as already up to date")
}

func TestReviewWatchlist_UnknownInstrumentSkipped(t *testing.T) {
	swapTempStore(t)
	srv := fakeOANDACandlesServer(t)
	defer srv.Close()

	svc := &Service{
		OANDA: &oanda.Client{BaseURL: srv.URL, Token: "t"},
		Log:   discardLogger(),
	}

	resp, err := svc.ReviewWatchlist(context.Background(), ReviewRequest{Instruments: []string{"NOTAPAIR"}})
	require.NoError(t, err)
	assert.Empty(t, resp.Results)
}
