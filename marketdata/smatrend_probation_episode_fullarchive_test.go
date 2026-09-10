//go:build fullarchive

// This file runs issue #354's own post-exit/whipsaw diagnostic: for
// every completed exit/re-entry episode in one continuous
// strategy/smatrend "probation-trend" run against real SPY history,
// measure what the market actually did while the strategy was flat,
// and record enough per-episode data to evaluate several candidate
// earlier re-entry signals later without folding any of them into
// one optimized strategy in this issue (issue #354's own explicit
// scope boundary — this is diagnostic only, and changes no strategy
// parameter or rule).
//
// Unlike smatrend_probation_walkforward_fullarchive_test.go's own
// fold-chained driver, this file runs ONE continuous backtest over
// the full dataset (SMA(200) warm-up is handled organically by
// backtest.Scheduler's own WarmupBars suppression — see
// docs/arch's own scheduler notes — so no separate train-window
// prefix is needed the way the walk-forward driver's fold protocol
// requires it). Position sizing is deliberately conservative and
// fixed, not re-anchored (unlike the walk-forward baseline and
// simple-comparison drivers): this diagnostic only ever reads each
// trade's own entry/exit price and timestamp, never its quantity or
// PnL, so sizing realism is irrelevant here as long as it never
// distorts the fill price itself.
//
// Lives under marketdata/ as package marketdata_test for the same
// reason eqs01_walkforward_fullarchive_test.go does (it needs
// marketdata/internal/provider/stooq) and reuses that file's own and
// smatrend_probation_walkforward_fullarchive_test.go's own
// already-built helpers (setupEQS01WalkForwardInstrument,
// eqs01WFInstrument, wfMemoryRecorder, wfJournalEnvironmentFactory,
// runWFBacktestWithJournal, weightedFillPrice) directly rather than
// duplicating them.
//
// Results go to a local, private directory
// (fullArchiveSMATrendEpisodeOutputDir, edited locally, never
// committed) — never anywhere under this repository, per the same
// decision recorded in eqs01_walkforward_fullarchive_test.go's own
// doc comment.
package marketdata_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/rustyeddy/trader/chart"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy/smatrend"
)

// fullArchiveSMATrendEpisodeOutputDir names the local directory this
// test writes its results into — empty by default; an operator edits
// this locally (for example to
// "/srv/trading/sweeps/smatrend-episode-analysis").
const fullArchiveSMATrendEpisodeOutputDir = ""

// episodeReboundThresholds and episodeBreakoutWindows are the
// candidate rebound-percent and breakout-lookback parameter sets
// issue #354 names explicitly under "Candidate follow-up rules."
var (
	episodeReboundThresholds = []float64{0.02, 0.03, 0.05}
	episodeBreakoutWindows   = []int{2, 3, 5, 10}
)

