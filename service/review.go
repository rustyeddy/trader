package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/review"
)

// ReviewRequest parameterises a watchlist review run.
type ReviewRequest struct {
	// Instruments to review. Defaults to market.AllInstruments() (every pair
	// in the instrument registry) when empty.
	Instruments []string
}

// ReviewResponse is the full review output.
type ReviewResponse struct {
	ScannedAt time.Time             `json:"scanned_at"`
	Results   []review.ReviewResult `json:"results"`
}

// reviewWorkers bounds the concurrent OANDA candle fetches per review run.
const reviewWorkers = 8

// reviewCandleCounts is the per-timeframe candle window from docs/Review.org's
// "Data requirements" table.
var reviewCandleCounts = map[string]int{"W": 30, "D": 60, "H4": 60}

// ReviewWatchlist runs the watchlist review over all instruments in req,
// fetches D1, H4, and W1 candles from OANDA, computes all indicators, and
// returns a classified ReviewResponse. Instruments that fail to fetch or
// don't yet have enough candle history are skipped rather than failing the
// whole run; check s.Log for skip reasons.
func (s *Service) ReviewWatchlist(ctx context.Context, req ReviewRequest) (*ReviewResponse, error) {
	instruments := req.Instruments
	if len(instruments) == 0 {
		instruments = market.AllInstruments()
	}

	results := make([]review.ReviewResult, 0, len(instruments))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, reviewWorkers)

	for _, name := range instruments {
		wg.Add(1)
		sem <- struct{}{}
		go func(name string) {
			defer wg.Done()
			defer func() { <-sem }()

			result, ok := s.reviewOneInstrument(ctx, name)
			if !ok {
				return
			}
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		}(name)
	}
	wg.Wait()

	return &ReviewResponse{
		ScannedAt: time.Now(),
		Results:   results,
	}, nil
}

// reviewOneInstrument fetches W1/D1/H4 candles and runs review.ReviewPair for
// a single instrument. ok is false when the instrument should be skipped
// (fetch failure or insufficient candle history).
func (s *Service) reviewOneInstrument(ctx context.Context, name string) (review.ReviewResult, bool) {
	log := s.Log
	if log == nil {
		log = slog.Default()
	}

	w1, err := s.fetchReviewCandles(ctx, name, "W")
	if err != nil {
		log.Warn("review: fetch W1 candles", "instrument", name, "err", err)
		return review.ReviewResult{}, false
	}
	d1, err := s.fetchReviewCandles(ctx, name, "D")
	if err != nil {
		log.Warn("review: fetch D1 candles", "instrument", name, "err", err)
		return review.ReviewResult{}, false
	}
	h4, err := s.fetchReviewCandles(ctx, name, "H4")
	if err != nil {
		log.Warn("review: fetch H4 candles", "instrument", name, "err", err)
		return review.ReviewResult{}, false
	}

	result, err := review.ReviewPair(name, w1, d1, h4)
	if err != nil {
		log.Warn("review: compute", "instrument", name, "err", err)
		return review.ReviewResult{}, false
	}
	return result, true
}

// reviewTimeframe maps a review OANDA granularity ("D", "H4") to the
// market.Timeframe the local candle store understands. "W" has no local
// timeframe/CSV representation (see CLAUDE.md's m1/h1/h4/d1 suffix list), so
// weekly candles always go straight to OANDA.
func reviewTimeframe(granularity string) (market.Timeframe, bool) {
	switch granularity {
	case "D":
		return market.D1, true
	case "H4":
		return market.H4, true
	default:
		return market.TF0, false
	}
}

// fetchReviewCandles fetches the most recent candleCount candles for
// instrument at the given OANDA granularity ("W", "D", "H4") and converts
// them to the internal fixed-point market.Candle type. D1 and H4 are served
// from the local DataManager-backed candle store, topping it up from OANDA
// (and caching the result) only when the store is missing recent bars; W1
// has no local store representation and is always fetched from OANDA.
func (s *Service) fetchReviewCandles(ctx context.Context, instrument, granularity string) ([]market.Candle, error) {
	inst := market.GetInstrument(instrument)
	if inst == nil {
		return nil, fmt.Errorf("review: unknown instrument %q", instrument)
	}
	oandaName := inst.BaseCurrency + "_" + inst.QuoteCurrency
	count := reviewCandleCounts[granularity]

	to := time.Now().UTC()
	from := to.Add(-reviewWindow(granularity, count))

	tf, cacheable := reviewTimeframe(granularity)
	if !cacheable {
		return s.fetchReviewCandlesFromOANDA(ctx, instrument, oandaName, granularity, from, to, count)
	}

	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	if err := s.ensureCachedOandaCandles(ctx, oandaName, granularity, from, to); err != nil {
		log.Warn("review: top up local candle cache", "instrument", instrument, "granularity", granularity, "err", err)
	}

	candles, err := s.readCachedOandaCandles(ctx, instrument, tf, from, to, count)
	if err != nil {
		log.Warn("review: read local candle cache", "instrument", instrument, "granularity", granularity, "err", err)
	}
	if len(candles) < count {
		// The cache doesn't hold enough bars to satisfy the review window —
		// whether because it's empty (brand-new pair), the top-up download
		// above failed, or the cached data is short/incomplete for some
		// other reason. lastNonZeroCandleDate only checks that a flagged-
		// valid row exists at some timestamp; it cannot confirm the cached
		// series actually has count bars of usable history, so this count
		// check is the real "is the cache good enough" gate. Fall back to a
		// direct OANDA fetch so review always gets a full series to work
		// with, rather than silently running indicators on too little data
		// (or skipping the instrument outright).
		return s.fetchReviewCandlesFromOANDA(ctx, instrument, oandaName, granularity, from, to, count)
	}
	return candles, nil
}

