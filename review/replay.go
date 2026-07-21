package review

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// SweepRequest parameterizes a historical classification sweep: replay
// ReviewPair's classification (Bucket/Bias/Notes) as of one or more past
// points in time. See docs/archive/asof-review-sweep-spec.md §4.
//
// This replays the classification layer only. It does not simulate trade
// entries, stops, or P&L — see the spec's §4.1 scope boundary.
type SweepRequest struct {
	// Instruments to review. Defaults to market.AllInstruments() when empty.
	Instruments []string

	// From, To bound the sweep, inclusive. A single-date review (--asof)
	// sets From == To.
	From, To time.Time

	// Interval steps between sweep dates when From != To. Defaults to 24h.
	Interval time.Duration

	// Thresholds overrides DefaultThresholds() field-by-field; a
	// zero-valued field falls back to the default (see MergeThresholds).
	Thresholds Thresholds
}

// sweepFetchHeadroom pads the count requested from getClosedCandles beyond
// the strict D1/H4 requirement (WeeklyLookbackDays / CandleCounts["H4"]).
// GetCandles' fetch window is sized from a flat weekday ratio
// (candleWindowBufferNum/Den) that doesn't model holidays, so a request
// for exactly the required count can legitimately come back one or more
// short on a real historical date — confirmed against live local data,
// where a request for exactly 220 D1 candles came back 219. The live path
// never notices this because a short cache falls back to a direct OANDA
// fetch; a sweep has no such fallback by design (§4's replay must only
// ever see what's on disk), so it needs enough headroom in the initial
// request to absorb ordinary holiday variance instead of skipping
// otherwise-valid dates.
const sweepFetchHeadroom = 20

// SweepResponse is the sweep output: one ReviewResult per (step time,
// instrument), each result's ScannedAt set to that step's time rather than
// time.Now(). Results are ordered by instrument, then by ScannedAt
// ascending, so a single pair's bucket transitions read as a time series.
type SweepResponse struct {
	Results []ReviewResult `json:"results"`
}

// RunSweep replays ReviewPair's classification as of every step time in
// [req.From, req.To] stepped by req.Interval, for each instrument in
// req.Instruments, using log for skip/warning diagnostics (slog.Default()
// when nil).
//
// Every candle read is closed-bars-only (spec §4.2): a step at time T only
// ever sees candles that were fully closed by T, so a sweep never leaks a
// still-forming bar's data into a historical classification — see
// getClosedCandles. Unlike a live review path, this never tops up the
// local store from OANDA and never falls back to a direct OANDA fetch: a
// historical replay only ever sees what's already on disk. An instrument
// or step with insufficient local history is skipped with a logged gap
// rather than failing the whole sweep (spec §4.3 items 2 and 3).
func RunSweep(ctx context.Context, log *slog.Logger, req SweepRequest) (*SweepResponse, error) {
	if log == nil {
		log = slog.Default()
	}

	instruments := req.Instruments
	if len(instruments) == 0 {
		instruments = market.AllInstruments()
	}

	interval := req.Interval
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	th := MergeThresholds(DefaultThresholds(), req.Thresholds)

	dm := datamanager.GetDataManager()

	var results []ReviewResult
	for step := req.From; !step.After(req.To); step = step.Add(interval) {
		for _, name := range instruments {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			result, ok := reviewOneInstrumentAsOf(ctx, dm, log, name, step, th)
			if !ok {
				continue
			}
			results = append(results, result)
		}
	}

	// The loop above appends in step-major order (date, then instrument)
	// since it's cheaper to fetch that way (adjacent steps for the same
	// instrument share monthly candle files). Sort into the documented
	// instrument-major order before returning, so every caller sees a
	// single pair's bucket transitions as a contiguous time series.
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Instrument != results[j].Instrument {
			return results[i].Instrument < results[j].Instrument
		}
		return results[i].ScannedAt.Before(results[j].ScannedAt)
	})

	return &SweepResponse{Results: results}, nil
}