// episodeRow is one completed exit/re-entry episode's own full
// record — issue #354's own required column set (further decline,
// re-entry vs exit, rebound, defensive-advantage capture), a simple,
// explicitly-not-tuned classification, candidate re-entry features at
// the post-exit low and at the actual re-entry bar, and — for each
// candidate re-entry rule issue #354 names — the date that rule would
// have triggered a hypothetical re-entry fill, purely as a
// first-trigger comparison against what actually happened. No
// candidate rule's hypothetical trigger is ever turned into a second,
// simulated P&L in this issue; that is explicitly deferred to a
// future, separate controlled experiment.
type episodeRow struct {
	Episode int `json:"episode"`

	ExitDate     string  `json:"exit_date"`
	ExitPrice    float64 `json:"exit_price"`
	ReEntryDate  string  `json:"reentry_date"`
	ReEntryPrice float64 `json:"reentry_price"`
	DaysFlat     int     `json:"days_flat"`

	PostExitLowDate string  `json:"post_exit_low_date"`
	PostExitLow     float64 `json:"post_exit_low"`

	FurtherDeclinePct       float64 `json:"further_decline_pct"`
	ReEntryVsExitPct        float64 `json:"reentry_vs_exit_pct"`
	ReboundBeforeReEntryPct float64 `json:"rebound_before_reentry_pct"`
	MaxDefensiveAdvantage   float64 `json:"max_defensive_advantage"`
	CapturedAdvantage       float64 `json:"captured_advantage"`
	CaptureRatio            float64 `json:"capture_ratio"` // NaN (empty in CSV) if MaxDefensiveAdvantage is zero

	// Classification is "good-defensive-exit" or "whipsaw" — a simple
	// threshold (FurtherDeclinePct > 1%), deliberately not tuned
	// against final performance (issue #354's own instruction).
	Classification string `json:"classification"`

	// Candidate re-entry features, captured at the post-exit-low bar
	// and at the actual re-entry bar (issue #354's own "Candidate
	// re-entry features to record" list). SMA200Distance/SMA20/SMA50
	// are (Close-SMA)/SMA; empty/omitted (NaN) wherever the series is
	// not yet warmed up — never the case for a bar late enough in the
	// dataset for this strategy to have already made a first entry,
	// but guarded regardless since this is a general-purpose helper.
	AtPostExitLowSMA200Distance   float64 `json:"at_post_exit_low_sma200_distance"`
	AtPostExitLowSMA20            float64 `json:"at_post_exit_low_sma20"`
	AtPostExitLowSMA50            float64 `json:"at_post_exit_low_sma50"`
	AtPostExitLowATR14            float64 `json:"at_post_exit_low_atr14"`
	AtPostExitLowHighestHighSince float64 `json:"at_post_exit_low_highest_high_since_exit"`

	AtReEntrySMA200Distance   float64 `json:"at_reentry_sma200_distance"`
	AtReEntrySMA20            float64 `json:"at_reentry_sma20"`
	AtReEntrySMA50            float64 `json:"at_reentry_sma50"`
	AtReEntryATR14            float64 `json:"at_reentry_atr14"`
	AtReEntryHighestHighSince float64 `json:"at_reentry_highest_high_since_exit"`
	AtReEntryLowestLowSince   float64 `json:"at_reentry_lowest_low_since_exit"`

	// Candidate rule first-trigger comparisons. Each *TriggerDate is
	// empty and *DaysEarlier is 0 when the candidate never triggered
	// before the actual re-entry (including the trivial case of zero
	// flat bars between exit and re-entry, where no candidate had any
	// bar to evaluate at all).
	CandidateAboveSMATriggerDate      string  `json:"candidate_above_sma_trigger_date"`
	CandidateAboveSMADaysEarlier      float64 `json:"candidate_above_sma_days_earlier"`
	CandidateReclaimExitTriggerDate   string  `json:"candidate_reclaim_exit_trigger_date"`
	CandidateReclaimExitDaysEarlier   float64 `json:"candidate_reclaim_exit_days_earlier"`
	CandidateReclaimExitMatchesActual bool    `json:"candidate_reclaim_exit_matches_actual"`

	CandidateBreakoutTriggerDate map[int]string      `json:"-"` // flattened into named columns below at CSV-write time
	CandidateBreakoutDaysEarlier map[int]float64     `json:"-"`
	CandidateReboundTriggerDate  map[float64]string  `json:"-"`
	CandidateReboundDaysEarlier  map[float64]float64 `json:"-"`
}

// smaSeries returns a per-bar simple moving average of Close, aligned
// index-for-index with bars — math.NaN() at any index before period-1
// bars are available. A plain, hand-rolled series (not
// indicator.SMA's own streaming API) since this diagnostic only ever
// needs a pure function of the already-fetched bars slice, computed
// once, not an incremental/stateful consumer.
func smaSeries(bars []marketdata.Bar, period int) []float64 {
	out := make([]float64, len(bars))
	sum := 0.0
	for i, b := range bars {
		sum += b.Close.Float64()
		if i >= period {
			sum -= bars[i-period].Close.Float64()
		}
		if i < period-1 {
			out[i] = math.NaN()
			continue
		}
		out[i] = sum / float64(period)
	}
	return out
}

// atrSeries returns a per-bar simple (non-Wilder-smoothed) rolling
// average true range, aligned index-for-index with bars —
// math.NaN() before period true-range samples are available (the
// first true-range sample itself needs a previous bar, so index 0 is
// always NaN). "Simple... if available or easy to compute" is
// issue #354's own explicit allowance; Wilder smoothing is not
// implemented here since nothing in this diagnostic depends on
// matching any specific ATR convention exactly.
func atrSeries(bars []marketdata.Bar, period int) []float64 {
	out := make([]float64, len(bars))
	out[0] = math.NaN()
	trueRanges := make([]float64, len(bars))
	for i := 1; i < len(bars); i++ {
		h, l, prevClose := bars[i].High.Float64(), bars[i].Low.Float64(), bars[i-1].Close.Float64()
		tr := h - l
		if v := math.Abs(h - prevClose); v > tr {
			tr = v
		}
		if v := math.Abs(l - prevClose); v > tr {
			tr = v
		}
		trueRanges[i] = tr
	}
	sum := 0.0
	for i := 1; i < len(bars); i++ {
		sum += trueRanges[i]
		if i > period {
			sum -= trueRanges[i-period]
		}
		if i < period {
			out[i] = math.NaN()
			continue
		}
		out[i] = sum / float64(period)
	}
	return out
}

// smaDistance returns (close-sma)/sma, or math.NaN() if sma is NaN or
// zero.
func smaDistance(close, sma float64) float64 {
	if math.IsNaN(sma) || sma == 0 {
		return math.NaN()
	}
	return (close - sma) / sma
}

