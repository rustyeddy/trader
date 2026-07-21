// Package reviewsvc is the service-layer orchestration for watchlist
// review scans, mirroring cmd/review. It is protocol-agnostic (see
// service/service.go's package doc) — shared by cmd/review, api/rest, and
// api/mcp, which reach it via the aliases re-exported from the top-level
// service package (see service/review.go) rather than importing this
// package directly.
//
// Named reviewsvc, not review, because the top-level
// "github.com/rustyeddy/trader/review" package (pure classification logic
// — thresholds, ReviewPair, ReviewResult) is imported alongside this one
// in every real call site; two packages both named review in the same
// file would collide.
package reviewsvc

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
	datasvc "github.com/rustyeddy/trader/service/data"
	"github.com/rustyeddy/trader/types"
)

// Service holds the dependency set review orchestration needs — no
// relation to the core service.Service beyond being called through it
// (see service/review.go).
type Service struct {
	OANDA *oanda.Client
	Log   *slog.Logger
}

// dataSvc constructs the candle-acquisition sub-service used to top up the
// local store from OANDA. review depends on data (one-directional, no
// cycle): data never calls back into review.
func (s *Service) dataSvc() *datasvc.Service {
	return &datasvc.Service{OANDA: s.OANDA}
}

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

// ReviewRangeRequest and ReviewSweepResponse are aliases for the
// review package's sweep types, kept here so existing call sites
// (service.ReviewRangeRequest{...} etc., via service/review.go's
// re-exports) keep compiling unchanged.
type (
	ReviewRangeRequest  = review.SweepRequest
	ReviewSweepResponse = review.SweepResponse
)

// ReviewWatchlistRange runs a historical classification sweep: a thin
// delegator to review.RunSweep, the provider both this live-review service
// and service/reviewsweep call directly — neither depends on the other.
func (s *Service) ReviewWatchlistRange(ctx context.Context, req ReviewRangeRequest) (*ReviewSweepResponse, error) {
	return review.RunSweep(ctx, s.Log, req)
}

// reviewWorkers bounds the concurrent OANDA candle fetches per review run.
const reviewWorkers = 8

// Candle-window sizing (review.CandleCounts, review.WeeklyLookbackDays) and
// the ReviewWatchlistRange/RunSweep historical replay engine live in the
// top-level review package now — see review/candles.go and review/replay.go
// — since both this live path and service/reviewsweep need them and
// neither should call through the other's service package.

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

	dailyWide, err := s.fetchReviewCandleTimes(ctx, name, "D", review.WeeklyLookbackDays)
	if err != nil {
		log.Warn("review: fetch D1 candles", "instrument", name, "err", err)
		return review.ReviewResult{}, false
	}
	d1 := review.CandlesOnly(dailyWide)
	if len(d1) > review.CandleCounts["D"] {
		d1 = d1[len(d1)-review.CandleCounts["D"]:]
	}
	// TODO: move this to datamanager, even though we may not persist
	// them still route through data manager
	w1 := review.DeriveWeeklyCandles(dailyWide, review.CandleCounts["W"])

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

	result = review.EnrichTradeableWithH1(result, log, name, func() ([]market.Candle, error) {
		return s.fetchReviewCandles(ctx, name, "H1")
	})

	return result, true
}

// reviewTimeframe maps a review OANDA granularity ("D", "H4", "H1") to the
// types.Timeframe the local candle store understands. There is no "W"
// case: weekly candles are derived from the cached D1 series (see
// review.DeriveWeeklyCandles) rather than fetched as their own OANDA
// granularity.
func reviewTimeframe(granularity string) (types.Timeframe, bool) {
	switch granularity {
	case "D":
		return types.D1, true
	case "H4":
		return types.H4, true
	case "H1":
		return types.H1, true
	default:
		return types.TF0, false
	}
}

// fetchReviewCandles fetches the most recent review.CandleCounts[granularity]
// candles for instrument at the given OANDA granularity ("D", "H4") and
// strips them to the internal fixed-point market.Candle type.
func (s *Service) fetchReviewCandles(ctx context.Context, instrument, granularity string) ([]market.Candle, error) {
	cts, err := s.fetchReviewCandleTimes(ctx, instrument, granularity, review.CandleCounts[granularity])
	if err != nil {
		return nil, err
	}
	return review.CandlesOnly(cts), nil
}