// reviewOneInstrumentAsOf mirrors the live review path's fetch-and-classify
// shape, but reads exclusively from the local candle store (via
// getClosedCandles) instead of topping up from OANDA: no OANDA fetch at
// all. ok is false when the instrument or asOf should be skipped — unknown
// instrument, or insufficient local history for D1 or H4 as of asOf.
func reviewOneInstrumentAsOf(ctx context.Context, dm *datamanager.DataManager, log *slog.Logger, name string, asOf time.Time, th Thresholds) (ReviewResult, bool) {
	if market.GetInstrument(name) == nil {
		log.Warn("review sweep: unknown instrument", "instrument", name)
		return ReviewResult{}, false
	}
	instNorm := market.NormalizeInstrument(name)

	dailyWide, err := getClosedCandles(ctx, dm, instNorm, types.D1, asOf, WeeklyLookbackDays+sweepFetchHeadroom)
	if err != nil || len(dailyWide) < WeeklyLookbackDays {
		log.Warn("review sweep: insufficient D1 history", "instrument", name, "asof", asOf, "got", len(dailyWide), "want", WeeklyLookbackDays, "err", err)
		return ReviewResult{}, false
	}
	d1 := CandlesOnly(dailyWide)
	if len(d1) > CandleCounts["D"] {
		d1 = d1[len(d1)-CandleCounts["D"]:]
	}
	w1 := DeriveWeeklyCandlesAsOf(dailyWide, CandleCounts["W"], asOf)

	h4Wide, err := getClosedCandles(ctx, dm, instNorm, types.H4, asOf, CandleCounts["H4"]+sweepFetchHeadroom)
	if err != nil || len(h4Wide) < CandleCounts["H4"] {
		log.Warn("review sweep: insufficient H4 history", "instrument", name, "asof", asOf, "got", len(h4Wide), "want", CandleCounts["H4"], "err", err)
		return ReviewResult{}, false
	}
	h4 := CandlesOnly(h4Wide)
	if len(h4) > CandleCounts["H4"] {
		h4 = h4[len(h4)-CandleCounts["H4"]:]
	}

	result, err := ReviewPair(name, w1, d1, h4, th)
	if err != nil {
		log.Warn("review sweep: compute", "instrument", name, "asof", asOf, "err", err)
		return ReviewResult{}, false
	}

	// H1 is an entry-timing refinement computed only for pairs already
	// classified tradeable as of this step, in this same call — never a
	// follow-up sweep pass. Insufficient/missing H1 history is best-effort:
	// it never drops the step/instrument from the sweep (EnrichTradeableWithH1
	// is a no-op for any other bucket).
	result = EnrichTradeableWithH1(result, log, name, func() ([]market.Candle, error) {
		h1Wide, fetchErr := getClosedCandles(ctx, dm, instNorm, types.H1, asOf, CandleCounts["H1"]+sweepFetchHeadroom)
		if fetchErr != nil {
			return nil, fmt.Errorf("fetch H1 candles: %w", fetchErr)
		}
		if len(h1Wide) < CandleCounts["H1"] {
			return nil, fmt.Errorf("insufficient H1 history: got %d, want %d", len(h1Wide), CandleCounts["H1"])
		}
		h1 := CandlesOnly(h1Wide)
		if len(h1) > CandleCounts["H1"] {
			h1 = h1[len(h1)-CandleCounts["H1"]:]
		}
		return h1, nil
	})

	result.ScannedAt = asOf
	return result, true
}

// getClosedCandles wraps DataManager.GetCandles with the spec §4.2
// closed-bars-only cutoff. GetCandles's own asof semantics are "candle
// open time <= asof" — inclusive of a bar that opened at exactly asof but
// has not closed yet. For a historical replay that bar's data was not
// actually available at that wall-clock moment, so including it would give
// every sweep step a one-bar lookahead bias. Shifting the query point back
// by one full bar period guarantees every returned candle's close time is
// <= asOf: a candle opening at asOf-tf closes at exactly asOf, so it is
// correctly included as "just closed", while a candle opening at asOf
// itself (not yet closed) is correctly excluded.
func getClosedCandles(ctx context.Context, dm *datamanager.DataManager, instrument string, tf types.Timeframe, asOf time.Time, count int) ([]market.Candle, error) {
	closedAsOf := asOf.Add(-time.Duration(tf) * time.Second)
	return dm.GetCandles(ctx, datamanager.CandleRequest{
		Source:     market.SourceOanda,
		Instrument: instrument,
		Range:      types.TimeRange{TF: tf},
	}, closedAsOf, count)
}