// highestHighInRange/lowestLowInRange return the highest High / lowest
// Low across bars[from:to] inclusive of both ends. Both require
// from<=to<len(bars); callers only ever call these with a
// non-empty, in-bounds range.
func highestHighInRange(bars []marketdata.Bar, from, to int) num.Price {
	h := bars[from].High
	for i := from + 1; i <= to; i++ {
		if bars[i].High.Cmp(h) > 0 {
			h = bars[i].High
		}
	}
	return h
}

func lowestLowInRange(bars []marketdata.Bar, from, to int) num.Price {
	l := bars[from].Low
	for i := from + 1; i <= to; i++ {
		if bars[i].Low.Cmp(l) < 0 {
			l = bars[i].Low
		}
	}
	return l
}

// indexOfBarTime returns the index of the bar whose Time equals t, or
// -1 if none exists — used to map a Trade's own OpenedAt/ClosedAt
// timestamp back to its position in the canonical bars slice.
func indexOfBarTime(bars []marketdata.Bar, t time.Time) int {
	for i, b := range bars {
		if b.Time.Equal(t) {
			return i
		}
	}
	return -1
}

// candidateFirstTrigger walks bars[flatFrom:flatTo] inclusive (the
// bars while the strategy is genuinely flat and could generate a
// signal that fills the *next* bar's open, matching every real
// ExitRule/ReEntryRule's own next-bar-open fill convention) and
// returns the index of the first bar satisfying cond, or -1 if none
// does. cond is called with the bar index and must not mutate shared
// state outside itself; per-candidate running state (sinceExitHigh,
// runningLow) is threaded through each candidate's own closure by its
// caller instead, so each candidate can be evaluated independently
// over the identical bar range in one pass.
func candidateFirstTrigger(flatFrom, flatTo int, cond func(i int) bool) int {
	if flatFrom > flatTo {
		return -1
	}
	for i := flatFrom; i <= flatTo; i++ {
		if cond(i) {
			return i
		}
	}
	return -1
}