// fetchReviewCandleTimes fetches the most recent count timestamped candles
// for instrument at the given OANDA granularity ("D", "H4"). D1 and H4 are
// served from the local DataManager-backed candle store (via GetCandles),
// topping it up from OANDA (and caching the result) only when the store is
// missing recent bars, retrying with a forced full-window re-download
// (still through DataManager) when the cache still can't satisfy count
// afterwards.
func (s *Service) fetchReviewCandleTimes(ctx context.Context, instrument, granularity string, count int) ([]market.Candle, error) {
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

	if err := s.ensureCachedOandaCandles(ctx, oandaName, granularity, from, to); err != nil {
		log.Warn("review: top up local candle cache", "instrument", instrument, "granularity", granularity, "err", err)
	}

	instNorm := market.NormalizeInstrument(instrument)
	dm := datamanager.NewDataManager([]string{instNorm}, from, to)
	candles, err := dm.GetCandles(ctx, datamanager.CandleRequest{
		Source:     market.SourceOanda,
		Instrument: instNorm,
		Range:      types.TimeRange{TF: tf},
	}, to, count)
	if err != nil {
		log.Warn("review: read local candle cache", "instrument", instrument, "granularity", granularity, "err", err)
	}
	if len(candles) < count-reviewFetchShortfallTolerance {
		// The cache doesn't hold enough bars to satisfy the review window —
		// whether because it's empty (brand-new pair), the top-up download
		// above failed, or the cached data is short/incomplete for some
		// other reason (e.g. a corrupted month file with a "flagged valid"
		// row at today's date but far fewer than count usable bars —
		// ensureCachedOandaCandles's LastCompleteDate check alone can't
		// detect that). Retry via a forced full-window re-download so
		// review always gets a full series to work with, rather than
		// silently running indicators on too little data or skipping the
		// instrument outright.
		//
		// reviewFetchShortfallTolerance absorbs the ordinary case: an
		// exact-count request can legitimately come back a few candles
		// short even when the cache is fully populated, since
		// datamanager.CandleWindow's calendar-to-weekday conversion is an
		// approximation that doesn't model holidays (same phenomenon
		// reviewSweepFetchHeadroom works around on the sweep path).
		// Without this tolerance, that ordinary few-candle shortfall
		// tripped the retry — a full multi-month re-download — on every
		// instrument, every run, turning a cache-hit into the slowest path
		// available.
		return s.retryReviewCandleTimesDownload(ctx, instrument, oandaName, granularity, from, to, count)
	}
	return candles, nil
}

// reviewFetchShortfallTolerance is how many candles short of an exact-count
// request fetchReviewCandleTimes tolerates before treating the cache as
// insufficient and triggering retryReviewCandleTimesDownload's full
// re-download. See the comment at its use site.
const reviewFetchShortfallTolerance = 20

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
	tf, err := types.ParseTimeframe(granularity)
	if err != nil {
		return fmt.Errorf("unknown timeframe %q", granularity)
	}

	dlFrom := from
	if last, err := datamanager.GetDataManager().LastCompleteDate(oandaName, tf, market.SourceOanda); err == nil {
		if next := last.Add(time.Duration(tf) * time.Second); next.After(dlFrom) {
			dlFrom = next
		}
	}
	if !dlFrom.Before(to) {
		return nil // already up to date, nothing to download
	}
	dlFrom = time.Date(dlFrom.Year(), dlFrom.Month(), 1, 0, 0, 0, 0, time.UTC)

	_, err = s.dataSvc().DownloadOandaCandles(ctx, datasvc.DownloadOandaCandlesRequest{
		Instrument: oandaName,
		Timeframe:  granularity,
		From:       dlFrom,
		To:         to,
	})
	return err
}

// retryReviewCandleTimesDownload is the fallback when the local candle cache
// still doesn't hold enough bars after ensureCachedOandaCandles's
// incremental top-up — e.g. the top-up failed, or LastCompleteDate reports
// the cache as current even though the underlying month file is short or
// corrupted (a "flagged valid" row can exist at today's date while holding
// far fewer than count usable bars). Unlike ensureCachedOandaCandles, this
// does not consult LastCompleteDate at all: it forces DataManager to
// re-download and overwrite the whole [from, to] window's month files, so a
// corrupted cache actually gets repaired rather than re-confirmed as
// current. Always goes through DataManager's OANDA provider — never a
// direct OANDA client call — so every candle review ever sees is written
// through the same canonical store path.
func (s *Service) retryReviewCandleTimesDownload(ctx context.Context, instrument, oandaName, granularity string, from, to time.Time, count int) ([]market.Candle, error) {
	dlFrom := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.dataSvc().DownloadOandaCandles(ctx, datasvc.DownloadOandaCandlesRequest{
		Instrument: oandaName,
		Timeframe:  granularity,
		From:       dlFrom,
		To:         to,
	}); err != nil {
		return nil, fmt.Errorf("retry download %s %s candles: %w", instrument, granularity, err)
	}

	tf, ok := reviewTimeframe(granularity)
	if !ok {
		return nil, fmt.Errorf("review: unsupported granularity %q", granularity)
	}
	instNorm := market.NormalizeInstrument(instrument)
	dm := datamanager.NewDataManager([]string{instNorm}, from, to)
	candles, err := dm.GetCandles(ctx, datamanager.CandleRequest{
		Source:     market.SourceOanda,
		Instrument: instNorm,
		Range:      types.TimeRange{TF: tf},
	}, to, count)
	if err != nil {
		return nil, fmt.Errorf("read local candle cache after retry download %s %s: %w", instrument, granularity, err)
	}
	// Deliberately no minimum-length check here: an exact-count request can
	// legitimately come back a few short of count even after a full
	// re-download, since CandleWindow's calendar-to-weekday-count conversion
	// is an approximation that doesn't model holidays (see review package's
	// sweepFetchHeadroom doc comment in review/replay.go for the same
	// phenomenon on the sweep path — a request for exactly 220 D1 candles
	// once came back 219 on real data). ReviewPair's own indicator warmup
	// checks are the real sufficiency gate for classification; a handful of
	// bars short of the full W1-derivation window is not a fetch failure.
	return candles, nil
}