// ensureCachedOandaCandles downloads and writes into the local candle store
// (via DownloadOandaCandles/DataManager's canonical layout) any candles
// between the last cached bar and to, so repeated review runs stop
// re-downloading the same history from OANDA. A no-op when the store is
// already current.
//
// The download always starts at the beginning of the calendar month
// containing the gap, never mid-month: DownloadOandaCandles writes a whole
// month file per call, so a mid-month start would silently zero out the
// already-cached earlier days of that month.
func (s *Service) ensureCachedOandaCandles(ctx context.Context, oandaName, granularity string, from, to time.Time) error {
	tf, err := market.ParseTimeframe(granularity)
	if err != nil {
		return fmt.Errorf("unknown timeframe %q", granularity)
	}

	dlFrom := from
	if last, err := datamanager.GetDataManager().LastCompleteDate(oandaName, tf, market.SourceOanda); err == nil {
		if next := last.AddDate(0, 0, 1); next.After(dlFrom) {
			dlFrom = next
		}
	}
	if !dlFrom.Before(to) {
		return nil // already up to date, nothing to download
	}
	dlFrom = time.Date(dlFrom.Year(), dlFrom.Month(), 1, 0, 0, 0, 0, time.UTC)
	_, err = s.DownloadOandaCandles(ctx, DownloadOandaCandlesRequest{
		Instrument: oandaName,
		Timeframe:  granularity,
		From:       dlFrom,
		To:         to,
	})
	return err
}

// readCachedOandaCandles loads candles for instrument/tf from the local
// candle store via DataManager, trimmed to the most recent count bars.
func (s *Service) readCachedOandaCandles(ctx context.Context, instrument string, tf market.Timeframe, from, to time.Time, count int) ([]market.Candle, error) {
	instNorm := market.NormalizeInstrument(instrument)
	dm := datamanager.NewDataManager([]string{instNorm}, from, to)
	iter, err := dm.Candles(ctx, datamanager.CandleRequest{
		Source:     market.SourceOanda,
		Instrument: instNorm,
		Range:      market.TimeRange{Start: market.FromTime(from), End: market.FromTime(to), TF: tf},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	var candles []market.Candle
	for ct, ok := iter.Next(); ok; ct, ok = iter.Next() {
		if ct.Candle.IsZero() {
			continue
		}
		candles = append(candles, ct.Candle)
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	if len(candles) > count {
		candles = candles[len(candles)-count:]
	}
	return candles, nil
}

// fetchReviewCandlesFromOANDA fetches candles directly from OANDA: used for
// weekly candles (no local store representation) and as a fallback when the
// local cache can't serve D1/H4 candles.
func (s *Service) fetchReviewCandlesFromOANDA(ctx context.Context, instrument, oandaName, granularity string, from, to time.Time, count int) ([]market.Candle, error) {
	raw, err := s.OANDA.FetchCandles(ctx, oanda.FetchCandlesOptions{
		Instrument:  oandaName,
		Granularity: granularity,
		From:        from,
		To:          to,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch %s %s candles: %w", instrument, granularity, err)
	}

	candles := make([]market.Candle, 0, len(raw))
	for _, c := range raw {
		if !c.Complete {
			continue
		}
		candles = append(candles, oandaCandleToCandleTime(c, instrument).Candle)
	}
	if len(candles) > count {
		candles = candles[len(candles)-count:]
	}
	return candles, nil
}

// reviewWindow returns the calendar duration needed to cover count candles
// at the given granularity, with a buffer for weekends/holidays.
func reviewWindow(granularity string, count int) time.Duration {
	switch granularity {
	case "W":
		return time.Duration(float64(count) * 7 * 24 * float64(time.Hour) * 1.3)
	case "H4":
		return time.Duration(float64(count) * 4 * float64(time.Hour) * 1.4)
	default: // "D"
		return time.Duration(float64(count) * 24 * float64(time.Hour) * 1.4)
	}
}