// TestSMATrendProbationTrendEpisodeAnalysis runs strategy/smatrend's
// "probation-trend" ExitRule (SMAPeriod 200, InitialStopBelowSMA
// 0.01, TrailActivationGain 0.05, TrailingStopPercent 0.10,
// ReEntryRuleName "reclaim-exit-price" — the exact playbook reference
// configuration smatrend_probation_walkforward_fullarchive_test.go's
// own baseline already established as "current") once, continuously,
// over SPY's full Stooq history, and extracts issue #354's own
// per-exit/re-entry-episode diagnostic table.
//
// Every completed exit/re-entry episode (a closed trade immediately
// followed by another entry, whether or not that next entry itself
// later closes) is recorded exactly once. A final exit with no
// subsequent re-entry before the dataset ends is not a "completed"
// episode and is excluded, matching issue #354's own acceptance
// criterion.
//
// Candidate re-entry rule comparisons in this file evaluate each
// rule's own bare ShouldEnter condition only — unlike the real
// Strategy.onFlat, which additionally requires price be back above
// the SMA before consulting any configured ReEntryRule at all (see
// strategy/smatrend/doc.go's own "gated centrally by Strategy itself"
// note). A candidate may therefore appear to trigger earlier than the
// real strategy ever could have, most visibly for
// "reclaim-exit-price" itself (the rule actually governing this run's
// own real re-entries): CandidateReclaimExitMatchesActual records how
// often the two agree exactly, as a direct sanity check on this
// file's own simulation rather than a claim that the SMA regime gate
// is unimportant.
func TestSMATrendProbationTrendEpisodeAnalysis(t *testing.T) {
	if fullArchiveSMATrendEpisodeOutputDir == "" {
		t.Skip("fullArchiveSMATrendEpisodeOutputDir is empty; edit the constant in this file to point at a local results directory to run this test")
	}
	if fullArchiveEQS01SPYCSVPath == "" {
		t.Skip("fullArchiveEQS01SPYCSVPath is empty; edit the constant in eqs01_walkforward_fullarchive_test.go to point at a local Stooq SPY export to run this test")
	}
	if err := os.MkdirAll(fullArchiveSMATrendEpisodeOutputDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullArchiveSMATrendEpisodeOutputDir, err)
	}

	ctx := context.Background()
	inst := eqs01WFInstrument{Symbol: "SPY", CSVPath: fullArchiveEQS01SPYCSVPath, Exchange: "ARCA"}
	setup := setupEQS01WalkForwardInstrument(t, ctx, inst)
	bars := setup.bars

	cfg := smatrend.Config{
		SMAPeriod:           200,
		ExitRuleName:        "probation-trend",
		ReEntryRuleName:     "reclaim-exit-price",
		InitialStopBelowSMA: num.MustParseRate("0.01"),
		TrailActivationGain: num.MustParseRate("0.05"),
		TrailingStopPercent: num.MustParseRate("0.10"),
	}

	startingCapital := num.MustParseMoney(eqs01WFStartingCapital, num.MustParseCurrency("USD"))
	// Conservative, fixed (never re-anchored) sizing — this diagnostic
	// only ever reads each trade's own entry/exit price and
	// timestamp, never its quantity or PnL, so realistic sizing is
	// irrelevant here as long as it never distorts a fill price.
	riskFraction := num.MustParseRate("0.01")
	adverseDistance := num.MustParsePrice("20.00000")

	span, err := marketdata.NewTimeRange(bars[0].Time, bars[len(bars)-1].Time.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("run span: %v", err)
	}

	resp, rec, err := runWFBacktestWithJournal(ctx, setup.mgr, setup.simResolver, setup.simID, span, cfg, startingCapital, riskFraction, adverseDistance, setup.priceByTime)
	if err != nil {
		t.Fatalf("backtest run: %v", err)
	}
	fills := rec.fillsByID()
	t.Logf("run produced %d closed trades, %d open at end", len(resp.Trades), len(resp.OpenTrades))

	// Build one OpenedAt-ordered timeline of every trade (closed and
	// still-open) so each closed trade can be paired with whichever
	// trade opened next, regardless of that next trade's own eventual
	// fate.
	all := make([]order.Trade, 0, len(resp.Trades)+len(resp.OpenTrades))
	all = append(all, resp.Trades...)
	all = append(all, resp.OpenTrades...)
	sort.Slice(all, func(i, j int) bool { return all[i].OpenedAt.Before(all[j].OpenedAt) })
	for i := 1; i < len(all); i++ {
		if all[i].OpenedAt.Before(all[i-1].OpenedAt) {
			t.Fatalf("trade %d opened before trade %d despite sort (%s < %s): this strategy is long-only single-position, trades must not overlap", i, i-1, all[i].OpenedAt, all[i-1].OpenedAt)
		}
	}

	sma20 := smaSeries(bars, 20)
	sma50 := smaSeries(bars, 50)
	sma200 := smaSeries(bars, 200)
	atr14 := atrSeries(bars, 14)

	var rows []episodeRow
	reclaimMatches, reclaimTotal := 0, 0

	for i := 0; i < len(all); i++ {
		tr := all[i]
		if tr.ClosedAt.IsZero() {
			continue // still open: cannot be the "exit" side of a completed episode
		}
		if i+1 >= len(all) {
			continue // no subsequent re-entry before the dataset ends: not a completed episode
		}
		next := all[i+1]

		exitIdx := indexOfBarTime(bars, tr.ClosedAt)
		reentryIdx := indexOfBarTime(bars, next.OpenedAt)
		if exitIdx < 0 || reentryIdx < 0 || reentryIdx <= exitIdx {
			t.Fatalf("episode %d: could not resolve exit bar (%s -> idx %d) / reentry bar (%s -> idx %d) against canonical bars", len(rows), tr.ClosedAt, exitIdx, next.OpenedAt, reentryIdx)
		}

		exitPrice, _, err := weightedFillPrice(t, tr.ExitFillIDs, fills)
		if err != nil {
			t.Fatalf("episode %d: reconstructing exit price: %v", len(rows), err)
		}
		reentryPrice, _, err := weightedFillPrice(t, next.EntryFillIDs, fills)
		if err != nil {
			t.Fatalf("episode %d: reconstructing reentry price: %v", len(rows), err)
		}
		exitPriceF, reentryPriceF := exitPrice.Float64(), reentryPrice.Float64()

		// Post-exit low: the lowest Low from the exit bar itself
		// through the bar immediately before re-entry, inclusive of
		// the exit bar — so a zero-flat-bar episode (re-entry on the
		// very next bar) still has a well-defined post-exit low
		// rather than an empty range.
		lowIdx := exitIdx
		low := bars[exitIdx].Low
		for j := exitIdx + 1; j < reentryIdx; j++ {
			if bars[j].Low.Cmp(low) < 0 {
				low = bars[j].Low
				lowIdx = j
			}
		}
		lowF := low.Float64()

		furtherDeclinePct := (exitPriceF - lowF) / exitPriceF
		reentryVsExitPct := (reentryPriceF - exitPriceF) / exitPriceF
		reboundPct := 0.0
		if lowF != 0 {
			reboundPct = (reentryPriceF - lowF) / lowF
		}
		maxAdvantage := exitPriceF - lowF
		capturedAdvantage := exitPriceF - reentryPriceF
		captureRatio := math.NaN()
		if maxAdvantage != 0 {
			captureRatio = capturedAdvantage / maxAdvantage
		}

		classification := "good-defensive-exit"
		if furtherDeclinePct <= 0.01 {
			classification = "whipsaw"
		}

		row := episodeRow{
			Episode:                 len(rows),
			ExitDate:                tr.ClosedAt.Format("2006-01-02"),
			ExitPrice:               exitPriceF,
			ReEntryDate:             next.OpenedAt.Format("2006-01-02"),
			ReEntryPrice:            reentryPriceF,
			DaysFlat:                reentryIdx - exitIdx - 1,
			PostExitLowDate:         bars[lowIdx].Time.Format("2006-01-02"),
			PostExitLow:             lowF,
			FurtherDeclinePct:       furtherDeclinePct,
			ReEntryVsExitPct:        reentryVsExitPct,
			ReboundBeforeReEntryPct: reboundPct,
			MaxDefensiveAdvantage:   maxAdvantage,
			CapturedAdvantage:       capturedAdvantage,
			CaptureRatio:            captureRatio,
			Classification:          classification,

			AtPostExitLowSMA200Distance:   smaDistance(bars[lowIdx].Close.Float64(), sma200[lowIdx]),
			AtPostExitLowSMA20:            sma20[lowIdx],
			AtPostExitLowSMA50:            sma50[lowIdx],
			AtPostExitLowATR14:            atr14[lowIdx],
			AtPostExitLowHighestHighSince: highestHighInRange(bars, exitIdx, lowIdx).Float64(),

			AtReEntrySMA200Distance:   smaDistance(bars[reentryIdx].Close.Float64(), sma200[reentryIdx]),
			AtReEntrySMA20:            sma20[reentryIdx],
			AtReEntrySMA50:            sma50[reentryIdx],
			AtReEntryATR14:            atr14[reentryIdx],
			AtReEntryHighestHighSince: highestHighInRange(bars, exitIdx, reentryIdx).Float64(),
			AtReEntryLowestLowSince:   lowestLowInRange(bars, exitIdx, reentryIdx).Float64(),

			CandidateBreakoutTriggerDate: map[int]string{},
			CandidateBreakoutDaysEarlier: map[int]float64{},
			CandidateReboundTriggerDate:  map[float64]string{},
			CandidateReboundDaysEarlier:  map[float64]float64{},
		}

		// Candidate rule first-trigger scan, over the bars where the
		// strategy is genuinely flat and a signal there would fill at
		// the *next* bar's open (flatFrom..flatTo inclusive; empty
		// when reentryIdx == exitIdx+1, meaning zero flat bars).
		flatFrom, flatTo := exitIdx+1, reentryIdx-1

		triggerDate := func(signalIdx int) (string, float64) {
			if signalIdx < 0 {
				return "", 0
			}
			fillTime := bars[signalIdx+1].Time
			daysEarlier := next.OpenedAt.Sub(fillTime).Hours() / 24
			return fillTime.Format("2006-01-02"), daysEarlier
		}

		aboveSMAIdx := candidateFirstTrigger(flatFrom, flatTo, func(j int) bool {
			return !math.IsNaN(sma200[j]) && bars[j].Close.Float64() > sma200[j]
		})
		row.CandidateAboveSMATriggerDate, row.CandidateAboveSMADaysEarlier = triggerDate(aboveSMAIdx)

		reclaimIdx := candidateFirstTrigger(flatFrom, flatTo, func(j int) bool {
			return bars[j].Close.Cmp(exitPrice) > 0
		})
		row.CandidateReclaimExitTriggerDate, row.CandidateReclaimExitDaysEarlier = triggerDate(reclaimIdx)
		reclaimTotal++
		if fd, _ := triggerDate(reclaimIdx); fd == row.ReEntryDate {
			reclaimMatches++
			row.CandidateReclaimExitMatchesActual = true
		}

		for _, n := range episodeBreakoutWindows {
			nn := n
			idx := candidateFirstTrigger(flatFrom, flatTo, func(j int) bool {
				priorFrom := j - nn
				if priorFrom < 0 {
					return false
				}
				priorHigh := highestHighInRange(bars, priorFrom, j-1)
				return bars[j].Close.Cmp(priorHigh) > 0
			})
			d, de := triggerDate(idx)
			row.CandidateBreakoutTriggerDate[nn] = d
			row.CandidateBreakoutDaysEarlier[nn] = de
		}

		for _, pct := range episodeReboundThresholds {
			p := pct
			idx := candidateFirstTrigger(flatFrom, flatTo, func(j int) bool {
				runningLow := lowestLowInRange(bars, exitIdx, j)
				threshold := runningLow.Float64() * (1 + p)
				return bars[j].Close.Float64() >= threshold
			})
			d, de := triggerDate(idx)
			row.CandidateReboundTriggerDate[p] = d
			row.CandidateReboundDaysEarlier[p] = de
		}

		rows = append(rows, row)
	}

	t.Logf("%d completed exit/re-entry episodes", len(rows))
	if reclaimTotal > 0 {
		t.Logf("reclaim-exit-price candidate matches actual re-entry date in %d/%d episodes (%.1f%%) — mismatches are expected whenever price reclaimed the exit level while still below the SMA, since the real strategy additionally gates every re-entry on being back above the SMA (see this test's own doc comment)",
			reclaimMatches, reclaimTotal, 100*float64(reclaimMatches)/float64(reclaimTotal))
	}

	writeEpisodeCSV(t, filepath.Join(fullArchiveSMATrendEpisodeOutputDir, "spy-exit-reentry-episodes.csv"), rows)
	writeEpisodeSummary(t, filepath.Join(fullArchiveSMATrendEpisodeOutputDir, "spy-episode-summary.json"), rows)
	renderEpisodeCharts(t, fullArchiveSMATrendEpisodeOutputDir, bars, rows, exitReentryEpisodeStopSeries(rows))
}

