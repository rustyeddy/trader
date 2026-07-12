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

	// Thresholds overrides review.DefaultThresholds() field-by-field; a
	// zero-valued field falls back to the default (see review.MergeThresholds).
	Thresholds review.Thresholds
}

// ReviewResponse is the full review output.
type ReviewResponse struct {
	ScannedAt time.Time             `json:"scanned_at"`
	Results   []review.ReviewResult `json:"results"`
}

// reviewWorkers bounds the concurrent OANDA candle fetches per review run.
const reviewWorkers = 8

// reviewCandleCounts is the per-timeframe candle window from docs/Review.org's
// "Data requirements" table. "W" is the number of weekly bars deriveWeeklyCandles
// produces from the D1 series, not a separate OANDA fetch (see reviewWeeklyLookbackDays).
var reviewCandleCounts = map[string]int{"W": 30, "D": 60, "H4": 60}

// reviewWeeklyLookbackDays sizes the D1 window fetched/cached so there is
// enough daily history to derive reviewCandleCounts["W"] complete weekly
// bars via deriveWeeklyCandles: ~30 weeks * 7 days, with headroom for
// holidays and the current partial week that gets dropped.
const reviewWeeklyLookbackDays = 220

// ReviewWatchlist runs the watchlist review over all instruments in
// req, fetches D1, H4, and W1 candles from DataManaager, computes all
// indicators, and returns a classified ReviewResponse. Instruments
// that fail to fetch or don't yet have enough candle history are
// skipped rather than failing the whole run; check s.Log for skip
// reasons.
func (s *Service) ReviewWatchlist(ctx context.Context, req ReviewRequest) (*ReviewResponse, error) {
	instruments := req.Instruments
	if len(instruments) == 0 {
		instruments = market.AllInstruments()
	}
	th := review.MergeThresholds(review.DefaultThresholds(), req.Thresholds)

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

			result, ok := s.reviewOneInstrument(ctx, name, th)
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

// reviewOneInstrument fetches D1/H4 candles, derives W1 from the D1 series,
// and runs review.ReviewPair for a single instrument. ok is false when the
// instrument should be skipped (fetch failure or insufficient candle
// history).
func (s *Service) reviewOneInstrument(ctx context.Context, name string, th review.Thresholds) (review.ReviewResult, bool) {
	log := s.Log
	if log == nil {
		log = slog.Default()
	}

	dailyWide, err := s.fetchReviewCandleTimes(ctx, name, "D", reviewWeeklyLookbackDays)
	if err != nil {
		log.Warn("review: fetch D1 candles", "instrument", name, "err", err)
		return review.ReviewResult{}, false
	}
	d1 := candlesOnly(dailyWide)
	if len(d1) > reviewCandleCounts["D"] {
		d1 = d1[len(d1)-reviewCandleCounts["D"]:]
	}
	// TODO: move this to datamanager, even though we may not persist
	// them still route through data manager
	w1 := deriveWeeklyCandles(dailyWide, reviewCandleCounts["W"])

	h4, err := s.fetchReviewCandles(ctx, name, "H4")
	if err != nil {
		log.Warn("review: fetch H4 candles", "instrument", name, "err", err)
		return review.ReviewResult{}, false
	}

	result, err := review.ReviewPair(name, w1, d1, h4, th)
	if err != nil {
		log.Warn("review: compute", "instrument", name, "err", err)
		return review.ReviewResult{}, false
	}
	return result, true
}

// reviewTimeframe maps a review OANDA granularity ("D", "H4") to the
// market.Timeframe the local candle store understands. There is no "W"
// case: weekly candles are derived from the cached D1 series (see
// deriveWeeklyCandles) rather than fetched as their own OANDA granularity.
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

// fetchReviewCandles fetches the most recent reviewCandleCounts[granularity]
// candles for instrument at the given OANDA granularity ("D", "H4") and
// strips them to the internal fixed-point market.Candle type.
func (s *Service) fetchReviewCandles(ctx context.Context, instrument, granularity string) ([]market.Candle, error) {
	cts, err := s.fetchReviewCandleTimes(ctx, instrument, granularity, reviewCandleCounts[granularity])
	if err != nil {
		return nil, err
	}
	return candlesOnly(cts), nil
}

// fetchReviewCandleTimes fetches the most recent count timestamped candles
// for instrument at the given OANDA granularity ("D", "H4"). D1 and H4 are
// served from the local DataManager-backed candle store (via GetCandles),
// topping it up from OANDA (and caching the result) only when the store is
// missing recent bars, falling back to a direct OANDA fetch when the cache
// still can't satisfy count afterwards.
func (s *Service) fetchReviewCandleTimes(ctx context.Context, instrument, granularity string, count int) ([]market.CandleTime, error) {
	inst := market.GetInstrument(instrument)
	if inst == nil {
		return nil, fmt.Errorf("review: unknown instrument %q", instrument)
	}
	oandaName := inst.BaseCurrency + "_" + inst.QuoteCurrency

	tf, ok := reviewTimeframe(granularity)
	if !ok {
		return nil, fmt.Errorf("review: unsupported granularity %q", granularity)
	}

	to := time.Now().UTC()
	from := to.Add(-datamanager.CandleWindow(tf, count))

	log := s.Log
	if log == nil {
		log = slog.Default()
	}

	// Change the name: while the candles are currently from
	if err := s.ensureCachedOandaCandles(ctx, oandaName, granularity, from, to); err != nil {
		log.Warn("review: top up local candle cache", "instrument", instrument, "granularity", granularity, "err", err)
	}

	// Q: Does this really need to be normalized?  inst.Instrument or instrument should
	// already be normalized
	instNorm := market.NormalizeInstrument(instrument)
	dm := datamanager.NewDataManager([]string{instNorm}, from, to)
	candles, err := dm.GetCandles(ctx, datamanager.CandleRequest{
		Source:     market.SourceOanda,
		Instrument: instNorm,
		Range:      market.TimeRange{TF: tf},
	}, to, count)
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
		return s.fetchReviewCandleTimesFromOANDA(ctx, instrument, oandaName, granularity, from, to, count)
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

	// XXX; we are certainly NOT going to D/L O&A candles here that is DM's job
	_, err = s.DownloadOandaCandles(ctx, DownloadOandaCandlesRequest{
		Instrument: oandaName,
		Timeframe:  granularity,
		From:       dlFrom,
		To:         to,
	})
	return err
}

// fetchReviewCandleTimesFromOANDA fetches timestamped candles directly from
// OANDA: used as a fallback when the local cache can't serve D1/H4 candles.
// XXX: No this is wrong.  This package should NEVER call oanda directly,
// always go through datamanager for this
func (s *Service) fetchReviewCandleTimesFromOANDA(ctx context.Context, instrument, oandaName, granularity string, from, to time.Time, count int) ([]market.CandleTime, error) {
	raw, err := s.OANDA.FetchCandles(ctx, oanda.FetchCandlesOptions{
		Instrument:  oandaName,
		Granularity: granularity,
		From:        from,
		To:          to,
	})

	// XXX: Either fix this or name it correctly.
	if err != nil {
		return nil, fmt.Errorf("fetch %s %s candles: %w", instrument, granularity, err)
	}

	candles := make([]market.CandleTime, 0, len(raw))
	for _, c := range raw {
		if !c.Complete {
			continue
		}
		candles = append(candles, oandaCandleToCandleTime(c, instrument))
	}
	if len(candles) > count {
		candles = candles[len(candles)-count:]
	}
	return candles, nil
}

// candlesOnly strips timestamps, keeping candle order.
func candlesOnly(cts []market.CandleTime) []market.Candle {
	out := make([]market.Candle, len(cts))
	for i, ct := range cts {
		out[i] = ct.Candle
	}
	return out
}
