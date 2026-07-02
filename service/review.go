package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rustyeddy/trader/brokers/oanda"
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

// fetchReviewCandles fetches the most recent candleCount candles for
// instrument at the given OANDA granularity ("W", "D", "H4") and converts
// them to the internal fixed-point market.Candle type.
func (s *Service) fetchReviewCandles(ctx context.Context, instrument, granularity string) ([]market.Candle, error) {
	inst := market.GetInstrument(instrument)
	if inst == nil {
		return nil, fmt.Errorf("review: unknown instrument %q", instrument)
	}
	oandaName := inst.BaseCurrency + "_" + inst.QuoteCurrency
	count := reviewCandleCounts[granularity]

	to := time.Now().UTC()
	from := to.Add(-reviewWindow(granularity, count))

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