// exitReentryEpisodeStopSeries is a placeholder returning nil: this
// diagnostic does not reconstruct the strategy's own resting-stop
// level series (ProbationStop/TrailingStop), since issue #354 does
// not ask for it and reconstructing it independently of the real
// ExitRule's own internal state risks silently disagreeing with what
// actually governed each trade. Episode charts render Close price and
// entry/exit/trough markers only.
func exitReentryEpisodeStopSeries([]episodeRow) []chart.LevelPoint { return nil }

// writeEpisodeCSV writes rows to path, one row per episode, flattening
// each candidate rule's own per-parameter trigger date/days-earlier
// map into named columns (breakout_2_trigger_date,
// breakout_2_days_earlier, ..., rebound_0.02_trigger_date, ...) so the
// CSV stays a single flat table.
func writeEpisodeCSV(t *testing.T, path string, rows []episodeRow) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	w := csv.NewWriter(f)

	header := []string{
		"episode", "exit_date", "exit_price", "reentry_date", "reentry_price", "days_flat",
		"post_exit_low_date", "post_exit_low",
		"further_decline_pct", "reentry_vs_exit_pct", "rebound_before_reentry_pct",
		"max_defensive_advantage", "captured_advantage", "capture_ratio", "classification",
		"at_post_exit_low_sma200_distance", "at_post_exit_low_sma20", "at_post_exit_low_sma50",
		"at_post_exit_low_atr14", "at_post_exit_low_highest_high_since_exit",
		"at_reentry_sma200_distance", "at_reentry_sma20", "at_reentry_sma50",
		"at_reentry_atr14", "at_reentry_highest_high_since_exit", "at_reentry_lowest_low_since_exit",
		"candidate_above_sma_trigger_date", "candidate_above_sma_days_earlier",
		"candidate_reclaim_exit_trigger_date", "candidate_reclaim_exit_days_earlier", "candidate_reclaim_exit_matches_actual",
	}
	for _, n := range episodeBreakoutWindows {
		header = append(header, fmt.Sprintf("breakout_%d_trigger_date", n), fmt.Sprintf("breakout_%d_days_earlier", n))
	}
	for _, pct := range episodeReboundThresholds {
		header = append(header, fmt.Sprintf("rebound_%.0fpct_trigger_date", pct*100), fmt.Sprintf("rebound_%.0fpct_days_earlier", pct*100))
	}
	if err := w.Write(header); err != nil {
		t.Fatalf("write header: %v", err)
	}

	ff := func(v float64) string {
		if math.IsNaN(v) {
			return ""
		}
		return fmt.Sprintf("%.6f", v)
	}

	for _, r := range rows {
		rec := []string{
			fmt.Sprintf("%d", r.Episode), r.ExitDate, ff(r.ExitPrice), r.ReEntryDate, ff(r.ReEntryPrice), fmt.Sprintf("%d", r.DaysFlat),
			r.PostExitLowDate, ff(r.PostExitLow),
			ff(r.FurtherDeclinePct), ff(r.ReEntryVsExitPct), ff(r.ReboundBeforeReEntryPct),
			ff(r.MaxDefensiveAdvantage), ff(r.CapturedAdvantage), ff(r.CaptureRatio), r.Classification,
			ff(r.AtPostExitLowSMA200Distance), ff(r.AtPostExitLowSMA20), ff(r.AtPostExitLowSMA50),
			ff(r.AtPostExitLowATR14), ff(r.AtPostExitLowHighestHighSince),
			ff(r.AtReEntrySMA200Distance), ff(r.AtReEntrySMA20), ff(r.AtReEntrySMA50),
			ff(r.AtReEntryATR14), ff(r.AtReEntryHighestHighSince), ff(r.AtReEntryLowestLowSince),
			r.CandidateAboveSMATriggerDate, ff(r.CandidateAboveSMADaysEarlier),
			r.CandidateReclaimExitTriggerDate, ff(r.CandidateReclaimExitDaysEarlier), fmt.Sprintf("%v", r.CandidateReclaimExitMatchesActual),
		}
		for _, n := range episodeBreakoutWindows {
			rec = append(rec, r.CandidateBreakoutTriggerDate[n], ff(r.CandidateBreakoutDaysEarlier[n]))
		}
		for _, pct := range episodeReboundThresholds {
			rec = append(rec, r.CandidateReboundTriggerDate[pct], ff(r.CandidateReboundDaysEarlier[pct]))
		}
		if err := w.Write(rec); err != nil {
			t.Fatalf("write row %d: %v", r.Episode, err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatalf("flush %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	t.Logf("wrote %s", path)
}

// episodeSummary answers issue #354's own six "Initial research
// questions" from rows, plus the sorted "biggest useful exits" /
// "worst whipsaws" views its Output section asks for.
type episodeSummary struct {
	TotalEpisodes int `json:"total_episodes"`

	// Question 1: how often does price continue lower after exit by
	// more than 1%/2%/5%/10%.
	DeclinedMoreThan map[string]int `json:"declined_more_than"`

	// Question 2: how often is the eventual re-entry above the prior
	// exit price.
	ReEntryAboveExitCount int     `json:"reentry_above_exit_count"`
	ReEntryAboveExitPct   float64 `json:"reentry_above_exit_pct"`

	// Question 3: rebound surrendered before re-entry.
	MeanReboundBeforeReEntryPct   float64 `json:"mean_rebound_before_reentry_pct"`
	MedianReboundBeforeReEntryPct float64 `json:"median_rebound_before_reentry_pct"`

	// Question 4: whipsaw vs. good-defensive-exit split.
	WhipsawCount           int     `json:"whipsaw_count"`
	GoodDefensiveExitCount int     `json:"good_defensive_exit_count"`
	WhipsawPct             float64 `json:"whipsaw_pct"`

	// Question 5: sorted views — the N episodes that gave back the
	// most rebound before re-entering (biggest apparent cost of
	// waiting, independent of Classification: several of these are
	// still "good-defensive-exit" episodes that simply also stayed
	// flat a very long time) and the N episodes with the largest
	// further decline (clearest defensive value).
	HighestReboundGivenBack     []episodeRow `json:"highest_rebound_given_back"`
	BestDefensiveExitsByDecline []episodeRow `json:"best_defensive_exits_by_decline"`

	// Question 6: for each candidate rule, how often it would have
	// triggered strictly earlier than the actual re-entry, and the
	// mean days earlier across only those episodes.
	CandidateEarlierCount    map[string]int     `json:"candidate_earlier_count"`
	CandidateMeanDaysEarlier map[string]float64 `json:"candidate_mean_days_earlier"`
}

func writeEpisodeSummary(t *testing.T, path string, rows []episodeRow) {
	t.Helper()

	s := episodeSummary{
		TotalEpisodes:            len(rows),
		DeclinedMoreThan:         map[string]int{},
		CandidateEarlierCount:    map[string]int{},
		CandidateMeanDaysEarlier: map[string]float64{},
	}
	if len(rows) == 0 {
		out, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(path, out, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}

	thresholds := []float64{0.01, 0.02, 0.05, 0.10}
	reboundSum, reboundVals := 0.0, make([]float64, 0, len(rows))
	for _, r := range rows {
		for _, th := range thresholds {
			if r.FurtherDeclinePct > th {
				s.DeclinedMoreThan[fmt.Sprintf("%.0fpct", th*100)]++
			}
		}
		if r.ReEntryVsExitPct > 0 {
			s.ReEntryAboveExitCount++
		}
		reboundSum += r.ReboundBeforeReEntryPct
		reboundVals = append(reboundVals, r.ReboundBeforeReEntryPct)
		if r.Classification == "whipsaw" {
			s.WhipsawCount++
		} else {
			s.GoodDefensiveExitCount++
		}
	}
	s.ReEntryAboveExitPct = float64(s.ReEntryAboveExitCount) / float64(len(rows))
	s.WhipsawPct = float64(s.WhipsawCount) / float64(len(rows))
	s.MeanReboundBeforeReEntryPct = reboundSum / float64(len(rows))
	sort.Float64s(reboundVals)
	s.MedianReboundBeforeReEntryPct = reboundVals[len(reboundVals)/2]

	sortedByRebound := append([]episodeRow(nil), rows...)
	sort.Slice(sortedByRebound, func(i, j int) bool {
		return sortedByRebound[i].ReboundBeforeReEntryPct > sortedByRebound[j].ReboundBeforeReEntryPct
	})
	n := 10
	if n > len(sortedByRebound) {
		n = len(sortedByRebound)
	}
	s.HighestReboundGivenBack = sortedByRebound[:n]

	sortedByDecline := append([]episodeRow(nil), rows...)
	sort.Slice(sortedByDecline, func(i, j int) bool {
		return sortedByDecline[i].FurtherDeclinePct > sortedByDecline[j].FurtherDeclinePct
	})
	n = 10
	if n > len(sortedByDecline) {
		n = len(sortedByDecline)
	}
	s.BestDefensiveExitsByDecline = sortedByDecline[:n]

	accumulate := func(name string, earlier func(episodeRow) (bool, float64)) {
		count := 0
		sum := 0.0
		for _, r := range rows {
			ok, days := earlier(r)
			if ok && days > 0 {
				count++
				sum += days
			}
		}
		s.CandidateEarlierCount[name] = count
		if count > 0 {
			s.CandidateMeanDaysEarlier[name] = sum / float64(count)
		}
	}
	accumulate("above-sma", func(r episodeRow) (bool, float64) {
		return r.CandidateAboveSMATriggerDate != "", r.CandidateAboveSMADaysEarlier
	})
	accumulate("reclaim-exit-price", func(r episodeRow) (bool, float64) {
		return r.CandidateReclaimExitTriggerDate != "", r.CandidateReclaimExitDaysEarlier
	})
	for _, nWin := range episodeBreakoutWindows {
		w := nWin
		accumulate(fmt.Sprintf("breakout-%d", w), func(r episodeRow) (bool, float64) {
			d := r.CandidateBreakoutTriggerDate[w]
			return d != "", r.CandidateBreakoutDaysEarlier[w]
		})
	}
	for _, pct := range episodeReboundThresholds {
		p := pct
		accumulate(fmt.Sprintf("rebound-%.0fpct", p*100), func(r episodeRow) (bool, float64) {
			d := r.CandidateReboundTriggerDate[p]
			return d != "", r.CandidateReboundDaysEarlier[p]
		})
	}

	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Logf("wrote %s", path)
	fmt.Println(string(out))
}

// renderEpisodeCharts renders one chart.RenderEpisode PNG per episode
// in the highest-rebound-given-back and best-defensive-exit top-5
// lists (issue
// #354's own "notes on any candidate earlier re-entry signals that
// visibly stand out" is easiest to assess visually), each windowed to
// roughly 30 bars before the exit through 30 bars after the re-entry
// (clipped to the dataset's own bounds).
func renderEpisodeCharts(t *testing.T, dir string, bars []marketdata.Bar, rows []episodeRow, stops []chart.LevelPoint) {
	t.Helper()
	if len(rows) == 0 {
		return
	}

	byRebound := append([]episodeRow(nil), rows...)
	sort.Slice(byRebound, func(i, j int) bool {
		return byRebound[i].ReboundBeforeReEntryPct > byRebound[j].ReboundBeforeReEntryPct
	})
	byDecline := append([]episodeRow(nil), rows...)
	sort.Slice(byDecline, func(i, j int) bool { return byDecline[i].FurtherDeclinePct > byDecline[j].FurtherDeclinePct })

	render := func(prefix string, selection []episodeRow) {
		n := 5
		if n > len(selection) {
			n = len(selection)
		}
		for _, r := range selection[:n] {
			exitIdx := indexOfBarTime(bars, mustParseDate(t, r.ExitDate))
			reentryIdx := indexOfBarTime(bars, mustParseDate(t, r.ReEntryDate))
			if exitIdx < 0 || reentryIdx < 0 {
				continue
			}
			from, to := exitIdx-30, reentryIdx+30
			if from < 0 {
				from = 0
			}
			if to >= len(bars) {
				to = len(bars) - 1
			}
			window := bars[from : to+1]

			markers := []chart.Marker{
				{Time: bars[exitIdx].Time, Price: bars[exitIdx].Close, Kind: chart.MarkerExit, Label: fmt.Sprintf("exit %s", r.Classification)},
				{Time: mustParseDate(t, r.PostExitLowDate), Price: num.MustParsePrice(fmt.Sprintf("%.4f", r.PostExitLow)), Kind: chart.MarkerTrough, Label: "post-exit low"},
				{Time: bars[reentryIdx].Time, Price: bars[reentryIdx].Close, Kind: chart.MarkerReentry, Label: "re-entry"},
			}

			in := chart.EpisodeInput{
				Title:   fmt.Sprintf("Episode %d: exit %s -> reentry %s (%s)", r.Episode, r.ExitDate, r.ReEntryDate, r.Classification),
				Bars:    window,
				Markers: markers,
			}
			path := filepath.Join(dir, fmt.Sprintf("%s-episode-%d.png", prefix, r.Episode))
			f, err := os.Create(path)
			if err != nil {
				t.Fatalf("create %s: %v", path, err)
			}
			if err := chart.RenderEpisode(f, in, chart.FormatPNG); err != nil {
				t.Fatalf("render %s: %v", path, err)
			}
			if err := f.Close(); err != nil {
				t.Fatalf("close %s: %v", path, err)
			}
			t.Logf("wrote %s", path)
		}
	}

	render("highest-rebound-given-back", byRebound)
	render("best-defensive-exit", byDecline)
}

func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return ts
}
